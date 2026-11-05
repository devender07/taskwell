package project

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestDelete: only an archived project can be deleted, by someone with projects.manage who can see it, and it takes
// its milestones with it.
func TestDelete(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	do := func(user, method, path string, form url.Values, want int) {
		t.Helper()
		if code, body := dbtest.Do(t, mux, user, method, "/api/workspaces/"+slug+path, form); code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
	}

	do(ann, "POST", "/projects", url.Values{"name": {"Web"}, "key": {"WEB"}}, 201)
	do(ann, "POST", "/projects/WEB/milestones", url.Values{"name": {"Beta"}, "date": {"2026-12-01"}}, 201)
	do(ann, "DELETE", "/projects/WEB", nil, 409) // archive first
	do(ann, "POST", "/projects/WEB/archive", nil, 200)
	do(bob, "DELETE", "/projects/WEB", nil, 403) // no projects.manage
	do(ann, "DELETE", "/projects/WEB", nil, 204)
	do(ann, "GET", "/projects/WEB", nil, 404)
	do(ann, "DELETE", "/projects/WEB", nil, 404)
	var left int
	if err := pool.QueryRow(ctx, `select count(*) from project_milestones m join projects p on p.id = m.project_id
		where p.workspace_id = $1`, wsID).Scan(&left); err != nil || left != 0 {
		t.Errorf("milestones left %d %v", left, err)
	}
}
