package wsaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
)

// TestRecordRollsBack: an entry recorded in a transaction that rolls back is gone with it.
func TestRecordRollsBack(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var wsID string
	if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by) values ('Audit', $1, $2) returning id::text`,
		dbtest.Name("audit-"), ann).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := Record(ctx, tx, wsID, ann, "role.deleted", "Triage", nil); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if es, _, err := List(ctx, pool, wsID, ann, "", nil); err != nil || len(es) != 0 {
		t.Fatalf("after rollback: %+v %v", es, err)
	}
	if err := Record(ctx, pool, wsID, ann, "role.deleted", "Triage", nil); err != nil {
		t.Fatal(err)
	}
	if es, _, err := List(ctx, pool, wsID, ann, "", nil); err != nil || len(es) != 1 || es[0].Actor.Name != "Ann" {
		t.Fatalf("after commit: %+v %v", es, err)
	}
}

// A private project's entries show only to who can see the project, and once it's deleted only to admins.
func TestProjectVisibility(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, projectID string
	err := pool.QueryRow(ctx, `
		with w as (insert into workspaces (name, slug, created_by) values ('Audit', $1, $2) returning id),
		     a as (insert into workspace_roles (workspace_id, name, builtin) select id, 'Admin', 'admin' from w returning id, workspace_id),
		     m as (insert into workspace_roles (workspace_id, name, builtin, permissions)
		           select id, 'Member', 'member', '{audit.view}' from w returning id, workspace_id),
		     wm as (insert into workspace_members (workspace_id, user_id, role_id)
		            select workspace_id, $2, id from a union all select workspace_id, $3, id from m),
		     p as (insert into projects (workspace_id, name, key, private, created_by) select id, 'Secret', 'SEC', true, $2 from w returning id)
		select (select id::text from w), (select id::text from p)`, dbtest.Name("audit-"), ann, bob).Scan(&wsID, &projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, pool, wsID, ann, "project.created", "SEC", map[string]any{"name": "Secret"}); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, pool, wsID, ann, "role.deleted", "Triage", nil); err != nil {
		t.Fatal(err)
	}
	seen := func(viewer string) int {
		es, _, err := List(ctx, pool, wsID, viewer, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return len(es)
	}
	if a, b := seen(ann), seen(bob); a != 2 || b != 1 {
		t.Fatalf("private project: admin sees %d, non-member sees %d; want 2 and 1", a, b)
	}
	if _, err := pool.Exec(ctx, `insert into project_members (project_id, user_id) values ($1, $2)`, projectID, bob); err != nil {
		t.Fatal(err)
	}
	if b := seen(bob); b != 2 {
		t.Fatalf("project member sees %d, want 2", b)
	}
	if _, err := pool.Exec(ctx, `delete from projects where id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	if a, b := seen(ann), seen(bob); a != 2 || b != 1 {
		t.Fatalf("deleted project: admin sees %d, member sees %d; want 2 and 1", a, b)
	}
}
