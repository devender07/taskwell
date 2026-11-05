package project_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/aitest"
	"github.com/devender07/taskwell/backend/user/internal/analytics"
	"github.com/devender07/taskwell/backend/user/internal/chat"
	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/feedback"
	"github.com/devender07/taskwell/backend/user/internal/issue"
	"github.com/devender07/taskwell/backend/user/internal/project"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestGuestSees: a guest sees only the projects they're a member of, public or not, and nothing workspace-wide.
// Ann is the admin, Mia a member, Gus a guest added to WEB; OPS is public and SEC private, neither has Gus.
func TestGuestSees(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, gus := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Gus")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Web', 'WEB', false), ('Ops', 'OPS', false), ('Sec', 'SEC', true)) v(n, k, pr))
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, gus, "guest")
	if _, err := pool.Exec(ctx, `insert into chats (workspace_id, kind) values ($1, 'workspace') on conflict do nothing`, wsID); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	ws := workspace.New(pool, dbtest.Auth, nil, nil)
	ws.Register(mux)
	project.New(pool, dbtest.Auth, ws.Member).Register(mux)
	issue.New(pool, dbtest.Auth, ws.Member).Register(mux)
	chat.New(pool, dbtest.Auth, ws.Member).Register(mux)
	feedback.New(pool, dbtest.Auth, ws.Member).Register(mux)
	analytics.New(pool, dbtest.Auth, ws.Member).Register(mux)
	aitest.New(pool, sysconfig.New(pool), aitest.Options{}, dbtest.Auth, ws.Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
		}
		return body
	}
	for _, key := range []string{"WEB", "OPS", "SEC"} {
		do(ann, "POST", "/projects/"+key+"/issues", url.Values{"title": {key + " secret plan"}}, 201)
	}
	do(ann, "POST", "/projects/WEB/members", url.Values{"user_ids": {mia}}, 409) // public: guests only
	do(ann, "POST", "/projects/WEB/members", url.Values{"user_ids": {gus}}, 200)

	if body := do(gus, "GET", "/projects", nil, 200); !strings.Contains(body, `"WEB"`) || strings.Contains(body, `"OPS"`) || strings.Contains(body, `"SEC"`) {
		t.Errorf("guest's projects: %s", body)
	}
	do(gus, "GET", "/projects/OPS", nil, 404)
	do(gus, "GET", "/issues/OPS-1", nil, 404)
	do(gus, "GET", "/issues/WEB-1", nil, 200)
	for _, path := range []string{"/my-issues", "/issues?q=secret", "/search?q=secret", "/activity", "/roadmap", "/project-stats"} {
		if body := do(gus, "GET", path, nil, 200); strings.Contains(body, "OPS") || strings.Contains(body, "SEC") {
			t.Errorf("guest's %s shows another project: %s", path, body)
		}
	}
	if body := do(gus, "GET", "/chats", nil, 200); strings.Contains(body, `"workspace"`) {
		t.Errorf("guest's chats show the workspace chat: %s", body)
	}
	var chatID string
	if err := pool.QueryRow(ctx, `select id::text from chats where workspace_id = $1 and kind = 'workspace'`, wsID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	do(gus, "GET", "/chats/"+chatID+"/messages", nil, 404)
	do(gus, "POST", "/chats/direct", url.Values{"user_id": {ann}}, 200)
	for _, path := range []string{"/feedback", "/analytics/team", "/members/" + gus + "/analytics", "/test-sites", "/test-accounts", "/roles"} {
		do(gus, "GET", path, nil, 403)
	}
	do(gus, "POST", "/projects", url.Values{"name": {"Mine"}, "key": {"MINE"}}, 403)
	do(gus, "POST", "/import/github", url.Values{"repo": {"a/b"}, "project": {"WEB"}}, 403)

	// Out of his last project, Gus stays in the workspace and sees nothing.
	do(ann, "DELETE", "/projects/WEB/members/"+gus, nil, 200)
	do(gus, "GET", "/issues/WEB-1", nil, 404)
	var ps []json.RawMessage
	if err := json.Unmarshal([]byte(do(gus, "GET", "/projects", nil, 200)), &ps); err != nil || len(ps) != 0 {
		t.Errorf("projects after removal: %d %v", len(ps), err)
	}
}
