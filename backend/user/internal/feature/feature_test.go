package feature

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const all = "free,starter,pro,advanced"

func TestMiddleware(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	cfg := sysconfig.New(pool)
	set := func(key, v string) {
		if _, err := pool.Exec(ctx, `update system_config set value = $2 where key = $1`, key, v); err != nil {
			t.Fatal(err)
		}
		cfg.Invalidate()
	}
	t.Cleanup(func() {
		for _, k := range []string{"feature_templates", "feature_api", "feature_files", "feature_feedback", "feature_integrations"} {
			set(k, "true")
			set(k+"_plans", all)
		}
	})

	// A Pro owner's workspace and a Free owner's, each with a feedback form and a GitHub hook.
	proUser, freeUser := dbtest.User(t, pool, "Pro"), dbtest.User(t, pool, "Free")
	dbtest.SetPlan(t, pool, proUser, "pro")
	workspaceOf := func(owner string) string {
		slug := dbtest.Name("feat-")
		var id string
		if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by, feedback_token) values ('F', $1, $2, $1)
			returning id::text`, slug, owner).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `insert into git_hooks (workspace_id, provider, token, secret) values ($1, 'github', $2, 'x')`,
			id, slug); err != nil {
			t.Fatal(err)
		}
		return slug
	}
	pro, free := workspaceOf(proUser), workspaceOf(freeUser)
	member := func(ctx context.Context, userID, slug string) (*workspace.Workspace, error) {
		var ws workspace.Workspace
		err := pool.QueryRow(ctx, `select id::text, slug from workspaces where slug = $1 and created_by = $2`, slug, userID).
			Scan(&ws.ID, &ws.Slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return &ws, err
	}

	f := New(cfg, pool)
	mux := http.NewServeMux()
	f.Register(mux, dbtest.Auth, member)
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/api/", ok)
	h := f.Middleware(mux)
	do := func(method, path, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	code := func(method, path, auth string) int { return do(method, path, auth).Code }
	flags := func(userID, slug string) (int, map[string]bool) {
		code, body := dbtest.Do(t, h, userID, http.MethodGet, "/api/workspaces/"+slug+"/features", nil)
		var got map[string]bool
		if code == http.StatusOK {
			if err := json.Unmarshal([]byte(body), &got); err != nil || len(got) != len(names) {
				t.Fatalf("features %s (%v)", body, err)
			}
		}
		return code, got
	}
	public := func() map[string][]string {
		var got map[string][]string
		if err := json.Unmarshal(do(http.MethodGet, "/api/features", "").Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	// Templates on for Pro only.
	set("feature_templates_plans", "pro")
	if c := code("POST", "/api/workspaces/"+pro+"/templates/123", ""); c != http.StatusOK {
		t.Errorf("pro workspace, pro feature: %d", c)
	}
	if w := do("POST", "/api/workspaces/"+free+"/templates/123", ""); w.Code != http.StatusNotFound || w.Body.String() != `{"error":"`+Off+`"}`+"\n" {
		t.Errorf("free workspace, pro feature: %d %s", w.Code, w.Body)
	}
	if c := code("POST", "/api/workspaces/"+free+"/issues/ENG-1", ""); c != http.StatusOK {
		t.Errorf("core route: %d", c)
	}
	if c, got := flags(proUser, pro); c != http.StatusOK || !got[Templates] || !got[Chat] {
		t.Errorf("pro workspace flags %d %v", c, got)
	}
	if c, got := flags(freeUser, free); c != http.StatusOK || got[Templates] || !got[Chat] {
		t.Errorf("free workspace flags %d %v", c, got)
	}
	if c, _ := flags(freeUser, pro); c != http.StatusNotFound {
		t.Errorf("non-member flags: %d", c)
	}
	if got := public(); !slices.Equal(got[Templates], []string{"pro"}) {
		t.Errorf("public flags with templates on for pro: %v", got)
	}
	set("feature_templates_plans", "")
	if got := public(); len(got[Templates]) != 0 || len(got[Chat]) != 4 {
		t.Errorf("public flags with templates off: %v", got)
	}
	if c := code("POST", "/api/workspaces/"+pro+"/templates", ""); c != http.StatusNotFound {
		t.Errorf("off everywhere: %d", c)
	}

	// The on/off switch overrides the plans and keeps them: off for Pro too, then back as it was.
	set("feature_templates_plans", "pro")
	set("feature_templates", "false")
	if c := code("POST", "/api/workspaces/"+pro+"/templates", ""); c != http.StatusNotFound {
		t.Errorf("switched off, pro: %d", c)
	}
	set("feature_templates", "true")
	if c := code("POST", "/api/workspaces/"+pro+"/templates", ""); c != http.StatusOK {
		t.Errorf("switched back on, pro: %d", c)
	}
	if c := code("POST", "/api/workspaces/"+free+"/templates", ""); c != http.StatusNotFound {
		t.Errorf("switched back on, free: %d", c)
	}

	// Tokens: per workspace, and outside one only off when off for every plan.
	set("feature_api_plans", "pro")
	if c := code("GET", "/api/workspaces/"+free+"/issues", "Bearer tth_x"); c != http.StatusNotFound {
		t.Errorf("token on a free workspace: %d", c)
	}
	if c := code("GET", "/api/workspaces/"+pro+"/issues", "Bearer tth_x"); c != http.StatusOK {
		t.Errorf("token on a pro workspace: %d", c)
	}
	if c := code("GET", "/api/workspaces/"+free+"/issues", ""); c != http.StatusOK {
		t.Errorf("cookie on a free workspace: %d", c)
	}
	if c := code("GET", "/api/me/tokens", ""); c != http.StatusOK {
		t.Errorf("tokens page with the API on for pro: %d", c)
	}
	set("feature_api_plans", "")
	if c := code("GET", "/api/me", "Bearer tth_x"); c != http.StatusNotFound {
		t.Errorf("token outside a workspace with the API off: %d", c)
	}
	if c := code("GET", "/api/me/tokens", ""); c != http.StatusNotFound {
		t.Errorf("tokens page with the API off: %d", c)
	}

	// The workspace from the feedback form's and the git hook's token.
	set("feature_feedback_plans", "pro")
	set("feature_integrations_plans", "pro")
	for _, c := range []struct {
		path string
		want int
	}{
		{"/api/feedback-forms/" + pro, http.StatusOK},
		{"/api/feedback-forms/" + free, http.StatusNotFound},
		{"/api/hooks/github/" + pro, http.StatusOK},
		{"/api/hooks/github/" + free, http.StatusNotFound},
	} {
		if got := code("POST", c.path, ""); got != c.want {
			t.Errorf("%s: %d, want %d", c.path, got, c.want)
		}
	}

	// Files off: the project file list is the feature, issue attachments (same endpoints) are core.
	set("feature_files_plans", "")
	if c := code("GET", "/api/workspaces/"+pro+"/projects/ENG/files", ""); c != http.StatusNotFound {
		t.Errorf("project files with files off: %d", c)
	}
	if c := code("GET", "/api/workspaces/"+pro+"/projects/ENG/files?issue=x", ""); c != http.StatusOK {
		t.Errorf("issue attachments with files off: %d", c)
	}
	if c := code("POST", "/api/workspaces/"+pro+"/projects/ENG/files", ""); c != http.StatusOK {
		t.Errorf("attachment upload with files off: %d", c)
	}
}
