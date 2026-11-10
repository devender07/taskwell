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
	if es, _, err := List(ctx, pool, wsID, ann, 1, nil); err != nil || len(es) != 0 {
		t.Fatalf("after rollback: %+v %v", es, err)
	}
	if err := Record(ctx, pool, wsID, ann, "role.deleted", "Triage", nil); err != nil {
		t.Fatal(err)
	}
	if es, _, err := List(ctx, pool, wsID, ann, 1, nil); err != nil || len(es) != 1 || es[0].Actor.Name != "Ann" {
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
	if err := Record(ctx, pool, wsID, ann, "issue.purged", "SEC-4", map[string]any{"title": "Secret plan"}); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, pool, wsID, ann, "role.deleted", "Triage", nil); err != nil {
		t.Fatal(err)
	}
	seen := func(viewer string) int {
		es, _, err := List(ctx, pool, wsID, viewer, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		return len(es)
	}
	if a, b := seen(ann), seen(bob); a != 3 || b != 1 {
		t.Fatalf("private project: admin sees %d, non-member sees %d; want 3 and 1", a, b)
	}
	if _, err := pool.Exec(ctx, `insert into project_members (project_id, user_id) values ($1, $2)`, projectID, bob); err != nil {
		t.Fatal(err)
	}
	if b := seen(bob); b != 3 {
		t.Fatalf("project member sees %d, want 3", b)
	}
	if _, err := pool.Exec(ctx, `delete from projects where id = $1`, projectID); err != nil {
		t.Fatal(err)
	}
	if a, b := seen(ann), seen(bob); a != 3 || b != 1 {
		t.Fatalf("deleted project: admin sees %d, member sees %d; want 3 and 1", a, b)
	}
}

// TestAppendOnly: entries can't be changed or deleted, except the actor nulled when the user is deleted, entries over a
// year old (the sweep), and every entry when the workspace is deleted.
func TestAppendOnly(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID string
	if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by) values ('Audit', $1, $2) returning id::text`,
		dbtest.Name("audit-"), ann).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	if err := Record(ctx, pool, wsID, bob, "role.deleted", "Triage", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into workspace_audit (workspace_id, action, at) values ($1, 'old', now() - interval '13 months')`, wsID); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`update workspace_audit set target = 'x' where workspace_id = $1`,
		`update workspace_audit set actor_id = null, target = 'x' where workspace_id = $1 and actor_id is not null`,
		`delete from workspace_audit where workspace_id = $1 and action <> 'old'`,
	} {
		if _, err := pool.Exec(ctx, sql, wsID); err == nil {
			t.Errorf("%s: allowed", sql)
		}
	}
	if _, err := pool.Exec(ctx, `delete from workspace_audit where workspace_id = $1 and action = 'old'`, wsID); err != nil {
		t.Fatalf("sweeping a year-old entry: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from users where id = $1`, bob); err != nil {
		t.Fatalf("deleting the actor: %v", err)
	}
	var actor *string
	var name string
	if err := pool.QueryRow(ctx, `select actor_id::text, actor_name from workspace_audit where workspace_id = $1`, wsID).Scan(&actor, &name); err != nil ||
		actor != nil || name != "Bob" {
		t.Fatalf("after deleting the actor: %v %q %v", actor, name, err)
	}
	if _, err := pool.Exec(ctx, `delete from workspaces where id = $1`, wsID); err != nil {
		t.Fatalf("deleting the workspace: %v", err)
	}
}
