package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

type weekRow struct {
	Week                         string
	Completed, Created, Comments int
}

type memberAnalytics struct {
	Member struct{ ID, Name string }
	Open   struct {
		Total       int
		ByCategory  map[string]int `json:"by_category"`
		Overdue     int
		DueThisWeek int `json:"due_this_week"`
	}
	Weeks         []weekRow
	CycleTimeDays *float64 `json:"cycle_time_days"`
	Completed30d  int      `json:"completed_30d"`
	Tests         struct {
		RunsStarted30d int `json:"runs_started_30d"`
	}
	CurrentCycles []struct {
		Project             struct{ Key string }
		Number              int
		Name                string
		Assigned, Completed int
	} `json:"current_cycles"`
}

// TestAnalytics runs both endpoints against the scratch database (see dbtest). Ann is the admin, Bob a member outside
// the private LEAD project whose work is seeded, Cara a member whose custom role has analytics.view, Out not in the
// workspace. Event times are days before this week's Monday (M), UTC.
func TestAnalytics(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cara, out := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cara"),
		dbtest.User(t, pool, "Out")
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
	one := func(sql string, args ...any) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&v); err != nil {
			t.Fatalf("%v in %s", err, sql)
		}
		return v
	}
	one(`with r as (insert into workspace_roles (workspace_id, name, permissions) values ($1, 'Lead', '{analytics.view}') returning id)
		insert into workspace_members (workspace_id, user_id, role_id) select $1, $2, id from r returning user_id::text`, wsID, cara)
	project := func(key string, private, archived bool) string {
		return one(`insert into projects (workspace_id, name, key, private, archived_at)
			values ($1, $2, $2, $3, case when $4 then now() end) returning id::text`, wsID, key, private, archived)
	}
	web, lead, old := project("WEB", false, false), project("LEAD", true, false), project("OLD", false, true)
	cycle := one(`insert into cycles (workspace_id, project_id, number, starts_on, ends_on)
		values ($1, $2, 1, (now() at time zone 'utc')::date - 1, (now() at time zone 'utc')::date + 7) returning id::text`, wsID, web)
	n := 0
	// issue adds an issue Bob created and is assigned, in the first status of category, due in due days ("" for none).
	issue := func(projectID, category, due string, inCycle bool) string {
		n++
		return one(`insert into issues (workspace_id, project_id, number, title, status_id, assignee_id, created_by, due_date,
			  cycle_id, rank)
			select $1, $2, $3::int, 'Issue', (select id from statuses where workspace_id = $1 and category = $4 order by position limit 1),
			  $5, $5, (now() at time zone 'utc')::date + nullif($6, '')::int, case when $7 then $8::uuid end, 'V' || $3::int
			returning id::text`, wsID, projectID, n, category, bob, due, inCycle, cycle)
	}
	// moved logs a status event into category, days before M.
	moved := func(issueID, category string, days int) {
		one(`insert into issue_events (issue_id, kind, data, created_at)
			values ($1, 'status', jsonb_build_object('from', jsonb_build_object('category', 'unstarted'),
			  'to', jsonb_build_object('category', $2::text)),
			  (date_trunc('week', now() at time zone 'utc') at time zone 'utc') - make_interval(days => $3))
			returning id::text`, issueID, category, days)
	}

	i1 := issue(web, "completed", "", true) // completed twice: counted once, at M-18; started at M-30, cycle 12 days
	moved(i1, "started", 30)
	moved(i1, "completed", 28)
	moved(i1, "started", 20)
	moved(i1, "completed", 18)
	i2 := issue(web, "completed", "", false) // M-9, cycle 1 day
	moved(i2, "started", 10)
	moved(i2, "completed", 9)
	moved(issue(web, "completed", "", false), "completed", 9) // never started: counted, no cycle time
	i4 := issue(web, "started", "-1", true)                   // completed then reopened: not counted; open, overdue
	moved(i4, "completed", 5)
	moved(i4, "started", 4)
	moved(issue(web, "completed", "", false), "completed", 100) // before the 12 weeks
	issue(web, "backlog", "0", false)                           // open, due this week
	issue(old, "started", "", false)                            // archived project: not open
	i8 := issue(lead, "completed", "", false)                   // private: only Ann sees it, M-2, cycle 1 day
	moved(i8, "started", 3)
	moved(i8, "completed", 2)
	issue(lead, "unstarted", "", false)
	one(`insert into comments (issue_id, author_id, body, created_at)
		values ($1, $2, '{}', (date_trunc('week', now() at time zone 'utc') at time zone 'utc') - interval '7 days'),
		       ($3, $2, '{}', now()) returning id::text`, i1, bob, i8)
	one(`insert into test_runs (workspace_id, issue_id, kind, started_by) values ($1, $2, 'manual', $3), ($1, $4, 'manual', $3)
		returning id::text`, wsID, i1, bob, i8)

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	get := func(user, path string, want int, into any) {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, "GET", "/api/workspaces/"+slug+path, nil)
		if code != want {
			t.Fatalf("GET %s as %s: %d %s, want %d", path, user, code, body, want)
		}
		if into != nil {
			if err := json.Unmarshal([]byte(body), into); err != nil {
				t.Fatalf("GET %s: %v in %s", path, err, body)
			}
		}
	}
	analytics := func(viewer string) memberAnalytics {
		t.Helper()
		var a memberAnalytics
		get(viewer, "/members/"+bob+"/analytics", 200, &a)
		return a
	}
	type counts struct{ completed, created, comments []int }
	series := func(ws []weekRow) counts {
		var c counts
		for _, w := range ws {
			c.completed, c.created, c.comments = append(c.completed, w.Completed), append(c.created, w.Created),
				append(c.comments, w.Comments)
		}
		return c
	}
	eq := slices.Equal[[]int]

	// Bob sees himself without LEAD.
	a := analytics(bob)
	if a.Member.ID != bob || a.Member.Name != "Bob" || len(a.Weeks) != 12 || a.Weeks[0].Week >= a.Weeks[11].Week {
		t.Fatalf("member/weeks %+v", a)
	}
	s := series(a.Weeks)
	if !eq(s.completed, []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 0, 0}) || !eq(s.created, []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 7}) ||
		!eq(s.comments, []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0}) {
		t.Errorf("Bob's weeks %+v", s)
	}
	if a.Open.Total != 2 || a.Open.ByCategory["backlog"] != 1 || a.Open.ByCategory["unstarted"] != 0 ||
		a.Open.ByCategory["started"] != 1 || a.Open.Overdue != 1 || a.Open.DueThisWeek != 1 {
		t.Errorf("Bob's open %+v", a.Open)
	}
	if a.CycleTimeDays == nil || *a.CycleTimeDays != 6.5 || a.Completed30d != 3 || a.Tests.RunsStarted30d != 1 {
		t.Errorf("Bob's cycle time %v, completed %d, runs %d", a.CycleTimeDays, a.Completed30d, a.Tests.RunsStarted30d)
	}
	if c := a.CurrentCycles; len(c) != 1 || c[0].Project.Key != "WEB" || c[0].Name != "Cycle 1" || c[0].Assigned != 2 ||
		c[0].Completed != 1 {
		t.Errorf("Bob's cycles %+v", a.CurrentCycles)
	}

	// Ann, who sees LEAD, gets its issues too.
	a = analytics(ann)
	s = series(a.Weeks)
	if !eq(s.completed, []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 1, 0}) || s.created[11] != 9 || s.comments[11] != 1 ||
		a.Open.Total != 3 || a.Open.ByCategory["unstarted"] != 1 || a.CycleTimeDays == nil || *a.CycleTimeDays != 1 ||
		a.Completed30d != 4 || a.Tests.RunsStarted30d != 2 {
		t.Errorf("Ann's view of Bob %+v %+v", s, a)
	}

	// Access.
	get(bob, "/members/"+ann+"/analytics", 403, nil)
	get(bob, "/members/"+out+"/analytics", 403, nil)
	get(cara, "/members/"+bob+"/analytics", 200, nil)
	get(ann, "/members/"+out+"/analytics", 404, nil)
	get(ann, "/members/nope/analytics", 404, nil)
	get(out, "/members/"+out+"/analytics", 404, nil)
	var fresh memberAnalytics
	get(ann, "/members/"+ann+"/analytics", 200, &fresh)
	if fresh.CycleTimeDays != nil || len(fresh.Weeks) != 12 || fresh.CurrentCycles == nil || fresh.Open.Total != 0 {
		t.Errorf("Ann's own %+v", fresh)
	}

	// Team.
	type row struct {
		ID, Name      string
		Open, Overdue int
		Completed30d  int      `json:"completed_30d"`
		CycleTimeDays *float64 `json:"cycle_time_days"`
	}
	team := func(viewer string) []row {
		t.Helper()
		var tm struct{ Members []row }
		get(viewer, "/analytics/team", 200, &tm)
		return tm.Members
	}
	get(bob, "/analytics/team", 403, nil)
	get(out, "/analytics/team", 404, nil)
	tm := team(ann)
	if len(tm) != 3 || tm[0].Name != "Ann" || tm[1].Name != "Bob" || tm[2].Name != "Cara" || tm[0].CycleTimeDays != nil {
		t.Fatalf("team %+v", tm)
	}
	if b := tm[1]; b.Open != 3 || b.Overdue != 1 || b.Completed30d != 4 || b.CycleTimeDays == nil || *b.CycleTimeDays != 1 {
		t.Errorf("Bob as Ann sees him %+v", b)
	}
	if b := team(cara)[1]; b.Open != 2 || b.Completed30d != 3 || b.CycleTimeDays == nil || *b.CycleTimeDays != 6.5 {
		t.Errorf("Bob as Cara sees him %+v", b)
	}
}
