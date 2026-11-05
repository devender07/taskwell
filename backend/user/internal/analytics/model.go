package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TeamMember is one row of the team page.
type TeamMember struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Open          int      `json:"open"`
	Overdue       int      `json:"overdue"`
	Completed30d  int      `json:"completed_30d"`
	CycleTimeDays *float64 `json:"cycle_time_days"`
}

type model struct{ db *pgxpool.Pool }

// Dates are UTC. since is the Monday starting the oldest of the 12 weeks (a timestamp), sinceTZ the same instant;
// sunday ends this week.
const (
	today   = `(now() at time zone 'utc')::date`
	since   = `(date_trunc('week', now() at time zone 'utc') - interval '11 weeks')`
	sinceTZ = `(` + since + ` at time zone 'utc')`
	sunday  = `(date_trunc('week', now() at time zone 'utc')::date + 6)`
	ago30d  = `now() - interval '30 days'`
	week    = `date_trunc('week', %s at time zone 'utc')`

	// vis is workspace $1's projects that viewer $2 can see.
	vis = `vis as (select id from projects where workspace_id = $1 and can_see_project(id, $2))`

	// openFrom is the open issues of workspace $1 in visible, unarchived projects.
	openFrom = `from issues i join statuses s on s.id = i.status_id join projects p on p.id = i.project_id
		where i.workspace_id = $1 and i.project_id in (select id from vis) and p.archived_at is null
		  and s.category in ('backlog', 'unstarted', 'started')`

	// medianDays is the median cycle time of done's rows in days, one decimal; null when none were started.
	medianDays = `round(extract(epoch from percentile_cont(0.5) within group (order by done_at - started_at)) / 86400, 1)`
)

// done is a CTE of the issues of workspace $1 in visible projects still in a completed status, each with its assignee,
// its latest completion in the 12 weeks (done_at) and its first start before that (started_at, null when never
// started); and narrows the issues i. A status change always bumps updated_at, so updated_at >= since skips issues
// completed long ago without reading their events.
func done(and string) string {
	return `done as (
		select distinct on (i.id) i.id, i.assignee_id, e.created_at done_at,
		  (select min(st.created_at) from issue_events st where st.issue_id = i.id and st.kind = 'status'
		     and st.data->'to'->>'category' = 'started' and st.created_at <= e.created_at) started_at
		from issues i join statuses s on s.id = i.status_id join issue_events e on e.issue_id = i.id
		where i.workspace_id = $1 and i.project_id in (select id from vis) and s.category = 'completed'
		  and i.updated_at >= ` + sinceTZ + ` and e.kind = 'status' and e.data->'to'->>'category' = 'completed'
		  and e.created_at >= ` + sinceTZ + ` ` + and + `
		order by i.id, e.created_at desc)`
}

// member returns member $3's analytics in workspace $1 as viewer $2 sees them, as the API's JSON; nil when they
// aren't a member.
func (m *model) member(ctx context.Context, wsID, viewerID, memberID string) (json.RawMessage, error) {
	var out json.RawMessage
	err := m.db.QueryRow(ctx, `with `+vis+`, `+done(`and i.assignee_id = $3`)+`,
		created as (select `+fmt.Sprintf(week, "i.created_at")+` wk, count(*) n from issues i
		  where i.workspace_id = $1 and i.created_by = $3 and i.created_at >= `+sinceTZ+`
		    and i.project_id in (select id from vis) group by 1),
		commented as (select `+fmt.Sprintf(week, "c.created_at")+` wk, count(*) n
		  from comments c join issues i on i.id = c.issue_id
		  where c.author_id = $3 and c.created_at >= `+sinceTZ+` and i.workspace_id = $1
		    and i.project_id in (select id from vis) group by 1),
		completed as (select `+fmt.Sprintf(week, "done_at")+` wk, count(*) n from done group by 1)
		select json_build_object(
		  'member', json_build_object('id', u.id, 'name', u.name, 'email', u.email,
		    'role', json_build_object('id', r.id, 'name', r.name)),
		  'open', (select json_build_object('total', count(*),
		      'by_category', json_build_object('backlog', count(*) filter (where s.category = 'backlog'),
		        'unstarted', count(*) filter (where s.category = 'unstarted'),
		        'started', count(*) filter (where s.category = 'started')),
		      'overdue', count(*) filter (where i.due_date < `+today+`),
		      'due_this_week', count(*) filter (where i.due_date between `+today+` and `+sunday+`))
		    `+openFrom+` and i.assignee_id = $3),
		  'weeks', (select json_agg(json_build_object('week', to_char(w, 'YYYY-MM-DD'), 'completed', coalesce(d.n, 0),
		      'created', coalesce(c.n, 0), 'comments', coalesce(k.n, 0)) order by w)
		    from generate_series(`+since+`, `+since+` + interval '11 weeks', interval '1 week') w
		    left join completed d on d.wk = w left join created c on c.wk = w left join commented k on k.wk = w),
		  'cycle_time_days', (select `+medianDays+` from done),
		  'completed_30d', (select count(*) from done where done_at >= `+ago30d+`),
		  'tests', json_build_object('runs_started_30d', (select count(*) from test_runs t join issues i on i.id = t.issue_id
		    where t.started_by = $3 and t.workspace_id = $1 and t.created_at >= `+ago30d+`
		      and i.project_id in (select id from vis))),
		  'current_cycles', (select coalesce(json_agg(json_build_object('project', json_build_object('key', p.key, 'name', p.name),
		      'number', cy.number, 'name', coalesce(cy.name, 'Cycle ' || cy.number), 'assigned', x.assigned,
		      'completed', x.completed) order by p.key, cy.number), '[]')
		    from (select i.cycle_id, count(*) assigned, count(*) filter (where s.category = 'completed') completed
		          from issues i join statuses s on s.id = i.status_id
		          where i.workspace_id = $1 and i.assignee_id = $3 and i.cycle_id is not null
		            and i.project_id in (select id from vis) group by 1) x
		    join cycles cy on cy.id = x.cycle_id join projects p on p.id = cy.project_id
		    where cy.starts_on <= `+today+` and cy.ends_on >= `+today+`))
		from workspace_members m join users u on u.id = m.user_id join workspace_roles r on r.id = m.role_id
		where m.workspace_id = $1 and m.user_id = $3`, wsID, viewerID, memberID).Scan(&out)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("member analytics: %w", err)
	}
	return out, nil
}

// team returns every member of workspace $1 by name with their numbers as viewer $2 sees them.
func (m *model) team(ctx context.Context, wsID, viewerID string) ([]TeamMember, error) {
	rows, err := m.db.Query(ctx, `with `+vis+`, `+done(`and i.assignee_id is not null`)+`,
		open as (select i.assignee_id uid, count(*) n, count(*) filter (where i.due_date < `+today+`) overdue
		  `+openFrom+` and i.assignee_id is not null group by 1),
		finished as (select assignee_id uid, count(*) filter (where done_at >= `+ago30d+`) n, `+medianDays+` days
		  from done group by 1)
		select u.id::text, u.name, coalesce(o.n, 0), coalesce(o.overdue, 0), coalesce(f.n, 0), f.days
		from workspace_members m join users u on u.id = m.user_id
		  left join open o on o.uid = m.user_id left join finished f on f.uid = m.user_id
		where m.workspace_id = $1
		order by lower(u.name), u.email`, wsID, viewerID)
	if err != nil {
		return nil, fmt.Errorf("team analytics: %w", err)
	}
	ms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (TeamMember, error) {
		var t TeamMember
		err := row.Scan(&t.ID, &t.Name, &t.Open, &t.Overdue, &t.Completed30d, &t.CycleTimeDays)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("team analytics: %w", err)
	}
	return ms, nil
}
