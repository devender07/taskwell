package aitest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestManualRun walks cases (validation, order, permissions) and a manual run through to failed, then checks the
// activity entry and that a result keeps the case text it ran after the case is edited.
func TestManualRun(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Engineering', 'ENG' from w returning id),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id, rank)
			      select (select id from w), p.id, 1, 'Sign in', (select id from s), 'V' from p returning id)
			select (select id::text from w), (select slug from w)`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")

	mux := http.NewServeMux()
	New(pool, sysconfig.New(pool), Options{}, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int, into any) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		if into != nil {
			if err := json.Unmarshal([]byte(body), into); err != nil {
				t.Fatal(err)
			}
		}
		return body
	}

	body := do(ann, "POST", "/issues/ENG-1/test-cases", url.Values{"steps": {" ", ""}}, 400, nil)
	for _, msg := range []string{"Enter a title.", "Add at least one step.", "Describe the expected result."} {
		if !strings.Contains(body, msg) {
			t.Errorf("missing %q in %s", msg, body)
		}
	}
	do(bob, "POST", "/issues/ENG-1/test-cases", url.Values{"title": {"x"}}, 403, nil)
	do(ann, "POST", "/issues/ENG-9/test-cases", nil, 404, nil)
	var a, b Case
	do(ann, "POST", "/issues/ENG-1/test-cases", url.Values{"title": {"Sign in"}, "steps": {"Open /signin", " Enter email "}, "expected": {"Home shows"}}, 201, &a)
	do(ann, "POST", "/issues/eng-1/test-cases", url.Values{"title": {"Sign out"}, "steps": {"Click sign out"}, "expected": {"Sign-in page"}}, 201, &b)
	if a.Source != "manual" || len(a.Steps) != 2 || a.Steps[1] != "Enter email" || b.Position != a.Position+1 || a.LastResult != nil {
		t.Fatalf("cases %+v %+v", a, b)
	}
	do(ann, "POST", "/issues/ENG-1/test-cases/order", url.Values{"ids": {b.ID}}, 400, nil)
	do(ann, "POST", "/issues/ENG-1/test-cases/order", url.Values{"ids": {b.ID, b.ID}}, 400, nil)
	var cs []Case
	do(ann, "POST", "/issues/ENG-1/test-cases/order", url.Values{"ids": {b.ID, a.ID}}, 200, &cs)
	if len(cs) != 2 || cs[0].ID != b.ID {
		t.Fatalf("order %+v", cs)
	}

	var run RunDetail
	do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"manual"}}, 201, &run)
	if run.Status != "running" || len(run.Results) != 2 || run.Results[0].CaseID != b.ID || run.CostUSD != nil {
		t.Fatalf("run %+v", run)
	}
	// Edited after the run started: the result keeps the old title.
	do(ann, "POST", "/test-cases/"+b.ID, url.Values{"title": {"Log out"}, "steps": {"Click log out"}, "expected": {"Sign-in page"}}, 200, nil)
	rp := "/test-runs/" + run.ID + "/results/"
	do(ann, "POST", rp+run.Results[0].ID, url.Values{"status": {"maybe"}}, 400, nil)
	do(bob, "POST", rp+run.Results[0].ID, url.Values{"status": {"passed"}}, 403, nil)
	do(ann, "POST", rp+run.Results[0].ID, url.Values{"status": {"failed"}, "note": {"Button missing"}}, 200, &run)
	if run.Status != "running" || run.Results[0].Status != "failed" || *run.Results[0].Summary != "Button missing" ||
		run.Results[0].MarkedBy.ID != ann || run.Results[0].Title != "Sign out" {
		t.Fatalf("marked %+v", run.Results[0])
	}
	do(ann, "POST", rp+run.Results[1].ID, url.Values{"status": {"skipped"}}, 200, &run)
	if run.Status != "failed" || run.FinishedAt == nil {
		t.Fatalf("finished %+v", run.Run)
	}
	do(ann, "POST", rp+run.Results[1].ID, url.Values{"status": {"passed"}}, 409, nil)
	do(ann, "POST", "/test-runs/"+run.ID+"/cancel", nil, 409, nil)

	var data string
	if err := pool.QueryRow(ctx, `select data::text from issue_events where kind = 'test_run' and data->>'run_id' = $1`, run.ID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, `"failed": 1`) || !strings.Contains(data, `"skipped": 1`) {
		t.Errorf("event %s", data)
	}
	do(ann, "GET", "/issues/ENG-1/test-cases", nil, 200, &cs)
	if cs[0].Title != "Log out" || cs[0].LastResult == nil || cs[0].LastResult.Status != "failed" || cs[0].LastResult.RunID != run.ID {
		t.Fatalf("last result %+v", cs[0])
	}

	// A second run is cancelled; deleting a case keeps the old run readable.
	var second RunDetail
	do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"manual"}}, 201, &second)
	do(ann, "POST", "/test-runs/"+second.ID+"/cancel", nil, 200, &second)
	if second.Status != "cancelled" {
		t.Fatalf("cancel %+v", second.Run)
	}
	do(ann, "DELETE", "/test-cases/"+b.ID, nil, 204, nil)
	do(ann, "DELETE", "/test-cases/"+b.ID, nil, 404, nil)
	do(bob, "GET", "/test-runs/"+run.ID, nil, 200, &run)
	if len(run.Results) != 2 {
		t.Fatalf("old run %+v", run)
	}
	var page struct {
		Runs    []Run `json:"runs"`
		HasMore bool  `json:"has_more"`
	}
	do(bob, "GET", "/issues/ENG-1/test-runs", nil, 200, &page)
	runs := page.Runs
	if len(runs) != 2 || page.HasMore || runs[0].ID != second.ID || runs[1].Counts.Failed != 1 || runs[1].Counts.Skipped != 1 {
		t.Fatalf("history %+v", page)
	}
	do(bob, "GET", "/issues/ENG-1/test-runs?before="+url.QueryEscape(runs[0].CreatedAt.Format(time.RFC3339Nano)), nil, 200, &page)
	if len(page.Runs) != 1 || page.Runs[0].ID != runs[1].ID {
		t.Fatalf("older history %+v", page)
	}
	do(bob, "GET", "/issues/ENG-1/test-runs?before=yesterday", nil, 400, nil)
	do(ann, "DELETE", "/test-cases/"+a.ID, nil, 204, nil)
	do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"manual"}}, 409, nil)
	do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"x"}}, 400, nil)
}

// TestDeleteIssueWithResults checks that an issue whose cases have run results can be deleted.
func TestDeleteIssueWithResults(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	m := &model{db: pool}
	if _, err := m.createCase(ctx, issueID, ann, caseFields{Title: "Sign in", Steps: []string{"Open"}, Expected: "Home"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.createManual(ctx, wsID, issueID, ann); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `delete from issues where id = $1`, issueID); err != nil {
		t.Fatalf("delete issue with test results: %v", err)
	}
}
