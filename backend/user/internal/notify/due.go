package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

// remindDue adds the due-date reminders due at now, once that day's hour has passed in the assignee's timezone: a
// due_soon the day before an open issue's due date and an overdue on the 7 days after it. One of each per issue,
// assignee and due date. The assignee must be an unsuspended member who can see the live issue's active project.
// It tells the recipients' tabs and returns how many it added.
func (m *model) remindDue(ctx context.Context, now time.Time, hour int) (int, error) {
	var n int
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		// Two instances in the same minute would both see no reminder yet.
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('notify.remindDue'))`); err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`with due as (
			   select i.id, i.workspace_id, i.assignee_id, i.due_date,
			     case when l.today = i.due_date - 1 then 'due_soon'
			       when l.today > i.due_date and l.today <= i.due_date + 7 then 'overdue' end kind
			   from issues i join projects p on p.id = i.project_id join statuses st on st.id = i.status_id
			   join workspace_members wm on wm.workspace_id = i.workspace_id and wm.user_id = i.assignee_id
			   join users u on u.id = i.assignee_id
			   left join notification_settings s on s.user_id = i.assignee_id
			   cross join lateral (select $1::timestamptz at time zone coalesce(s.timezone, 'UTC') lt) z
			   cross join lateral (select z.lt::date today) l
			   where i.due_date between ($1::timestamptz - interval '9 days')::date and ($1::timestamptz + interval '2 days')::date
			     and p.archived_at is null and st.category not in ('completed', 'canceled')
			     and u.suspended_at is null and extract(hour from z.lt) >= $2 and can_see_project(p.id, i.assignee_id)),
			 added as (
			   insert into notifications (user_id, workspace_id, issue_id, kind, data)
			   select d.assignee_id, d.workspace_id, d.id, d.kind, jsonb_build_object('due_date', d.due_date)
			   from due d where d.kind is not null and not exists (select 1 from notifications t
			     where t.user_id = d.assignee_id and t.issue_id = d.id and t.kind = d.kind and t.data ->> 'due_date' = d.due_date::text)
			   returning user_id, workspace_id)
			 select a.user_id::text, w.slug, count(*) from added a join workspaces w on w.id = a.workspace_id
			 group by a.user_id, w.slug`, now, hour)
		if err != nil {
			return err
		}
		type added struct {
			user, slug string
			n          int
		}
		as, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (added, error) {
			var a added
			return a, r.Scan(&a.user, &a.slug, &a.n)
		})
		if err != nil {
			return err
		}
		n = 0
		for _, a := range as {
			n += a.n
			if err := realtime.Publish(ctx, tx, changed(a.slug, "", a.user)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("add due reminders: %w", err)
	}
	return n, nil
}
