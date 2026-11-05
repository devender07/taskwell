package issue

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestAssignedBy: null when unassigned, the creator for an issue created assigned, then the last assignee change's actor.
func TestAssignedBy(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var id string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Engineering', 'ENG' from w returning id),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1)
			insert into issues (workspace_id, project_id, number, title, status_id, rank, created_by)
			select (select id from w), (select id from p), 1, 'One', (select id from s), 'V', $1 returning id::text`, ann).Scan(&id)
	}); err != nil {
		t.Fatal(err)
	}
	m := &model{db: pool}
	by := func() string {
		t.Helper()
		i, err := m.byID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if i.AssignedBy == nil {
			return ""
		}
		return i.AssignedBy.Name
	}
	if got := by(); got != "" {
		t.Fatalf("unassigned: assigned_by %q, want none", got)
	}
	if _, err := pool.Exec(ctx, `update issues set assignee_id = $2 where id = $1`, id, bob); err != nil {
		t.Fatal(err)
	}
	if got := by(); got != "Ann" {
		t.Fatalf("no assignee event: assigned_by %q, want the creator", got)
	}
	if _, err := pool.Exec(ctx, `insert into issue_events (issue_id, actor_id, kind) values ($1, $2, 'assignee')`, id, bob); err != nil {
		t.Fatal(err)
	}
	if got := by(); got != "Bob" {
		t.Fatalf("after Bob's change: assigned_by %q, want Bob", got)
	}
}
