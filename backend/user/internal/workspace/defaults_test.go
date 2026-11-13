package workspace

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
)

// TestWorkspaceDefaults edits the defaults (a custom role, a type, reordered statuses, a label and a template using
// them), creates a workspace and finds exactly those in it with their references mapped, while an existing workspace
// keeps what it had. A missing Guest default and empty default statuses fall back to the built-in ones. Everything
// runs in a transaction that rolls back, so the shared defaults other tests see never change.
func TestWorkspaceDefaults(t *testing.T) {
	p := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, p, "Ann")
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // the point: nothing commits
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	// rows reads one line per row of a query on the workspace.
	rows := func(sql, wsID string) []string {
		t.Helper()
		rs, err := tx.Query(ctx, sql, wsID)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(rs, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	const (
		rolesQ    = `select name || ' ' || coalesce(builtin, '-') || ' ' || permissions::text from workspace_roles where workspace_id = $1 order by name`
		typesQ    = `select name || ' ' || category || ' ' || icon || ' ' || coalesce(color, '') from status_types where workspace_id = $1 order by name`
		statusesQ = `select s.name || ' ' || s.category || ' ' || coalesce(t.name, '-') from statuses s left join status_types t on t.id = s.type_id
			where s.workspace_id = $1 order by s.position`
		labelsQ    = `select name || ' ' || color from labels where workspace_id = $1 order by name`
		templatesQ = `select t.name || ' ' || t.title || ' ' || t.priority || ' ' || coalesce(s.name, '-') || ' ' ||
			  (select coalesce(string_agg(l.name, ',' order by l.name), '') from labels l where l.id = any(t.label_ids) and l.workspace_id = t.workspace_id) ||
			  ' ' || cardinality(t.label_ids) || ' ' || (t.description ->> 'type') || ' ' || (t.project_id is null and t.created_by is null and t.repeat_every is null)
			from issue_templates t left join statuses s on s.id = t.status_id and s.workspace_id = t.workspace_id where t.workspace_id = $1 order by t.name`
	)
	snapshot := func(wsID string) string {
		return fmt.Sprint(rows(rolesQ, wsID), rows(typesQ, wsID), rows(statusesQ, wsID), rows(labelsQ, wsID), rows(templatesQ, wsID))
	}
	check := func(what string, got, want []string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", what, got, want)
		}
	}

	old, err := create(ctx, tx, ann, "Old", dbtest.Name("defaults-old-"))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(old.ID)
	check("seeded statuses", rows(statusesQ, old.ID), []string{"Backlog backlog -", "Todo unstarted -", "In progress started -",
		"In review started -", "Done completed -", "Cancelled canceled -"})

	exec(`delete from default_templates`)
	exec(`delete from default_labels`)
	exec(`delete from default_statuses`)
	exec(`delete from default_status_types`)
	exec(`update default_roles set permissions = '{comments.create}' where builtin = 'guest'`)
	exec(`insert into default_roles (name, permissions) values ('QA', '{issues.status_any}')`)
	exec(`with t as (insert into default_status_types (name, category, icon, color) values ('In review', 'started', 'eye', '#5e6ad2') returning id)
		insert into default_statuses (name, category, type_id, position)
		select 'Todo', 'unstarted', null, 0 union all select 'Review', 'started', t.id, 1 from t union all select 'Done', 'completed', null, 2`)
	exec(`update default_statuses set position = case name when 'Review' then 0 when 'Todo' then 1 else 2 end`) // reorder
	exec(`insert into default_labels (name, color) values ('Bug', '#eb5757'), ('Gone', '#000000')`)
	exec(`insert into default_templates (name, title, description, status_id, priority, label_ids)
		select 'Bug report', 'Bug:', '{"type":"doc"}', (select id from default_statuses where name = 'Review'), 2,
		       array(select id from default_labels order by name)`)
	exec(`delete from default_labels where name = 'Gone'`) // left in label_ids: dropped when copied

	ws, err := create(ctx, tx, ann, "New", dbtest.Name("defaults-new-"))
	if err != nil {
		t.Fatal(err)
	}
	check("roles", rows(rolesQ, ws.ID), []string{"Admin admin {}", "Guest guest {comments.create}",
		"Member member {issues.create,comments.create}", "QA - {issues.status_any}"})
	check("types", rows(typesQ, ws.ID), []string{"In review started eye #5e6ad2"})
	check("statuses", rows(statusesQ, ws.ID), []string{"Review started In review", "Todo unstarted -", "Done completed -"})
	check("labels", rows(labelsQ, ws.ID), []string{"Bug #eb5757"})
	check("templates", rows(templatesQ, ws.ID), []string{"Bug report Bug: 2 Review Bug 1 doc true"})
	if after := snapshot(old.ID); after != before {
		t.Errorf("existing workspace changed:\n%s\nwas\n%s", after, before)
	}

	exec(`delete from default_roles where builtin = 'guest'`)
	exec(`delete from default_statuses`)
	ws, err = create(ctx, tx, ann, "Fallback", dbtest.Name("defaults-fallback-"))
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(rolesQ, ws.ID); !slices.Contains(got, "Guest guest {"+strings.Join(memberDefaults, ",")+"}") {
		t.Errorf("roles without a Guest default = %q", got)
	}
	check("fallback statuses", rows(statusesQ, ws.ID), []string{"Backlog backlog -", "Todo unstarted -", "In progress started -",
		"Done completed -", "Cancelled canceled -"})
}
