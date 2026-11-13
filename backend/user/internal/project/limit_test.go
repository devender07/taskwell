package project

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestProjectLimit: projects are limited by the workspace owner's plan, archived ones included: a 2nd is a 402 on
// Free, a 6th on Starter, none on Pro. A member creating one counts against the owner's plan, not their own.
func TestProjectLimit(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "admin")
	dbtest.SetPlan(t, pool, mia, "advanced")
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	create := func(user string, want int, msg string) {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, "POST", "/api/workspaces/"+slug+"/projects",
			url.Values{"name": {"Web"}, "key": {strings.ToUpper(dbtest.Name("P"))[:5]}})
		if code != want || !strings.Contains(body, msg) {
			t.Fatalf("create: %d %s, want %d %q", code, body, want, msg)
		}
	}
	create(ann, 201, "")
	if _, err := pool.Exec(ctx, `update projects set archived_at = now() where workspace_id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	create(mia, 402, "This workspace's plan allows 1 project. Its owner can upgrade for more.")
	dbtest.SetPlan(t, pool, ann, "starter")
	for range 4 {
		create(mia, 201, "")
	}
	create(ann, 402, "This workspace's plan allows 5 projects. Its owner can upgrade for more.")
	dbtest.SetPlan(t, pool, ann, "pro")
	create(ann, 201, "")
}
