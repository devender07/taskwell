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

// TestPoints runs estimates through the API: validation, clearing and the activity event, a cycle's points by
// category with canceled issues left out, the daily snapshot (one row per day), and the burndown.
func TestPoints(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Web', 'WEB' from w)
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
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
	h := New(pool, dbtest.Auth, wsH.Member, issueH.CycleIssues)
	h.Register(mux)
	expect := func(step string, want int, method, path string, form url.Values, out any) {
		t.Helper()
		code, body := dbtest.Do(t, mux, ann, method, "/api/workspaces/"+slug+path, form)
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

	var cur Cycle
	expect("create cycle", 201, "POST", "/projects/WEB/cycles", url.Values{"starts_on": {day(-3)}, "ends_on": {day(10)}}, &cur)

	var errs struct{ Errors map[string]string }
	expect("bad estimate", 400, "POST", "/projects/WEB/issues", url.Values{"title": {"x"}, "estimate": {"4"}}, &errs)
	if errs.Errors["estimate"] != "Choose 1, 2, 3, 5 or 8 points." {
		t.Errorf("estimate errors %v", errs.Errors)
	}
	for _, c := range []struct{ title, status, estimate string }{
		{"Todo", "Todo", "3"}, {"Doing", "In progress", "5"}, {"Done", "Done", "8"}, {"Dropped", "Cancelled", "2"}, {"None", "Todo", ""},
	} {
		var i issue.Issue
		expect("issue "+c.title, 201, "POST", "/projects/WEB/issues",
			url.Values{"title": {c.title}, "status_id": {status(c.status)}, "cycle": {cur.ID}, "estimate": {c.estimate}}, &i)
		if want := c.estimate != ""; (i.Estimate != nil) != want {
			t.Errorf("%s: estimate %v", c.title, i.Estimate)
		}
	}

	// Changing and clearing an estimate log events; an update without the field keeps it.
	var i issue.Issue
	expect("change estimate", 200, "POST", "/issues/WEB-1", url.Values{"estimate": {"1"}}, &i)
	expect("keep estimate", 200, "POST", "/issues/WEB-1", url.Values{"title": {"Todo 2"}}, &i)
	if i.Estimate == nil || *i.Estimate != 1 {
		t.Fatalf("estimate after title change %v", i.Estimate)
	}
	expect("clear estimate", 200, "POST", "/issues/WEB-1", url.Values{"estimate": {""}}, &i)
	if i.Estimate != nil {
		t.Fatalf("cleared estimate %v", *i.Estimate)
	}
	rows, _ := pool.Query(ctx, `select e.data::text from issue_events e join issues i on i.id = e.issue_id
		where i.workspace_id = $1 and i.number = 1 and e.kind = 'estimate' order by e.id`, wsID)
	if got, err := pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || len(got) != 2 ||
		got[0] != `{"to": 1, "from": 3}` || got[1] != `{"to": null, "from": 1}` {
		t.Errorf("estimate events %v %v", got, err)
	}
	expect("set again", 200, "POST", "/issues/WEB-1", url.Values{"estimate": {"3"}}, nil)

	// Points: total 3+5+8 (canceled's 2 and the unestimated left out), started 5, completed 8.
	var got struct{ Cycle Cycle }
	expect("get cycle", 200, "GET", "/projects/WEB/cycles/1", nil, &got)
	if want := (Points{Total: 16, Started: 5, Completed: 8}); got.Cycle.Progress.Points != want {
		t.Fatalf("points %+v, want %+v", got.Cycle.Progress.Points, want)
	}

	// The snapshot keeps one row a day and skips cycles that aren't current.
	expect("upcoming cycle", 201, "POST", "/projects/WEB/cycles", url.Values{"starts_on": {day(20)}, "ends_on": {day(30)}}, nil)
	now := time.Now().UTC()
	for range 2 {
		if err := h.model.snapshotPoints(ctx, now); err != nil {
			t.Fatal(err)
		}
	}
	var n, scope, done int
	if err := pool.QueryRow(ctx, `select count(*), max(scope), max(done) from cycle_points p join cycles c on c.id = p.cycle_id
		where c.workspace_id = $1`, wsID).Scan(&n, &scope, &done); err != nil || n != 1 || scope != 16 || done != 8 {
		t.Fatalf("snapshot rows %d scope %d done %d %v", n, scope, done, err)
	}

	// Burndown: recorded days before today (one before the start is left out), today live, planned the first day's.
	if _, err := pool.Exec(ctx, `insert into cycle_points (cycle_id, day, scope, done) values
		($1, $2::date, 99, 0), ($1, $3::date, 20, 0), ($1, $4::date, 18, 3)`, cur.ID, day(-4), day(-2), day(-1)); err != nil {
		t.Fatal(err)
	}
	var bd struct {
		Planned *int  `json:"planned"`
		Days    []Day `json:"days"`
	}
	expect("burndown", 200, "GET", "/projects/WEB/cycles/1/burndown", nil, &bd)
	if bd.Planned == nil || *bd.Planned != 20 || len(bd.Days) != 3 || bd.Days[0] != (Day{day(-2), 20, 0}) ||
		bd.Days[2] != (Day{day(0), 16, 8}) {
		t.Fatalf("burndown %+v planned %v", bd.Days, bd.Planned)
	}
	expect("upcoming burndown", 200, "GET", "/projects/WEB/cycles/2/burndown", nil, &bd)
	if bd.Planned != nil || bd.Days == nil || len(bd.Days) != 0 {
		t.Errorf("upcoming burndown %+v planned %v", bd.Days, bd.Planned)
	}
	expect("no cycle", 404, "GET", "/projects/WEB/cycles/9/burndown", nil, nil)
}
