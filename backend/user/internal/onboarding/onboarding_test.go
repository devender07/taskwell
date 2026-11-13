package onboarding

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestOnboarding: the steps flip to done as the workspace gets a project, a teammate, an issue and a GitHub hook;
// invite is left out on Free and github while integrations are off; a plain member can't dismiss, an admin can,
// once in the audit log; guests get 403 and outsiders 404.
func TestOnboarding(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, gus, cy := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Gus"), dbtest.User(t, pool, "Cy")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, gus, "guest")
	integrations := true
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, sysconfig.New(pool)).Member,
		func(context.Context, string) bool { return integrations }).Register(mux)
	api := "/api/workspaces/" + slug + "/onboarding"
	type answer struct {
		Dismissed bool `json:"dismissed"`
		Steps     []step
	}
	get := func() answer {
		t.Helper()
		code, body := dbtest.Do(t, mux, ann, "GET", api, nil)
		var a answer
		if err := json.Unmarshal([]byte(body), &a); code != 200 || err != nil {
			t.Fatalf("get: %d %s %v", code, body, err)
		}
		return a
	}
	want := func(dismissed bool, steps ...step) {
		t.Helper()
		a := get()
		if a.Dismissed != dismissed || len(a.Steps) != len(steps) {
			t.Fatalf("got %+v, want %v %+v", a, dismissed, steps)
		}
		for i := range steps {
			if a.Steps[i] != steps[i] {
				t.Fatalf("got %+v, want %+v", a.Steps, steps)
			}
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	status := func(user, method, path string, want int) {
		t.Helper()
		if code, body := dbtest.Do(t, mux, user, method, path, nil); code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
	}

	want(false, step{"project", false}, step{"issue", false}, step{"github", false})
	dbtest.SetPlan(t, pool, ann, "starter")
	want(false, step{"project", false}, step{"invite", true}, step{"issue", false}, step{"github", false}) // Gus is a member
	exec(`delete from workspace_members where workspace_id = $1 and user_id = $2`, wsID, gus)
	want(false, step{"project", false}, step{"invite", false}, step{"issue", false}, step{"github", false})
	exec(`insert into workspace_invites (workspace_id, email, token_hash, expires_at) values ($1, 'x@dbtest.test', '\x00', now() - interval '1 day')`, wsID)
	want(false, step{"project", false}, step{"invite", false}, step{"issue", false}, step{"github", false}) // expired
	dbtest.Join(t, pool, wsID, bob, "member")
	dbtest.Join(t, pool, wsID, gus, "guest")
	exec(`insert into projects (workspace_id, name, key) values ($1, 'Web', 'WEB')`, wsID)
	exec(`insert into issues (workspace_id, project_id, number, title, status_id, rank)
		select $1, p.id, 1, 'First', (select id from statuses where workspace_id = $1 order by position limit 1), 'V'
		from projects p where p.workspace_id = $1`, wsID)
	exec(`insert into git_hooks (workspace_id, provider, token, secret) values ($1, 'github', $2, 's')`, wsID, dbtest.Name("tok"))
	want(false, step{"project", true}, step{"invite", true}, step{"issue", true}, step{"github", true})
	integrations = false
	want(false, step{"project", true}, step{"invite", true}, step{"issue", true})

	status(gus, "GET", api, 403)
	status(cy, "GET", api, 404)
	status(bob, "GET", api, 200)
	status(bob, "POST", api+"/dismiss", 403)
	status(gus, "POST", api+"/dismiss", 403)
	status(cy, "POST", api+"/dismiss", 404)
	status(ann, "POST", api+"/dismiss", 204)
	status(ann, "POST", api+"/dismiss", 204)
	want(true, step{"project", true}, step{"invite", true}, step{"issue", true})
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from workspace_audit where workspace_id = $1 and action = 'workspace.onboarding_dismissed'
		and actor_id = $2 and target = $3`, wsID, ann, slug).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit entries %d %v", n, err)
	}
}
