package issue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestActivity pages the merged feed of events and comments with tied timestamps, and checks that description
// edits, archived projects and other workspaces stay out.
func TestActivity(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, out := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Out")
	var wsID, slug, live, archived string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateDefault(ctx, tx, ann, "Ann"); err != nil {
			return err
		}
		if err := workspace.CreateDefault(ctx, tx, out, "Out"); err != nil { // Out's own workspace, whose activity mustn't leak
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, archived_at)
			      select id, n, k, a from w, (values ('Engineering', 'ENG', null::timestamptz), ('Old', 'OLD', now())) v(n, k, a)
			      returning id, key),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id)
			      select (select id from w), p.id, 1, p.key || ' issue', (select id from s) from p returning id, title)
			select (select id::text from w), (select slug from w),
			       (select id::text from i where title = 'ENG issue'), (select id::text from i where title = 'OLD issue')`, ann).
			Scan(&wsID, &slug, &live, &archived)
	}); err != nil {
		t.Fatal(err)
	}
	// 70 events and 25 comments, five to a second (events and comments share seconds), plus noise that's left out.
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`insert into issue_events (issue_id, actor_id, kind, data, created_at)
		select $1, $2, 'title', '{"from":"a","to":"b"}', now() - interval '1 day' + (n / 5) * interval '1 second'
		from generate_series(1, 70) n`, live, ann)
	exec(`insert into comments (issue_id, author_id, body, created_at)
		select $1, $2, '{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]}',
		       now() - interval '1 day' + (n / 5) * interval '1 second' from generate_series(1, 25) n`, live, ann)
	exec(`insert into issue_events (issue_id, actor_id, kind) values ($1, $3, 'description'), ($2, $3, 'title')`, live, archived, ann)
	exec(`insert into comments (issue_id, author_id, body) values ($1, $2, '{}')`, archived, ann)

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	get := func(user, query string) (int, string) {
		t.Helper()
		return dbtest.Do(t, mux, user, "GET", "/api/workspaces/"+slug+"/activity"+query, nil)
	}
	seen := map[string]bool{}
	comments := 0
	before := ""
	var last time.Time
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("too many pages")
		}
		q := ""
		if before != "" {
			q = "?before=" + url.QueryEscape(before)
		}
		code, body := get(ann, q)
		if code != 200 {
			t.Fatalf("page: %d %s", code, body)
		}
		var p struct {
			Items   []Activity `json:"items"`
			HasMore bool       `json:"has_more"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Items) == 0 || len(p.Items) > activityPage {
			t.Fatalf("page of %d", len(p.Items))
		}
		for _, a := range p.Items {
			if seen[a.ID] {
				t.Fatalf("%s on two pages", a.ID)
			}
			if !last.IsZero() && a.CreatedAt.After(last) {
				t.Fatal("not newest first")
			}
			seen[a.ID], last = true, a.CreatedAt
			switch {
			case a.Type == "comment" && (a.Excerpt == nil || *a.Excerpt != "hello"):
				t.Errorf("comment excerpt %v", a.Excerpt)
			case a.Type == "comment":
				comments++
			case a.Kind != "title" && a.Kind != "created":
				t.Errorf("event kind %q", a.Kind)
			}
			var is struct {
				Identifier string `json:"identifier"`
				Status     Status `json:"status"`
			}
			if json.Unmarshal(a.Issue, &is) != nil || is.Identifier != "ENG-1" || is.Status.Name == "" {
				t.Errorf("issue %s", a.Issue)
			}
		}
		if !p.HasMore {
			break
		}
		before = p.Items[len(p.Items)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	if len(seen) != 95 || comments != 25 {
		t.Errorf("paged %d items (%d comments), want 95 (25)", len(seen), comments)
	}
	if code, _ := get(ann, "?before=yesterday"); code != 400 {
		t.Errorf("bad before: %d", code)
	}
	if code, _ := get(out, ""); code != 404 {
		t.Errorf("non-member: %d", code)
	}
}

func TestActivityCut(t *testing.T) {
	at := func(s int) time.Time { return time.Unix(int64(s), 0) }
	var as []Activity
	for s := 40; s > 0; s-- {
		as = append(as, Activity{CreatedAt: at(s)})
	}
	if got, more := activityCut(as); !more || len(got) != activityPage {
		t.Fatalf("plain cut: %d, %v", len(got), more)
	}
	as[activityPage-1].CreatedAt = as[activityPage].CreatedAt // the 30th and 31st tie: both go to the next page
	if got, more := activityCut(as); !more || len(got) != activityPage-1 {
		t.Fatalf("tie cut: %d, %v", len(got), more)
	}
	if got, more := activityCut(as[:10]); more || len(got) != 10 {
		t.Fatalf("short page: %d, %v", len(got), more)
	}
}
