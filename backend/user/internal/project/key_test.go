package project

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/db"
	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestOldKeyKept: after ENG is renamed CORE, no other project can take ENG (old ENG-12 links would point at it), but
// CORE can go back to ENG.
func TestOldKeyKept(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var wsID, projectID string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id from workspaces where created_by = $1)
			insert into projects (workspace_id, name, key) select id, 'Eng', 'ENG' from w returning workspace_id::text, id::text`,
			ann).Scan(&wsID, &projectID)
	}); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) error { _, err := pool.Exec(ctx, sql, args...); return err }
	if err := exec(`update projects set key = 'CORE' where id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	if err := exec(`insert into projects (workspace_id, name, key) values ($1, 'New', 'ENG')`, wsID); !db.IsUniqueViolation(err) {
		t.Fatalf("taking an old key: %v, want a unique violation", err)
	}
	if err := exec(`update projects set key = 'ENG' where id = $1`, projectID); err != nil {
		t.Fatalf("taking back its own key: %v", err)
	}
}
