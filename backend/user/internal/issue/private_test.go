package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestPrivate: a private project's issues answer 404 to someone outside it and stay out of their lists, search,
// My issues, activity and the relations of issues they can see; they can't be assigned it, and mentioning them
// doesn't notify them. Ann is the admin, Mia a member of the private LEAD project, Otto outside it.
func TestPrivate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateDefault(ctx, tx, ann, "Ann"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Leadership', 'LEAD', true), ('Web', 'WEB', false)) v(n, k, pr) returning id, key)
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, otto, "member")
	if _, err := pool.Exec(ctx, `insert into project_members (project_id, user_id)
		select id, $2 from projects where workspace_id = $1 and key = 'LEAD'`, wsID, mia); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member)
	h.Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}
	do(mia, "POST", "/projects/LEAD/issues", url.Values{"title": {"Secret plan"}}, 201)
	do(mia, "POST", "/projects/WEB/issues", url.Values{"title": {"Public page"}}, 201)
	do(mia, "POST", "/issues/WEB-1/links", url.Values{"type": {"blocks"}, "issue": {"LEAD-1"}}, 201)
	mention := func(ids ...string) string {
		var ns []string
		for _, id := range ids {
			ns = append(ns, fmt.Sprintf(`{"type":"mention","attrs":{"id":%q,"label":"x"}}`, id))
		}
		return `{"type":"doc","content":[{"type":"paragraph","content":[` + strings.Join(ns, ",") + `]}]}`
	}
	var comment struct{ ID string }
	if err := json.Unmarshal([]byte(do(mia, "POST", "/issues/LEAD-1/comments", url.Values{"body": {mention(otto, ann)}}, 201)), &comment); err != nil {
		t.Fatal(err)
	}
	// Assigned before it went private: kept, but still hidden from them.
	if _, err := pool.Exec(ctx, `update issues set assignee_id = $1 where workspace_id = $2`, otto, wsID); err != nil {
		t.Fatal(err)
	}

	// Mentioning Otto neither notified him nor made him a watcher; Ann was notified.
	var ottoNotes, annNotes, ottoWatches int
	if err := pool.QueryRow(ctx, `select (select count(*) from notifications where user_id = $1),
		(select count(*) from notifications where user_id = $2), (select count(*) from issue_watchers where user_id = $1)`,
		otto, ann).Scan(&ottoNotes, &annNotes, &ottoWatches); err != nil {
		t.Fatal(err)
	}
	if ottoNotes != 0 || annNotes != 1 || ottoWatches != 0 {
		t.Errorf("otto notes %d watches %d, ann notes %d", ottoNotes, ottoWatches, annNotes)
	}

	// Otto: not found, and left out of every list.
	for _, r := range []struct{ method, path string }{
		{"GET", "/issues/LEAD-1"}, {"POST", "/issues/LEAD-1"}, {"DELETE", "/issues/LEAD-1"}, {"GET", "/issues/LEAD-1/timeline"},
		{"POST", "/issues/LEAD-1/comments"}, {"POST", "/issues/LEAD-1/watch"}, {"GET", "/projects/LEAD/issues"},
		{"POST", "/projects/LEAD/issues"}, {"POST", "/comments/" + comment.ID}, {"DELETE", "/comments/" + comment.ID},
	} {
		body := do(otto, r.method, r.path, url.Values{"title": {"x"}, "body": {mention()}}, 404)
		if !strings.Contains(body, "not found.") {
			t.Errorf("%s %s: %s", r.method, r.path, body)
		}
	}
	do(otto, "POST", "/issues/WEB-1/links", url.Values{"type": {"blocks"}, "issue": {"LEAD-1"}}, 400)
	for _, path := range []string{"/my-issues", "/issues?q=", "/issues?q=LEAD-1", "/search?q=", "/search?q=secret", "/search?q=LEAD",
		"/activity", "/issues/WEB-1", "/issues/WEB-1/timeline"} {
		if body := do(otto, "GET", path, nil, 200); strings.Contains(body, "LEAD") || strings.Contains(body, "Secret") {
			t.Errorf("outsider %s shows the private project: %s", path, body)
		}
	}
	if s, msg, err := h.Join(ctx, nil, otto, slug, "LEAD-1"); s != nil || msg != errIssueNotFound || err != nil {
		t.Errorf("outsider joined live editing: %v %q %v", s, msg, err)
	}

	// Mia and Ann see all of it.
	for _, u := range []string{mia, ann} {
		do(u, "GET", "/issues/LEAD-1", nil, 200)
		for _, path := range []string{"/search?q=secret", "/activity", "/issues/WEB-1", "/issues/WEB-1/timeline", "/projects/LEAD/issues"} {
			if body := do(u, "GET", path, nil, 200); !strings.Contains(body, "LEAD-1") {
				t.Errorf("%s %s misses the private issue: %s", u, path, body)
			}
		}
		if s, _, err := h.Join(ctx, nil, u, slug, "LEAD-1"); s == nil || err != nil {
			t.Errorf("%s can't join live editing: %v", u, err)
		}
	}

	// Assignees must see the project; the one assigned before stays when the field is sent unchanged.
	if body := do(mia, "POST", "/issues/WEB-1", url.Values{"assignee_id": {mia}}, 200); !strings.Contains(body, mia) {
		t.Errorf("assign public: %s", body)
	}
	do(mia, "POST", "/issues/LEAD-1", url.Values{"assignee_id": {otto}, "title": {"Still secret"}}, 200)
	do(mia, "POST", "/issues/LEAD-1", url.Values{"assignee_id": {""}}, 200)
	if body := do(mia, "POST", "/issues/LEAD-1", url.Values{"assignee_id": {otto}}, 400); !strings.Contains(body, "They can't see this project.") {
		t.Errorf("assign outsider: %s", body)
	}
	do(mia, "POST", "/projects/LEAD/issues", url.Values{"title": {"x"}, "assignee_id": {otto}}, 400)
	do(mia, "POST", "/projects/LEAD/issues", url.Values{"title": {"x"}, "assignee_id": {ann}}, 201)
}
