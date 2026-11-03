package issue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestMove runs manual ordering through the API against the scratch database (see dbtest): create placement, moves
// within and across statuses, the errors, two moves into one gap at once and a rebalance. Ann is an admin, Bob a
// member outside the private LEAD project.
func TestMove(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Web', 'WEB', false), ('Ops', 'OPS', false), ('Leadership', 'LEAD', true)) v(n, k, pr))
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	rows, _ := pool.Query(ctx, `select id::text from statuses where workspace_id = $1 order by position`, wsID)
	statuses, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(statuses) < 2 {
		t.Fatalf("statuses: %v %v", statuses, err)
	}

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	expect := func(step string, want int, user, method, path string, form url.Values) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s: got %d %s, want %d", step, code, body, want)
		}
		return body
	}
	order := func() []string {
		t.Helper()
		var is []Issue
		if err := json.Unmarshal([]byte(expect("list", 200, ann, "GET", "/projects/WEB/issues?sort=manual", nil)), &is); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for k, i := range is {
			if k > 0 && is[k-1].Rank >= i.Rank {
				t.Errorf("ranks out of order: %q, %q", is[k-1].Rank, i.Rank)
			}
			ids = append(ids, strings.TrimPrefix(i.Identifier, "WEB-"))
		}
		return ids
	}
	want := func(step string, ids ...string) {
		t.Helper()
		if got := order(); !slices.Equal(got, ids) {
			t.Errorf("%s: order %v, want %v", step, got, ids)
		}
	}
	move := func(step string, code int, identifier string, form url.Values) {
		t.Helper()
		expect(step, code, ann, "POST", "/issues/"+identifier+"/move", form)
	}

	for _, title := range []string{"One", "Two", "Three", "Four"} {
		expect("create "+title, 201, ann, "POST", "/projects/WEB/issues", url.Values{"title": {title}})
	}
	expect("create OPS-1", 201, ann, "POST", "/projects/OPS/issues", url.Values{"title": {"Elsewhere"}})
	expect("create LEAD-1", 201, ann, "POST", "/projects/LEAD/issues", url.Values{"title": {"Secret"}})
	want("new issues go last", "1", "2", "3", "4")
	expect("create after", 201, ann, "POST", "/projects/WEB/issues", url.Values{"title": {"Five"}, "after": {"WEB-1"}})
	want("created after WEB-1", "1", "5", "2", "3", "4")
	expect("create next to another project's issue", 400, ann, "POST", "/projects/WEB/issues",
		url.Values{"title": {"Six"}, "before": {"OPS-1"}})

	var manual bool
	if err := pool.QueryRow(ctx, `select manual_order from projects where workspace_id = $1 and key = 'WEB'`, wsID).Scan(&manual); err != nil || manual {
		t.Fatalf("manual_order before a move: %v %v", manual, err)
	}
	move("after", 200, "WEB-4", url.Values{"after": {"WEB-1"}})
	want("moved WEB-4 after WEB-1", "1", "4", "5", "2", "3")
	if err := pool.QueryRow(ctx, `select manual_order from projects where workspace_id = $1 and key = 'WEB'`, wsID).Scan(&manual); err != nil || !manual {
		t.Fatalf("manual_order after a move: %v %v", manual, err)
	}
	move("before only", 200, "WEB-3", url.Values{"before": {"WEB-1"}})
	want("moved WEB-3 first", "3", "1", "4", "5", "2")
	move("a stale before loses to after", 200, "WEB-2", url.Values{"after": {"WEB-1"}, "before": {"WEB-3"}})
	want("moved WEB-2 after WEB-1", "3", "1", "2", "4", "5")
	move("next to itself", 200, "WEB-2", url.Values{"after": {"WEB-2"}})
	move("already there", 200, "WEB-2", url.Values{"after": {"WEB-1"}})
	want("no-ops", "3", "1", "2", "4", "5")

	// Across statuses: status and rank change in one call, with one status event.
	var i Issue
	if err := json.Unmarshal([]byte(expect("cross-status", 200, ann, "POST", "/issues/WEB-5/move",
		url.Values{"after": {"WEB-3"}, "status_id": {statuses[1]}})), &i); err != nil {
		t.Fatal(err)
	}
	if i.Status.ID != statuses[1] {
		t.Errorf("status after the move: %s, want %s", i.Status.ID, statuses[1])
	}
	want("moved WEB-5 after WEB-3", "3", "5", "1", "2", "4")
	var events int
	if err := pool.QueryRow(ctx, `select count(*) from issue_events e join issues i on i.id = e.issue_id
		where i.workspace_id = $1 and i.number = 5 and e.kind = 'status'`, wsID).Scan(&events); err != nil || events != 1 {
		t.Errorf("status events: %d %v, want 1", events, err)
	}

	move("neighbour in another project", 400, "WEB-1", url.Values{"after": {"OPS-1"}})
	move("neighbour not found", 400, "WEB-1", url.Values{"before": {"WEB-99"}})
	move("neighbour not visible", 400, "WEB-1", url.Values{"after": {"LEAD-1"}})
	move("nowhere", 400, "WEB-1", url.Values{})
	move("bad status", 400, "WEB-1", url.Values{"after": {"WEB-2"}, "status_id": {"nope"}})
	move("no such issue", 404, "WEB-99", url.Values{"after": {"WEB-1"}})
	expect("Bob can't edit Ann's issue", 403, bob, "POST", "/issues/WEB-1/move", url.Values{"after": {"WEB-2"}})
	expect("Bob can't see LEAD", 404, bob, "POST", "/issues/LEAD-1/move", url.Values{"after": {"LEAD-1"}})
	want("errors change nothing", "3", "5", "1", "2", "4")

	// Two moves into the same gap at once both land, in some order.
	var wg sync.WaitGroup
	for _, id := range []string{"WEB-2", "WEB-4"} {
		wg.Go(func() {
			if code, body := dbtest.Do(t, mux, ann, "POST", base+"/issues/"+id+"/move", url.Values{"after": {"WEB-3"}}); code != 200 {
				t.Errorf("concurrent move of %s: %d %s", id, code, body)
			}
		})
	}
	wg.Wait()
	if got := order(); !slices.Equal(got[:1], []string{"3"}) || !slices.Equal(slices.Sorted(slices.Values(got[1:3])), []string{"2", "4"}) {
		t.Errorf("after two moves into one gap: %v", got)
	}

	// A gap too narrow for a short rank renumbers the project, keeping its order.
	if _, err := pool.Exec(ctx, `update issues i set rank = case i.number when 1 then 'V' when 2 then 'V' || repeat('0', 48) || '1'
		else 'W' || i.number end from projects p where p.id = i.project_id and p.workspace_id = $1 and p.key = 'WEB'`, wsID); err != nil {
		t.Fatal(err)
	}
	move("into a narrow gap", 200, "WEB-5", url.Values{"after": {"WEB-1"}})
	want("rebalanced", "1", "5", "2", "3", "4")
	var long int
	if err := pool.QueryRow(ctx, `select count(*) from issues i join projects p on p.id = i.project_id
		where p.workspace_id = $1 and p.key = 'WEB' and length(i.rank) > 12`, wsID).Scan(&long); err != nil || long != 0 {
		t.Errorf("long ranks after the rebalance: %d %v", long, err)
	}
}
