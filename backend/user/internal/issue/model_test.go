package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const testDoc = `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"%s"}]}]}`

type timelineJSON struct {
	Comments []Comment `json:"comments"`
	Events   []Event   `json:"events"`
	HasMore  bool      `json:"has_more"`
}

// TestIssueFlows runs issues, sub-issues, links, comments and the timeline through the API against the scratch
// database (see dbtest). Ann is an admin; Bob and Cat are members.
func TestIssueFlows(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cat := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cat")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Engineering', 'ENG' from w)
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	dbtest.Join(t, pool, wsID, cat, "member")
	rows, _ := pool.Query(ctx, `select id::text from statuses where workspace_id = $1 order by position`, wsID)
	statuses, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(statuses) < 2 {
		t.Fatalf("statuses: %v %v", statuses, err)
	}

	mux := http.NewServeMux()
	wsH := workspace.New(pool, dbtest.Auth, nil, nil)
	New(pool, dbtest.Auth, wsH.Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values) (int, string) {
		t.Helper()
		return dbtest.Do(t, mux, user, method, base+path, form)
	}
	expect := func(step string, want int, user, method, path string, form url.Values) string {
		t.Helper()
		code, body := do(user, method, path, form)
		if code != want {
			t.Fatalf("%s: got %d %s, want %d", step, code, body, want)
		}
		return body
	}
	kinds := func(identifier string) []string {
		t.Helper()
		var p timelineJSON
		if err := json.Unmarshal([]byte(expect("timeline", 200, ann, "GET", "/issues/"+identifier+"/timeline", nil)), &p); err != nil {
			t.Fatal(err)
		}
		var ks []string
		for _, e := range p.Events {
			ks = append(ks, e.Kind)
		}
		return ks
	}

	// Create and update: each real change is one event, and only the creator, assignee or issues.edit_any edits.
	var i Issue
	if err := json.Unmarshal([]byte(expect("create", 201, ann, "POST", "/projects/ENG/issues",
		url.Values{"title": {"First"}, "assignee_id": {bob}})), &i); err != nil {
		t.Fatal(err)
	}
	if i.Identifier != "ENG-1" || i.Assignee == nil || i.Assignee.ID != bob || i.Status.ID != statuses[0] {
		t.Fatalf("created %+v", i)
	}
	expect("update", 200, ann, "POST", "/issues/ENG-1",
		url.Values{"title": {"Renamed"}, "priority": {"2"}, "status_id": {statuses[1]}, "assignee_id": {bob}})
	if got, want := kinds("ENG-1"), []string{"created", "title", "status", "priority"}; !sameSet(got, want) {
		t.Errorf("events %v, want %v", got, want)
	}
	expect("a member can't edit others' issues", 403, cat, "POST", "/issues/ENG-1", url.Values{"title": {"Mine"}})
	expect("the assignee can", 200, bob, "POST", "/issues/ENG-1", url.Values{"title": {"Bob's"}})
	expect("bad status", 400, ann, "POST", "/issues/ENG-1", url.Values{"status_id": {"nope"}})
	// Only REST writes of the description bump its generation.
	generation := func() int {
		t.Helper()
		var got Issue
		if err := json.Unmarshal([]byte(expect("get", 200, ann, "GET", "/issues/ENG-1", nil)), &got); err != nil {
			t.Fatal(err)
		}
		return got.DescriptionGeneration
	}
	if g := generation(); g != 0 {
		t.Errorf("generation after create and title edits: %d, want 0", g)
	}
	expect("description", 200, ann, "POST", "/issues/ENG-1", url.Values{"description": {`{"type":"doc","content":[]}`}})
	if g := generation(); g != 1 {
		t.Errorf("generation after a REST description write: %d, want 1", g)
	}
	var assigned int
	if err := pool.QueryRow(ctx, `select count(*) from notifications where user_id = $1 and kind = 'assigned'`, bob).Scan(&assigned); err != nil || assigned != 1 {
		t.Errorf("bob's assigned notifications: %d %v, want 1", assigned, err)
	}

	// Sub-issues are one level deep, in the same project.
	expect("sub-issue", 201, ann, "POST", "/projects/ENG/issues", url.Values{"title": {"Child"}, "parent": {"ENG-1"}})
	expect("no grandchildren", 400, ann, "POST", "/projects/ENG/issues", url.Values{"title": {"Grandchild"}, "parent": {"ENG-2"}})
	expect("not its own parent", 400, ann, "POST", "/issues/ENG-1", url.Values{"parent": {"ENG-1"}})
	expect("a parent can't become a child", 400, ann, "POST", "/issues/ENG-1", url.Values{"parent": {"ENG-2"}})
	expect("third", 201, ann, "POST", "/projects/ENG/issues", url.Values{"title": {"Third"}})

	// Links: once per pair either way, never to itself, logged on both sides.
	expect("link", 201, ann, "POST", "/issues/ENG-1/links", url.Values{"type": {"blocks"}, "issue": {"ENG-3"}})
	expect("linked either way", 409, ann, "POST", "/issues/ENG-3/links", url.Values{"type": {"blocks"}, "issue": {"ENG-1"}})
	expect("not to itself", 400, ann, "POST", "/issues/ENG-1/links", url.Values{"type": {"blocks"}, "issue": {"ENG-1"}})
	expect("unlink from the other side", 200, ann, "DELETE", "/issues/ENG-3/links/ENG-1", nil)
	expect("unlink again", 404, ann, "DELETE", "/issues/ENG-3/links/ENG-1", nil)
	if got, want := kinds("ENG-3"), []string{"created", "link", "link"}; !slices.Equal(got, want) {
		t.Errorf("ENG-3 events %v, want %v", got, want)
	}

	// Comments: the author edits and deletes their own, comments.manage anyone's.
	var c Comment
	if err := json.Unmarshal([]byte(expect("comment", 201, bob, "POST", "/issues/ENG-1/comments",
		url.Values{"body": {fmt.Sprintf(testDoc, "hi")}})), &c); err != nil {
		t.Fatal(err)
	}
	if !c.CanEdit || !c.CanDelete {
		t.Errorf("author's comment: can_edit %v can_delete %v", c.CanEdit, c.CanDelete)
	}
	edit := url.Values{"body": {fmt.Sprintf(testDoc, "edited")}}
	expect("someone else can't edit it", 403, cat, "POST", "/comments/"+c.ID, edit)
	expect("someone else can't delete it", 403, cat, "DELETE", "/comments/"+c.ID, nil)
	expect("the author edits it", 200, bob, "POST", "/comments/"+c.ID, edit)
	expect("an empty comment", 400, bob, "POST", "/issues/ENG-1/comments", url.Values{"body": {""}})
	expect("an admin deletes it", 204, ann, "DELETE", "/comments/"+c.ID, nil)
	expect("gone", 404, bob, "DELETE", "/comments/"+c.ID, nil)
	expect("a non-uuid id", 404, bob, "DELETE", "/comments/x", nil)

	// Pagination: 250 events, three to a timestamp, page back without gaps or repeats.
	var issueID string
	if err := pool.QueryRow(ctx, `select id::text from issues where workspace_id = $1 and number = 3`, wsID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`insert into issue_events (issue_id, actor_id, kind, data, created_at)
		 select $1, $2, 'title', '{}', now() - interval '1 day' + (n / 3) * interval '1 second' from generate_series(1, 250) n`,
		issueID, ann); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := pool.QueryRow(ctx, `select (select count(*) from issue_events where issue_id = $1) + (select count(*) from comments where issue_id = $1)`,
		issueID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	before := ""
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatal("too many pages")
		}
		path := "/issues/ENG-3/timeline"
		if before != "" {
			path += "?before=" + url.QueryEscape(before)
		}
		var p timelineJSON
		if err := json.Unmarshal([]byte(expect("page", 200, ann, "GET", path, nil)), &p); err != nil {
			t.Fatal(err)
		}
		if n := len(p.Events) + len(p.Comments); n > timelinePage || n == 0 {
			t.Fatalf("page of %d", n)
		}
		if !slices.IsSortedFunc(p.Events, func(a, b Event) int { return a.CreatedAt.Compare(b.CreatedAt) }) {
			t.Error("events not oldest first")
		}
		for _, e := range p.Events {
			if seen[e.ID] {
				t.Fatalf("event %s on two pages", e.ID)
			}
			seen[e.ID] = true
		}
		if !p.HasMore {
			break
		}
		before = p.Events[0].CreatedAt.Format(time.RFC3339Nano)
	}
	if len(seen) != total {
		t.Errorf("paged %d items, want %d", len(seen), total)
	}
	expect("bad before", 400, ann, "GET", "/issues/ENG-3/timeline?before=yesterday", nil)
}

func sameSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

func TestTimelineCut(t *testing.T) {
	at := func(s int) time.Time { return time.Unix(int64(s), 0) }
	var es []Event
	for s := 200; s > 0; s-- { // newest first, one a second
		es = append(es, Event{ID: fmt.Sprint(s), CreatedAt: at(s)})
	}
	_, got, more := timelineCut(nil, slices.Clone(es))
	if !more || len(got) != timelinePage || got[len(got)-1].CreatedAt != at(201-timelinePage) {
		t.Fatalf("plain cut: %d items, more %v, oldest %v", len(got), more, got[len(got)-1].CreatedAt)
	}
	// The last event of a page and the next item share a second, so that whole second goes to the next page.
	cs := []Comment{{ID: "c", CreatedAt: at(201 - timelinePage)}}
	gotC, got, more := timelineCut(cs, slices.Clone(es[:timelinePage]))
	if !more || len(gotC) != 0 || len(got) != timelinePage-1 {
		t.Fatalf("tie cut: %d comments, %d events, more %v; want 0, %d, true", len(gotC), len(got), more, timelinePage-1)
	}
	_, got, more = timelineCut(nil, slices.Clone(es[:timelinePage/2]))
	if more || len(got) != timelinePage/2 {
		t.Fatalf("short page: %d, more %v", len(got), more)
	}
}
