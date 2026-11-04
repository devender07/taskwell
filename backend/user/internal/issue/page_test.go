package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestPagedLists runs the paged mode of the project, cycle, my-issues and watching lists (filters, each sort, grouping counts
// and in/offset) against the scratch database (see dbtest).
func TestPagedLists(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var wsID, slug, s0, s1, label string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select w.id::text, w.slug,
			  (select id::text from statuses where workspace_id = w.id order by position limit 1),
			  (select id::text from statuses where workspace_id = w.id order by position offset 1 limit 1)
			from workspaces w where created_by = $1`, ann).Scan(&wsID, &slug, &s0, &s1); err != nil {
			return err
		}
		// number, status, priority, assignee, due date, in the cycle; issue 1 is the newest created, 4 the newest
		// updated (then 5 and 3, then 6 and 2, then 1). The manual order is 3, 1, 6, 2, 5, 4.
		_, err := tx.Exec(ctx, `
			with p as (insert into projects (workspace_id, name, key) values ($1, 'Engineering', 'ENG') returning id),
			c as (insert into cycles (workspace_id, project_id, number, starts_on, ends_on)
			      select $1, id, 1, '2026-01-01', '2026-01-14' from p returning id),
			v (number, status_id, priority, mine, due, cycled) as (values
			  (1, $2::uuid, 0, true, null::date, true), (2, $2, 1, false, '2026-01-05', false), (3, $2, 3, true, '2026-01-01', true),
			  (4, $3::uuid, 1, false, null, false), (5, $2, 1, true, null, false), (6, $3, 4, true, '2026-01-03', true))
			insert into issues (workspace_id, project_id, number, title, status_id, priority, assignee_id, due_date, cycle_id,
			                    created_at, updated_at, rank)
			select $1, p.id, v.number, 'Issue ' || v.number, v.status_id, v.priority, case when v.mine then $4::uuid end, v.due,
			       case when v.cycled then c.id end, now() - v.number * interval '1 minute',
			       now() - abs(v.number - 4) * interval '1 minute',
			       substr('BDAFEC', v.number, 1)
			from v, p, c`, wsID, s0, s1, ann)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with l as (insert into labels (workspace_id, name, color) values ($1, 'Bug', '#ff0000') returning id)
			insert into issue_labels (issue_id, label_id) select i.id, l.id from issues i, l
			where i.workspace_id = $1 and i.number in (2, 6) returning label_id::text`, wsID).Scan(&label)
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	get := func(path string, want int, v any) {
		t.Helper()
		code, body := dbtest.Do(t, mux, ann, "GET", base+path, nil)
		if code != want {
			t.Fatalf("%s: got %d %s, want %d", path, code, body, want)
		}
		if v != nil {
			if err := json.Unmarshal([]byte(body), v); err != nil {
				t.Fatal(err)
			}
		}
	}
	type page struct {
		Total  int     `json:"total"`
		Groups []Group `json:"groups"`
		Issues []Issue `json:"issues"`
		More   bool    `json:"has_more"`
	}
	numbers := func(is []Issue) []int {
		ns := []int{}
		for _, i := range is {
			ns = append(ns, i.Number)
		}
		return ns
	}
	// groups is "key count [numbers] more" per group.
	groups := func(path string) (int, map[string]string) {
		t.Helper()
		var p page
		get(path, 200, &p)
		m := map[string]string{}
		for _, g := range p.Groups {
			m[g.Key] = fmt.Sprint(g.Count, numbers(g.Issues), g.HasMore)
		}
		return p.Total, m
	}
	in := func(path string, want []int, more bool) {
		t.Helper()
		var p page
		get(path, 200, &p)
		if !slices.Equal(numbers(p.Issues), want) || p.More != more {
			t.Errorf("%s: got %v %v, want %v %v", path, numbers(p.Issues), p.More, want, more)
		}
	}
	check := func(path string, wantTotal int, want map[string]string) {
		t.Helper()
		total, got := groups(path)
		if total != wantTotal || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: got %d %v, want %d %v", path, total, got, wantTotal, want)
		}
	}

	check("/projects/ENG/issues?limit=2", 6, map[string]string{s0: "4 [5 2] true", s1: "2 [4 6] false"})
	in("/projects/ENG/issues?limit=2&in="+s0+"&offset=2", []int{3, 1}, false)
	in("/projects/ENG/issues?limit=2&in="+s0+"&offset=9", []int{}, false)
	in("/projects/ENG/issues?limit=10&group=none&sort=due&in=all", []int{3, 6, 2, 5, 4, 1}, false)
	in("/projects/ENG/issues?limit=10&group=none&sort=created&in=all", []int{1, 2, 3, 4, 5, 6}, false)
	in("/projects/ENG/issues?limit=10&group=none&sort=updated&in=all", []int{4, 5, 3, 6, 2, 1}, false)
	in("/projects/ENG/issues?limit=10&group=none&sort=manual&in=all", []int{3, 1, 6, 2, 5, 4}, false)
	check("/projects/ENG/issues?limit=1&group=assignee", 6, map[string]string{ann: "4 [5] true", "none": "2 [4] true"})
	check("/projects/ENG/issues?limit=5&group=priority", 6,
		map[string]string{"0": "1 [1] false", "1": "3 [5 4 2] false", "3": "1 [3] false", "4": "1 [6] false"})
	check("/projects/ENG/issues?limit=5&group=none&assignee=me&priority=1,3,9", 2, map[string]string{"all": "2 [5 3] false"})
	check("/projects/ENG/issues?limit=5&group=none&label="+label+",bad", 2, map[string]string{"all": "2 [2 6] false"})
	check("/projects/ENG/issues?limit=5&group=none&cycle=none&status="+s0, 2, map[string]string{"all": "2 [5 2] false"})
	check("/projects/ENG/issues?limit=5&group=none&assignee=none&status="+s1, 1, map[string]string{"all": "1 [4] false"})
	check("/projects/ENG/issues?limit=5&status="+s1+"&priority=0", 0, map[string]string{})
	for _, q := range []string{"limit=0", "limit=101", "limit=x", "limit=5&group=label", "limit=5&sort=title", "limit=5&in=all&offset=-1"} {
		get("/projects/ENG/issues?"+q, 400, nil)
	}

	var all []Issue
	get("/projects/ENG/issues", 200, &all)
	if len(all) != 6 || all[0].Number != 6 || !all[0].CanEdit {
		t.Errorf("unpaged list: %v", numbers(all))
	}
	get("/projects/ENG/cycles/1/issues", 200, &all)
	if !slices.Equal(numbers(all), []int{6, 3, 1}) {
		t.Errorf("unpaged cycle list: %v", numbers(all))
	}
	get("/projects/ENG/issues?sort=manual", 200, &all)
	if !slices.Equal(numbers(all), []int{3, 1, 6, 2, 5, 4}) || all[0].Rank != "A" {
		t.Errorf("unpaged manual list: %v", numbers(all))
	}
	get("/projects/ENG/cycles/1/issues?sort=manual", 200, &all)
	if !slices.Equal(numbers(all), []int{3, 1, 6}) {
		t.Errorf("unpaged manual cycle list: %v", numbers(all))
	}
	get("/projects/ENG/issues?sort=title", 400, nil)
	check("/projects/ENG/cycles/1/issues?limit=5&group=none", 3, map[string]string{"all": "3 [3 6 1] false"})
	get("/projects/ENG/cycles/2/issues?limit=5", 404, nil)
	get("/projects/NOPE/cycles/1/issues", 404, nil)
	check("/my-issues?limit=5&group=status&sort=due", 4, map[string]string{s0: "3 [3 5 1] false", s1: "1 [6] false"})

	check("/watching?limit=5", 0, map[string]string{})
	if _, err := pool.Exec(ctx, `insert into issue_watchers (issue_id, user_id) select id, $2 from issues
		where workspace_id = $1 and number in (2, 4)`, wsID, ann); err != nil {
		t.Fatal(err)
	}
	check("/watching?limit=5&group=none", 2, map[string]string{"all": "2 [4 2] false"})
	get("/watching", 200, &all)
	if !slices.Equal(numbers(all), []int{2, 4}) {
		t.Errorf("unpaged watching: %v", numbers(all))
	}
	var d IssueDetail
	get("/issues/ENG-2", 200, &d)
	if !d.Watching || len(d.Watchers) != 1 || d.Watchers[0].ID != ann {
		t.Errorf("ENG-2 watchers: %v %v", d.Watching, d.Watchers)
	}
}
