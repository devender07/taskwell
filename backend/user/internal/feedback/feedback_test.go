package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

func null(m json.RawMessage) bool { return m == nil || string(m) == "null" }

// TestFeedback runs the feedback API against the scratch database (see dbtest): validation, states and paging,
// link/unlink with a private project, permissions, the form's on/off/rotate and public submits. Ann is the admin, Bob a
// member outside the private LEAD project, Out not in the workspace.
func TestFeedback(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, out := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Out")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Web', 'WEB', false), ('Leadership', 'LEAD', true)) v(n, k, pr) returning id, key),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id, rank)
			      select (select id from w), p.id, 1, p.key || ' issue', (select id from s), 'V' from p returning id)
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from rate_limits where key like 'feedback:%'`)
	})

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	call := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, path, form)
		if code != want {
			t.Fatalf("%s %s as %s: %d %s, want %d", method, path, user, code, body, want)
		}
		return strings.TrimSpace(body)
	}
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int, into any) string {
		t.Helper()
		body := call(user, method, base+path, form, want)
		if into != nil {
			if err := json.Unmarshal([]byte(body), into); err != nil {
				t.Fatalf("%s %s: %v in %s", method, path, err, body)
			}
		}
		return body
	}
	type page struct {
		Items   []Feedback `json:"items"`
		HasMore bool       `json:"has_more"`
		Total   int        `json:"total"`
	}
	list := func(user, query string) page {
		t.Helper()
		var p page
		do(user, "GET", "/feedback?"+query, nil, 200, &p)
		return p
	}
	count := func() int {
		t.Helper()
		var c struct{ New int }
		do(bob, "GET", "/feedback/count", nil, 200, &c)
		return c.New
	}

	// Validation.
	if got := do(bob, "POST", "/feedback", url.Values{"body": {"  "}, "email": {"nope"}, "name": {strings.Repeat("n", 81)}}, 400, nil); got !=
		`{"errors":{"body":"Write the feedback.","email":"Enter a valid email.","name":"Name must be 80 characters or fewer."}}` {
		t.Errorf("validation: %s", got)
	}
	do(bob, "POST", "/feedback", url.Values{"body": {strings.Repeat("x", 5001)}}, 400, nil)
	do(out, "POST", "/feedback", url.Values{"body": {"hi"}}, 404, nil)

	var f Feedback
	do(bob, "POST", "/feedback", url.Values{"body": {" Dark mode please\nthanks "}, "email": {"Ana@Acme.com"}, "name": {"Ana"}}, 201, &f)
	if f.Body != "Dark mode please\nthanks" || *f.Email != "ana@acme.com" || f.Source != "member" || !null(f.Issue) ||
		f.Archived || !strings.Contains(string(f.CreatedBy), bob) {
		t.Fatalf("created %+v", f)
	}
	if count() != 1 {
		t.Fatal("count after create")
	}

	// Link, unlink, private issues.
	do(bob, "POST", "/feedback/"+f.ID, url.Values{"issue": {"LEAD-1"}}, 400, nil) // Bob can't see LEAD
	do(bob, "POST", "/feedback/"+f.ID, url.Values{"issue": {"nope"}}, 400, nil)
	do(bob, "POST", "/feedback/"+f.ID, url.Values{"issue": {"web-1"}}, 200, &f)
	if !strings.Contains(string(f.Issue), `"identifier":"WEB-1"`) || count() != 0 || len(list(bob, "state=linked").Items) != 1 {
		t.Fatalf("linked %s", f.Issue)
	}
	var fs page
	do(bob, "GET", "/issues/WEB-1/feedback", nil, 200, &fs)
	if len(fs.Items) != 1 || fs.Total != 1 || fs.HasMore {
		t.Fatalf("issue feedback %+v", fs)
	}
	do(bob, "GET", "/issues/WEB-1/feedback?before="+f.ID, nil, 200, &fs)
	if len(fs.Items) != 0 {
		t.Fatalf("issue feedback before itself %+v", fs)
	}
	do(bob, "GET", "/issues/WEB-1/feedback?before=nope", nil, 400, nil)
	do(bob, "GET", "/issues/LEAD-1/feedback", nil, 404, nil)
	do(ann, "POST", "/feedback/"+f.ID, url.Values{"issue": {"LEAD-1"}}, 200, nil)
	if p := list(bob, "state=linked"); len(p.Items) != 1 || !null(p.Items[0].Issue) {
		t.Fatalf("Bob sees the private issue: %+v", p.Items)
	}
	do(ann, "GET", "/issues/LEAD-1/feedback", nil, 200, &fs)
	if len(fs.Items) != 1 || null(fs.Items[0].Issue) {
		t.Fatalf("Ann's LEAD-1 feedback %+v", fs)
	}
	do(bob, "POST", "/feedback/"+f.ID, url.Values{"issue": {""}}, 200, &f)
	if !null(f.Issue) || count() != 1 {
		t.Fatal("unlink")
	}

	// Archive needs feedback.manage; delete too.
	do(bob, "POST", "/feedback/"+f.ID, url.Values{"archived": {"true"}}, 403, nil)
	do(ann, "POST", "/feedback/"+f.ID, url.Values{"archived": {"true"}}, 200, &f)
	if !f.Archived || count() != 0 || len(list(bob, "state=archived").Items) != 1 || len(list(bob, "").Items) != 0 {
		t.Fatal("archive")
	}
	do(ann, "POST", "/feedback/"+f.ID, url.Values{"archived": {"false"}}, 200, &f)
	if f.Archived || count() != 1 {
		t.Fatal("unarchive")
	}
	do(bob, "DELETE", "/feedback/"+f.ID, nil, 403, nil)
	do(ann, "DELETE", "/feedback/"+f.ID, nil, 204, nil)
	do(ann, "DELETE", "/feedback/"+f.ID, nil, 404, nil)
	do(ann, "POST", "/feedback/"+f.ID, url.Values{"issue": {""}}, 404, nil)
	do(ann, "POST", "/feedback/not-a-uuid", nil, 404, nil)

	// Paging and search.
	if _, err := pool.Exec(ctx, `insert into feedback (workspace_id, body, source)
		select $1, 'item ' || n || case when n = 7 then ' needle' else '' end, 'member' from generate_series(1, 60) n`, wsID); err != nil {
		t.Fatal(err)
	}
	p1 := list(bob, "state=new")
	if len(p1.Items) != 50 || !p1.HasMore || p1.Items[0].Body != "item 60" {
		t.Fatalf("page 1: %d more=%v first=%q", len(p1.Items), p1.HasMore, p1.Items[0].Body)
	}
	p2 := list(bob, "before="+p1.Items[49].ID)
	if len(p2.Items) != 10 || p2.HasMore || p2.Items[9].Body != "item 1" {
		t.Fatalf("page 2: %d more=%v", len(p2.Items), p2.HasMore)
	}
	if p := list(bob, "q=NEEDLE"); len(p.Items) != 1 {
		t.Fatalf("search: %d", len(p.Items))
	}
	do(bob, "GET", "/feedback?before=x", nil, 400, nil)

	// The form: off by default, on, rotate, off.
	type form struct {
		Enabled bool    `json:"enabled"`
		Token   *string `json:"token"`
	}
	var fm form
	do(bob, "GET", "/feedback-form", nil, 403, nil)
	do(bob, "POST", "/feedback-form", url.Values{"enabled": {"true"}}, 403, nil)
	do(ann, "GET", "/feedback-form", nil, 200, &fm)
	if fm.Enabled || fm.Token != nil {
		t.Fatalf("form starts %+v", fm)
	}
	do(ann, "POST", "/feedback-form", url.Values{"enabled": {"true"}}, 200, &fm)
	if !fm.Enabled || fm.Token == nil || len(*fm.Token) != 43 {
		t.Fatalf("form on %+v", fm)
	}
	old := *fm.Token
	do(ann, "POST", "/feedback-form", url.Values{"enabled": {"true"}}, 200, &fm)
	if *fm.Token != old {
		t.Fatal("re-enabling changed the token")
	}
	do(ann, "POST", "/feedback-form", url.Values{"rotate": {"true"}}, 200, &fm)
	if *fm.Token == old {
		t.Fatal("rotate kept the token")
	}
	pub := "/api/feedback-forms/"
	call("", "GET", pub+old, nil, 404)
	if got := call("", "GET", pub+*fm.Token, nil, 200); !strings.HasPrefix(got, `{"workspace":`) {
		t.Fatalf("public form %s", got)
	}

	// Public submits.
	before := count()
	if got := call("", "POST", pub+*fm.Token, url.Values{"body": {"hi"}}, 400); got != `{"errors":{"email":"Enter your email."}}` {
		t.Errorf("public validation: %s", got)
	}
	call("", "POST", pub+*fm.Token, url.Values{"body": {"spam"}, "website": {"x"}}, 201)
	call("", "POST", pub+*fm.Token, url.Values{"body": {"From the form"}, "email": {"c@d.co"}}, 201)
	if count() != before+1 {
		t.Fatal("public submit count")
	}
	if p := list(bob, ""); p.Items[0].Source != "form" || !null(p.Items[0].CreatedBy) || p.Items[0].Body != "From the form" {
		t.Fatalf("form feedback %+v", p.Items[0])
	}
	do(ann, "POST", "/feedback-form", url.Values{"enabled": {"false"}}, 200, &fm)
	if fm.Enabled || fm.Token != nil {
		t.Fatalf("form off %+v", fm)
	}
	call("", "POST", pub+old, url.Values{"body": {"x"}, "email": {"c@d.co"}}, 404)

	// Rate limit: 10 per IP, from an address of its own.
	r := func() int {
		req := httptest.NewRequest("POST", pub+"none", strings.NewReader(""))
		req.RemoteAddr = "203.0.113.77:1"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code
	}
	for range 10 {
		if c := r(); c != 404 {
			t.Fatalf("under the limit: %d", c)
		}
	}
	if c := r(); c != 429 {
		t.Fatalf("over the limit: %d", c)
	}
}
