package cycle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/issue"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestCycles runs cycles through the API against the scratch database (see dbtest): overlaps are rejected, issues
// join a cycle of their own project only, rollover moves only open issues, and deleting a cycle clears its issues'.
// Ann is an admin, Bob a member.
func TestCycles(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, n, k from w, (values ('Web', 'WEB'), ('Api', 'API')) v(n, k))
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	status := func(name string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `select id::text from statuses where workspace_id = $1 and name = $2`, wsID, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	mux := http.NewServeMux()
	wsH := workspace.New(pool, dbtest.Auth, nil, nil)
	issueH := issue.New(pool, dbtest.Auth, wsH.Member)
	issueH.Register(mux)
	New(pool, dbtest.Auth, wsH.Member, issueH.CycleIssues).Register(mux)
	expect := func(step string, want int, user, method, path string, form url.Values, out any) {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, "/api/workspaces/"+slug+path, form)
		if code != want {
			t.Fatalf("%s: got %d %s, want %d", step, code, body, want)
		}
		if out != nil {
			if err := json.Unmarshal([]byte(body), out); err != nil {
				t.Fatalf("%s: %v in %s", step, err, body)
			}
		}
	}
	day := func(offset int) string { return time.Now().UTC().AddDate(0, 0, offset).Format(time.DateOnly) }
	dates := func(from, to int) url.Values { return url.Values{"starts_on": {day(from)}, "ends_on": {day(to)}} }

	var past, current Cycle
	expect("create past", 201, ann, "POST", "/projects/WEB/cycles", dates(-20, -7), &past)
	expect("create current", 201, ann, "POST", "/projects/WEB/cycles", dates(0, 13), &current)
	if past.Number != 1 || past.State != "past" || current.Number != 2 || current.State != "current" || current.Name != "Cycle 2" {
		t.Fatalf("created %+v %+v", past, current)
	}
	expect("a member can't create", 403, bob, "POST", "/projects/WEB/cycles", dates(30, 40), nil)

	// Overlaps are rejected, on create and update, but a cycle may keep its own dates.
	var errs struct{ Errors map[string]string }
	expect("overlap", 400, ann, "POST", "/projects/WEB/cycles", dates(5, 20), &errs)
	if errs.Errors["starts_on"] == "" {
		t.Errorf("overlap errors %v", errs.Errors)
	}
	expect("update into overlap", 400, ann, "POST", "/projects/WEB/cycles/1", url.Values{"ends_on": {day(1)}}, nil)
	expect("rename", 200, ann, "POST", "/projects/WEB/cycles/1", url.Values{"name": {"Launch"}, "ends_on": {day(-6)}}, &past)
	if past.Name != "Launch" || past.CustomName == nil || past.EndsOn != day(-6) {
		t.Errorf("renamed %+v", past)
	}
	expect("too long", 400, ann, "POST", "/projects/WEB/cycles", dates(100, 190), nil)
	expect("another project's dates don't clash", 201, ann, "POST", "/projects/API/cycles", dates(0, 13), nil)

	// Issues join a cycle of their own project only.
	var other []Cycle
	expect("api cycles", 200, ann, "GET", "/projects/API/cycles", nil, &other)
	expect("other project's cycle", 400, ann, "POST", "/projects/WEB/issues", url.Values{"title": {"x"}, "cycle": {other[0].ID}}, nil)
	for _, st := range []string{"Todo", "In progress", "Done", "Cancelled"} {
		expect("issue "+st, 201, ann, "POST", "/projects/WEB/issues",
			url.Values{"title": {st}, "status_id": {status(st)}, "cycle": {past.ID}}, nil)
	}
	var one struct {
		Cycle  Cycle         `json:"cycle"`
		Issues []issue.Issue `json:"issues"`
	}
	expect("get past", 200, ann, "GET", "/projects/WEB/cycles/1", nil, &one)
	if want := (Progress{Total: 4, Started: 1, Completed: 1, Canceled: 1}); one.Cycle.Progress != want || len(one.Issues) != 4 ||
		one.Issues[0].Cycle == nil || one.Issues[0].Cycle.Number != 1 {
		t.Fatalf("past cycle %+v with %d issues", one.Cycle, len(one.Issues))
	}

	// Rollover moves only open issues, to a cycle that isn't past or the source, and logs each move.
	expect("rollover to itself", 400, ann, "POST", "/projects/WEB/cycles/1/rollover", url.Values{"to": {"1"}}, nil)
	expect("rollover to nowhere", 400, ann, "POST", "/projects/WEB/cycles/1/rollover", url.Values{"to": {"9"}}, nil)
	expect("rollover to the past", 400, ann, "POST", "/projects/WEB/cycles/2/rollover", url.Values{"to": {"1"}}, nil)
	var rolled struct {
		Moved int   `json:"moved"`
		Cycle Cycle `json:"cycle"`
	}
	expect("rollover", 200, ann, "POST", "/projects/WEB/cycles/1/rollover", url.Values{"to": {"2"}}, &rolled)
	if rolled.Moved != 2 || rolled.Cycle.Progress.Total != 2 {
		t.Errorf("rolled %+v", rolled)
	}
	var left []string
	rows, _ := pool.Query(ctx, `select i.title from issues i join cycles c on c.id = i.cycle_id where c.number = 1 and c.workspace_id = $1 order by 1`, wsID)
	if left, _ = pgx.CollectRows(rows, pgx.RowTo[string]); len(left) != 2 || left[0] != "Cancelled" || left[1] != "Done" {
		t.Errorf("left in cycle 1: %v", left)
	}
	var events int
	if err := pool.QueryRow(ctx, `select count(*) from issue_events e join issues i on i.id = e.issue_id
		where i.workspace_id = $1 and e.kind = 'cycle' and e.data->'to'->>'number' = '2'`, wsID).Scan(&events); err != nil || events != 2 {
		t.Errorf("rollover events: %d %v", events, err)
	}

	// Clearing an issue's cycle logs the change.
	expect("clear cycle", 200, ann, "POST", "/issues/WEB-3", url.Values{"cycle": {""}}, nil)
	var data string
	if err := pool.QueryRow(ctx, `select e.data::text from issue_events e join issues i on i.id = e.issue_id
		where i.workspace_id = $1 and i.number = 3 and e.kind = 'cycle'`, wsID).Scan(&data); err != nil ||
		data != `{"to": null, "from": {"name": "Launch", "number": 1}}` {
		t.Errorf("clear event %s %v", data, err)
	}

	// Deleting a cycle keeps its issues, with no cycle.
	expect("delete", 204, ann, "DELETE", "/projects/WEB/cycles/2", nil, nil)
	expect("deleted", 404, ann, "GET", "/projects/WEB/cycles/2", nil, nil)
	var inNone int
	if err := pool.QueryRow(ctx, `select count(*) from issues where workspace_id = $1 and cycle_id is null and title in ('Todo', 'In progress')`, wsID).
		Scan(&inNone); err != nil || inNone != 2 {
		t.Errorf("issues without a cycle after delete: %d %v", inNone, err)
	}
}

func TestValidation(t *testing.T) {
	d := func(s string) time.Time { t, _ := parseDate(s); return t }
	today := d("2026-10-31")
	cases := []struct{ got, want string }{
		{checkSpan(d("2026-11-02"), d("2026-11-01")), "The end date can't be before the start date."},
		{checkSpan(d("2026-11-01"), d("2026-11-01")), ""},
		{checkSpan(d("2026-11-01"), d("2027-01-29")), ""}, // 90 days
		{checkSpan(d("2026-11-01"), d("2027-01-30")), "A cycle can be at most 90 days."},
		{overlapMsg("Cycle 3", d("2026-11-02"), d("2026-11-15"), today), "This overlaps Cycle 3 (Nov 2 – Nov 15)."},
		{overlapMsg("Cycle 4", d("2026-12-28"), d("2027-01-10"), today), "This overlaps Cycle 4 (Dec 28 – Jan 10, 2027)."},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	if _, ok := parseDate("2026-02-30"); ok {
		t.Error("parseDate accepts Feb 30")
	}
}
