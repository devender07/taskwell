package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// weeklySection names the weekly email's sections, in order; weeklyLine.section indexes it.
var weeklySection = [...]struct{ heading, count string }{
	{"Closed last week", "closed"}, {"Due", "due"}, {"Blocked", "blocked"}, {"Blocking others", "blocking"},
}

// weeklyLines caps each section of a workspace.
const weeklyLines = 15

type weeklyLine struct {
	workspace, slug   string
	section           int
	identifier, title string
	note              string // "overdue since Nov 3", "due Thu Nov 12", "waiting on API-9", "blocking WEB-5"; "" when closed
}

// weeklyFrom is the lines of user $1's weekly email at $2 in timezone $3: their visible live issues in active projects
// that closed in the 7 days before $2 and are still completed, or are open and due before a week from today, waiting
// on an open blocker or blocking someone else's open issue. Blockers and blocked issues must be live, in an active
// project of the same workspace the user can see.
const weeklyFrom = `with mine as (
	  select i.id, i.workspace_id, i.due_date, p.key || '-' || i.number ident, i.title, p.key, i.number,
	    st.category = 'completed' done, st.category not in ('completed', 'canceled') open
	  from issues i join projects p on p.id = i.project_id join statuses st on st.id = i.status_id
	  join workspace_members m on m.workspace_id = i.workspace_id and m.user_id = $1
	  where i.assignee_id = $1 and p.archived_at is null and can_see_project(p.id, $1)),
	 day as (select ($2::timestamptz at time zone $3)::date today),
	 lines as (
	  select mine.*, 0 sec, '' note from mine where done and exists (select 1 from issue_events e
	    where e.issue_id = mine.id and e.kind = 'status' and e.data->'to'->>'category' = 'completed'
	      and e.created_at > $2::timestamptz - interval '7 days' and e.created_at <= $2)
	  union all
	  select mine.*, 1, case when due_date < today then 'overdue since ' || to_char(due_date, 'Mon FMDD')
	    else 'due ' || to_char(due_date, 'Dy Mon FMDD') end
	  from mine, day where open and due_date < today + 7
	  union all
	  select mine.*, 2, 'waiting on ' || o.idents from mine cross join lateral (
	    select string_agg(p.key || '-' || r.number, ', ' order by p.key, r.number) idents
	    from issue_links l join issues r on r.id = l.blocker_id join projects p on p.id = r.project_id
	    join statuses st on st.id = r.status_id
	    where l.blocked_id = mine.id and r.workspace_id = mine.workspace_id and st.category not in ('completed', 'canceled')
	      and p.archived_at is null and can_see_project(p.id, $1)) o
	  where open and o.idents is not null
	  union all
	  select mine.*, 3, 'blocking ' || o.idents from mine cross join lateral (
	    select string_agg(p.key || '-' || r.number, ', ' order by p.key, r.number) idents
	    from issue_links l join issues r on r.id = l.blocked_id join projects p on p.id = r.project_id
	    join statuses st on st.id = r.status_id
	    where l.blocker_id = mine.id and r.workspace_id = mine.workspace_id and r.assignee_id <> $1
	      and st.category not in ('completed', 'canceled') and p.archived_at is null and can_see_project(p.id, $1)) o
	  where open and o.idents is not null)
	select w.name, w.slug, l.sec, l.ident, l.title, l.note from lines l join workspaces w on w.id = l.workspace_id
	order by w.name, w.id, l.sec, l.key, l.number`

// weeklyNext locks a user whose weekly email (Monday at hour in their timezone, this week as of now) is due, queues it
// with queue in the same transaction when they want it and it has lines, and records the week as sent either way. It
// returns false when nobody is due.
func (m *model) weeklyNext(ctx context.Context, now time.Time, hour int,
	queue func(tx pgx.Tx, to string, lines []weeklyLine) error) (bool, error) {
	// Members with no settings row have the defaults; a row lets them be locked and the send recorded. Starting at
	// now, like the column default, their first one comes next Monday.
	if _, err := m.db.Exec(ctx,
		`insert into notification_settings (user_id, last_weekly_at)
		 select u.id, $1 from users u where not exists (select 1 from notification_settings s where s.user_id = u.id)
		   and exists (select 1 from workspace_members m where m.user_id = u.id)
		 on conflict do nothing`, now); err != nil {
		return false, fmt.Errorf("add weekly settings: %w", err)
	}
	var userID string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var to, tz string
		var wants bool
		err := tx.QueryRow(ctx,
			`select s.user_id::text, u.email, z.tz,
			   s.weekly and s.email <> 'off' and u.email_verified_at is not null and u.suspended_at is null
			 from notification_settings s join users u on u.id = s.user_id
			 cross join lateral (select coalesce(s.timezone, 'UTC') tz) z
			 cross join lateral (select (date_trunc('week', $1::timestamptz at time zone z.tz) + make_interval(hours => $2))
			   at time zone z.tz) d(at)
			 where $1 >= d.at and (s.last_weekly_at is null or s.last_weekly_at < d.at)
			 limit 1 for update of s skip locked`, now, hour).Scan(&userID, &to, &tz, &wants)
		if errors.Is(err, pgx.ErrNoRows) {
			userID = ""
			return nil
		}
		if err != nil {
			return err
		}
		if wants {
			rows, err := tx.Query(ctx, weeklyFrom, userID, now, tz)
			if err != nil {
				return err
			}
			lines, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (weeklyLine, error) {
				var l weeklyLine
				return l, r.Scan(&l.workspace, &l.slug, &l.section, &l.identifier, &l.title, &l.note)
			})
			if err != nil {
				return err
			}
			if len(lines) > 0 {
				if err := queue(tx, to, lines); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(ctx, `update notification_settings set last_weekly_at = $2 where user_id = $1`, userID, now)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("queue weekly email: %w", err)
	}
	return userID != "", nil
}

// weeklyEmail returns the subject and body of one user's weekly email; lines is ordered by workspace, then section.
func weeklyEmail(lines []weeklyLine, base string) (subject, body string) {
	var totals [len(weeklySection)]int
	for _, l := range lines {
		totals[l.section]++
	}
	var counts []string
	for i, n := range totals {
		if n > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", n, weeklySection[i].count))
		}
	}
	var b strings.Builder
	for i := 0; i < len(lines); {
		l := lines[i]
		if i == 0 || l.slug != lines[i-1].slug {
			fmt.Fprintf(&b, "\n%s\n", l.workspace)
		}
		n := 1 // the run of lines in l's workspace and section
		for i+n < len(lines) && lines[i+n].slug == l.slug && lines[i+n].section == l.section {
			n++
		}
		fmt.Fprintf(&b, "\n%s (%d)\n", weeklySection[l.section].heading, n)
		for _, o := range lines[i : i+min(n, weeklyLines)] {
			line := o.identifier + " " + o.title
			if o.note != "" {
				line += ", " + o.note
			}
			fmt.Fprintf(&b, "- %s  %s\n", line, issueURL(base, message{slug: o.slug, identifier: o.identifier}))
		}
		if n > weeklyLines {
			fmt.Fprintf(&b, "- and %d more  %s/%s/my-issues\n", n-weeklyLines, strings.TrimRight(base, "/"), l.slug)
		}
		i += n
	}
	return "Your week in Taskwell: " + strings.Join(counts, ", "), strings.Trim(b.String(), "\n") + footer(base, lines[0].slug)
}
