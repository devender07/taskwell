package feature

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestRequireMFA: an admin without two-factor can't require it (400); with it they can, and it's audited. A member
// without two-factor then gets 403 mfa_required on an issue route but can still open the workspace and leave it.
func TestRequireMFA(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	slug := dbtest.Name("mfa-")
	mux := http.NewServeMux()
	workspace.New(pool, dbtest.Auth, nil, nil).Register(mux)
	mux.HandleFunc("/api/workspaces/{slug}/issues", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := New(sysconfig.New(pool), pool).RequireMFA(dbtest.Auth, mux)

	if code, body := dbtest.Do(t, h, ann, "POST", "/api/workspaces", url.Values{"name": {"Strict"}, "slug": {slug}}); code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	var wsID string
	if err := pool.QueryRow(ctx, `select id::text from workspaces where slug = $1`, slug).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	require := url.Values{"name": {"Strict"}, "slug": {slug}, "require_mfa": {"true"}}
	if code, body := dbtest.Do(t, h, ann, "POST", "/api/workspaces/"+slug, require); code != 400 ||
		!strings.Contains(body, `"require_mfa":"Turn on two-factor sign-in for your account first."`) {
		t.Fatalf("require without two-factor: %d %s", code, body)
	}
	if _, err := pool.Exec(ctx, `insert into user_mfa (user_id, secret, recovery_codes, enabled_at) values ($1, 'x', '{}', now())`, ann); err != nil {
		t.Fatal(err)
	}
	if code, body := dbtest.Do(t, h, ann, "POST", "/api/workspaces/"+slug, require); code != 200 || !strings.Contains(body, `"require_mfa":true`) {
		t.Fatalf("require with two-factor: %d %s", code, body)
	}
	var detail string
	if err := pool.QueryRow(ctx, `select detail::text from workspace_audit where workspace_id = $1 and action = 'workspace.require_mfa_changed'`,
		wsID).Scan(&detail); err != nil || detail != `{"to": true}` {
		t.Fatalf("audit: %q %v", detail, err)
	}

	for _, c := range []struct {
		who, method, path string
		want              int
	}{
		{ann, "GET", "/issues", 200},
		{bob, "GET", "/issues", 403},
		{bob, "POST", "", 403},
		{bob, "GET", "", 200},
		{bob, "GET", "/features", 404}, // not mounted here: the middleware let it through
		{bob, "DELETE", "/members/" + ann, 403},
		{bob, "DELETE", "/members/" + bob, 204},
	} {
		code, body := dbtest.Do(t, h, c.who, c.method, "/api/workspaces/"+slug+c.path, nil)
		if code != c.want {
			t.Fatalf("%s %s by %s: %d %s, want %d", c.method, c.path, c.who, code, body, c.want)
		}
		if code == 403 && !strings.Contains(body, `"mfa_required":true`) {
			t.Fatalf("%s %s: %s", c.method, c.path, body)
		}
		if c.method == "GET" && c.path == "" && !strings.Contains(body, `"mfa_blocked":true`) {
			t.Fatalf("workspace for a blocked member: %s", body)
		}
	}
}
