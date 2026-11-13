package issue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	linearCSV = "\uFEFFID,Team,Title,Description,Status,Priority,Assignee,Labels,Due Date\n" +
		"ENG-1,Eng,Fix login,\"Steps:\nopen the page\",In Progress,Urgent,ann@x.test,\"Bug, UI\",2026-12-01\n" +
		"ENG-2,Eng,,No title,Todo,High,,,\n" +
		"ENG-3,Eng,Ship it,,Done,No priority,Sam Smith,ui,2026-12-01T10:00:00Z\n"
	jiraCSV = "Summary,Issue key,Issue id,Status,Priority,Assignee,Labels,Labels,Comment,Comment,Due date\n" +
		"Crash on save,WEB-7,10007,Won't Do,Highest,Ann,backend,crash,\"05/Mar/26 9:15 AM;jdoe;Seen it too\",plain note,05/Mar/26 12:00 AM\n"
)

func TestParseCSV(t *testing.T) {
	for _, c := range []struct{ name, csv, source string }{
		{"linear", linearCSV, "linear"},
		{"jira", jiraCSV, "jira"},
		{"plain", "Name,Body\nA,b\n", "csv"},
	} {
		t.Run(c.name, func(t *testing.T) {
			source, _, err := parseCSV([]byte(c.csv))
			if err != nil || source != c.source {
				t.Errorf("got %q %v, want %q", source, err, c.source)
			}
		})
	}
	_, is, _ := parseCSV([]byte(jiraCSV))
	j := is[0]
	if j.title != "Crash on save" || j.footer != "Imported from Jira WEB-7." || strings.Join(j.labels, "|") != "backend|crash" ||
		len(j.comments) != 2 || j.comments[0] != (sourceComment{"jdoe", "05/Mar/26 9:15 AM", "Seen it too"}) ||
		j.comments[1] != (sourceComment{body: "plain note"}) {
		t.Errorf("jira row: %+v", j)
	}
	for _, c := range []struct {
		name, csv string
		want      error
	}{
		{"no title", "Foo,Bar\n1,2\n", errNoTitleColumn},
		{"empty", "", errNoTitleColumn},
		{"binary", "Title\nx\x00y\n", errBadCSV},
		{"not utf-8", "Title\n\xff\n", errBadCSV},
		{"too many rows", "Title\n" + strings.Repeat("x\n", maxImportRows+1), errTooManyRows},
	} {
		if _, _, err := parseCSV([]byte(c.csv)); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	if _, is, err := parseCSV([]byte("Title\n" + strings.Repeat("x\n", maxImportRows))); err != nil || len(is) != maxImportRows {
		t.Errorf("5,000 rows: %d %v", len(is), err)
	}
}

func TestMapStatus(t *testing.T) {
	ss := []Status{{ID: "todo", Name: "Todo", Category: "unstarted"}, {ID: "back", Name: "Backlog", Category: "backlog"},
		{ID: "doing", Name: "In progress", Category: "started"}, {ID: "done", Name: "Done", Category: "completed"},
		{ID: "done2", Name: "Shipped", Category: "completed"}, {ID: "no", Name: "Canceled", Category: "canceled"}}
	for _, c := range []struct{ name, category, want string }{
		{"in progress", "", "doing"}, {"Shipped", "", "done2"}, {"Closed", "", "done"}, {"Resolved", "", "done"},
		{"Won't Do", "", "no"}, {"Duplicate", "", "no"}, {"In Review", "", "doing"}, {"Icebox", "", "back"},
		{"Triage", "", "back"}, {"Whatever", "", "todo"}, {"", "", "todo"},
		{"Open", "default", "todo"}, {"Not planned", "canceled", "no"}, {"Closed", "completed", "done"},
	} {
		if got := mapStatus(c.name, c.category, ss).ID; got != c.want {
			t.Errorf("%q/%q: got %s, want %s", c.name, c.category, got, c.want)
		}
	}
	if got := mapStatus("Done", "", ss[:1]).ID; got != "todo" {
		t.Errorf("no completed status: got %s, want the default", got)
	}
}

func TestMapPriority(t *testing.T) {
	for in, want := range map[string]int{"Urgent": 1, "highest": 1, "Blocker": 1, "critical": 1, "High": 2, "Medium": 3,
		"normal": 3, "Low": 4, "Lowest": 4, "minor": 4, "Trivial": 4, "0": 0, "3": 3, "5": 0, "No priority": 0, "": 0} {
		if got := mapPriority(in); got != want {
			t.Errorf("%q: got %d, want %d", in, got, want)
		}
	}
	for in, want := range map[string]string{"2026-12-01": "2026-12-01", "2026-12-01T23:00:00Z": "2026-12-01",
		"05/Mar/26 9:15 AM": "2026-03-05", "tomorrow": ""} {
		if got := parseDue(in); got == nil && want != "" || got != nil && *got != want {
			t.Errorf("%q: got %v, want %q", in, got, want)
		}
	}
}

func TestParseRepo(t *testing.T) {
	for in, want := range map[string]string{"owner/name": "owner/name", " https://github.com/o-1/n.js/ ": "o-1/n.js",
		"github.com/o/n.git": "o/n", "https://www.github.com/o/n/issues/3": "o/n", "o": "", "o/..": "", "gitlab.com/o/n": "",
		"o/n?x": ""} {
		if got, ok := parseRepo(in); ok && got != want || !ok && want != "" {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
}

// githubServer serves o/r's issues: #1 open with a comment, #2 a pull request, #3 closed as not planned, #4 closed;
// x/missing is a 404 and the token "bad" a 401.
func githubServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("Authorization") == "Bearer bad":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/repos/o/r/issues" && r.URL.Query().Get("state") == "all":
			fmt.Fprint(w, `[{"number":4,"title":"Done thing","state":"closed","state_reason":"completed","labels":[]},
				{"number":3,"title":"Nope","state":"closed","state_reason":"not_planned","labels":[{"name":"wontfix"}]},
				{"number":2,"title":"A PR","state":"open","pull_request":{"url":"x"}},
				{"number":1,"title":"Open bug","body":"It breaks","state":"open","comments":1,"labels":[{"name":"bug"}]}]`)
		case r.URL.Path == "/repos/o/r/issues":
			fmt.Fprint(w, `[{"number":1,"title":"Open bug","body":"It breaks","state":"open","comments":1,"labels":[{"name":"bug"}]}]`)
		case r.URL.Path == "/repos/o/r/issues/1/comments":
			fmt.Fprint(w, `[{"body":"Me too","user":{"login":"octo"},"created_at":"2026-01-02T03:04:05Z"}]`)
		case r.URL.Path == "/repos/o/limited/issues":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	old := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = old })
}

func TestFetchGitHub(t *testing.T) {
	githubServer(t)
	ctx := context.Background()
	is, err := fetchGitHub(ctx, "o/r", "", true, true)
	if err != nil || len(is) != 3 {
		t.Fatalf("got %d issues, %v", len(is), err)
	}
	if is[0].row != 1 || is[0].category != "default" || len(is[0].comments) != 1 || is[0].comments[0].author != "octo" ||
		is[0].footer != "Imported from GitHub o/r#1." || is[1].category != "canceled" || is[2].category != "completed" {
		t.Errorf("issues: %+v", is)
	}
	if is, _ := fetchGitHub(ctx, "o/r", "", false, false); len(is) != 1 || is[0].comments != nil || is[0].commentCount != 1 {
		t.Errorf("open only, no comments: %+v", is)
	}
	for repo, want := range map[string]error{"x/missing": errNoRepo, "o/limited": errRateLimit} {
		if _, err := fetchGitHub(ctx, repo, "", false, false); !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", repo, err, want)
		}
	}
	if _, err := fetchGitHub(ctx, "o/r", "bad", false, false); !errors.Is(err, errBadToken) {
		t.Errorf("bad token: %v", err)
	}
	githubAPI = "http://127.0.0.1:1"
	if _, err := fetchGitHub(ctx, "o/r", "", false, false); !errors.Is(err, errGitHubDown) {
		t.Errorf("down: %v", err)
	}
}

// TestImport imports CSVs and a GitHub repository into WEB, which has an issue already; Ann is the admin, Otto a
// member without issues.create.
func TestImport(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Otto")
	var wsID, slug, annEmail string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, archived_at)
			      select id, n, k, a from w, (values ('Web', 'WEB', null), ('Old', 'OLD', now())) v(n, k, a) returning id),
			l as (insert into labels (workspace_id, name, color) select id, 'bug', '#ff0000' from w)
			select w.id::text, slug, (select email from users where id = $1) from w`, ann).Scan(&wsID, &slug, &annEmail)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, otto, "member")
	if _, err := pool.Exec(ctx, `update workspace_roles set permissions = array_remove(permissions, 'issues.create')
		where workspace_id = $1 and builtin = 'member'`, wsID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	code, body := dbtest.Do(t, mux, ann, "POST", base+"/projects/WEB/issues", url.Values{"title": {"Existing"}})
	if code != 201 {
		t.Fatal(body)
	}
	upload := func(user, csv string, fields map[string]string, want int) summary {
		t.Helper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if csv != "" {
			fw, _ := mw.CreateFormFile("file", "export.csv")
			fw.Write([]byte(csv))
		}
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		mw.Close()
		r := httptest.NewRequest("POST", base+"/import/csv", &buf)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.Header.Set("X-Test-User", user)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("import: %d %s, want %d", w.Code, w.Body, want)
		}
		var s summary
		json.Unmarshal(w.Body.Bytes(), &s)
		return s
	}
	count := func() (issues, comments, labels int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `select (select count(*) from issues where workspace_id = $1),
			(select count(*) from comments c join issues i on i.id = c.issue_id where i.workspace_id = $1),
			(select count(*) from labels where workspace_id = $1)`, wsID).Scan(&issues, &comments, &labels); err != nil {
			t.Fatal(err)
		}
		return
	}

	// A Linear dry run writes nothing.
	csv := strings.Replace(linearCSV, "ann@x.test", annEmail, 1)
	s := upload(ann, csv, map[string]string{"project": "web", "dry_run": "1"}, 200)
	if s.Source != "linear" || s.Total != 3 || len(s.Skipped) != 1 || s.Skipped[0] != (skipped{2, "Title is required."}) ||
		strings.Join(s.NewLabels, ",") != "UI" || strings.Join(s.UnmatchedAssignees, ",") != "Sam Smith" || s.Imported != nil {
		t.Errorf("dry run: %+v", s)
	}
	if i, c, l := count(); i != 1 || c != 0 || l != 1 {
		t.Errorf("dry run wrote: %d issues, %d comments, %d labels", i, c, l)
	}

	// The import lands in WEB after the existing issue.
	s = upload(ann, csv, map[string]string{"project": "WEB"}, 201)
	if s.Imported == nil || *s.Imported != 2 || s.First != "WEB-2" {
		t.Errorf("import: %+v", s)
	}
	code, body = dbtest.Do(t, mux, ann, "GET", base+"/issues/WEB-2", nil)
	var i Issue
	json.Unmarshal([]byte(body), &i)
	if code != 200 || i.Title != "Fix login" || i.Status.Name != "In progress" || i.Priority != 1 || i.Assignee == nil ||
		i.Assignee.ID != ann || len(i.Labels) != 2 || i.DueDate == nil || *i.DueDate != "2026-12-01" ||
		!strings.Contains(string(i.Description), "Imported from Linear ENG-1.") {
		t.Errorf("WEB-2: %d %s", code, body)
	}
	if code, body = dbtest.Do(t, mux, ann, "GET", base+"/issues/WEB-3", nil); !strings.Contains(body, `"title":"Ship it"`) {
		t.Errorf("WEB-3: %d %s", code, body)
	}

	// Jira: comments are the importer's, headed by their author.
	s = upload(ann, jiraCSV, map[string]string{"project": "WEB"}, 201)
	if s.Source != "jira" || s.First != "WEB-4" || s.Comments != 2 || s.Statuses[0].From != "Won't Do" || s.Statuses[0].To.Name != "Cancelled" {
		t.Errorf("jira: %+v", s)
	}
	var head string
	if err := pool.QueryRow(ctx, `select c.body->'content'->0->'content'->0->>'text' from comments c join issues i on i.id = c.issue_id
		where i.workspace_id = $1 and c.author_id = $2 order by c.id limit 1`, wsID, ann).Scan(&head); err != nil || head != "jdoe" {
		t.Errorf("comment head %q %v", head, err)
	}

	// GitHub.
	githubServer(t)
	form := url.Values{"repo": {"https://github.com/o/r"}, "project": {"WEB"}, "closed": {"1"}}
	code, body = dbtest.Do(t, mux, ann, "POST", base+"/import/github", form)
	if code != 201 || !strings.Contains(body, `"first":"WEB-5"`) || !strings.Contains(body, `"imported":3`) || !strings.Contains(body, `"comments":1`) {
		t.Errorf("github: %d %s", code, body)
	}

	// Failures.
	upload(otto, csv, map[string]string{"project": "WEB"}, 403)
	upload(ann, csv, map[string]string{"project": "OLD"}, 409)
	upload(ann, csv, map[string]string{"project": "NOPE"}, 404)
	upload(ann, "", map[string]string{}, 400)
	upload(ann, "Title\n,\n", map[string]string{"project": "WEB"}, 400)
	for _, c := range []struct {
		form url.Values
		code int
		want string
	}{
		{url.Values{"repo": {"nope"}}, 400, `{"errors":{"project":"Choose a project.","repo":"Enter a repository like owner/name."}}`},
		{url.Values{"repo": {"x/missing"}, "project": {"WEB"}}, 400, `{"errors":{"repo":"Repository not found or not accessible."}}`},
		{url.Values{"repo": {"o/r"}, "project": {"WEB"}, "token": {"bad"}}, 400, `{"errors":{"token":"This token isn't valid."}}`},
		{url.Values{"repo": {"o/limited"}, "project": {"WEB"}}, 429, "rate limit"},
		{url.Values{"repo": {"o/empty"}, "project": {"WEB"}}, 400, "Repository not found"},
	} {
		if code, body := dbtest.Do(t, mux, ann, "POST", base+"/import/github", c.form); code != c.code || !strings.Contains(body, c.want) {
			t.Errorf("%v: %d %s", c.form, code, body)
		}
	}
	if i, _, _ := count(); i != 7 {
		t.Errorf("%d issues, want 7", i)
	}
}
