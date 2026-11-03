package aitest

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestPrivate: the test cases, runs, results and browser sessions of a private project's issue are not found for
// someone outside it, whatever their testing permissions; the admin sees them.
func TestPrivate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	otto := dbtest.User(t, pool, "Otto")
	dbtest.Join(t, pool, wsID, otto, "member")
	var slug string
	if err := pool.QueryRow(ctx, `update projects set private = true where workspace_id = $1 returning
		(select slug from workspaces where id = $1)`, wsID).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update workspace_roles set permissions = permissions || '{tests.manage,ai_tests.run}'
		where workspace_id = $1 and builtin = 'member'`, wsID); err != nil {
		t.Fatal(err)
	}
	m := &model{db: pool}
	caseID, err := m.createCase(ctx, issueID, ann, caseFields{Title: "Sign in", Steps: []string{"Sign in"}, Expected: "Home"})
	if err != nil {
		t.Fatal(err)
	}
	runID, err := m.createManual(ctx, wsID, issueID, ann)
	if err != nil {
		t.Fatal(err)
	}
	var resultID string
	if err := pool.QueryRow(ctx, `select id::text from test_results where run_id = $1`, runID).Scan(&resultID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.createSite(ctx, wsID, slug, ann, "https://app.acme.test", "app.acme.test"); err != nil {
		t.Fatal(err)
	}
	sessionID := "00000000-0000-4000-8000-0000000000aa"
	if err := m.createSession(ctx, sessionID, wsID, issueID, otto, "http://tester.test", "tok"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(pool, sysconfig.New(pool), Options{}, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	form := url.Values{"title": {"x"}, "steps": {"x"}, "expected": {"x"}, "kind": {"manual"}, "status": {"passed"},
		"script": {`{"steps":[{"tool":"navigate","url":"https://app.acme.test"}],"check":"x"}`}, "action": {"snapshot"}, "case_id": {caseID}, "check": {"x"}}
	for _, r := range []struct{ method, path string }{
		{"GET", "/issues/ENG-1/test-cases"}, {"POST", "/issues/ENG-1/test-cases"}, {"GET", "/issues/ENG-1/test-runs"},
		{"POST", "/issues/ENG-1/test-runs"}, {"GET", "/issues/ENG-1/playwright"}, {"POST", "/issues/ENG-1/browser-sessions"},
		{"POST", "/test-cases/" + caseID}, {"POST", "/test-cases/" + caseID + "/script"}, {"DELETE", "/test-cases/" + caseID + "/script"},
		{"DELETE", "/test-cases/" + caseID}, {"GET", "/test-runs/" + runID}, {"GET", "/test-runs/" + runID + "/live"},
		{"POST", "/test-runs/" + runID + "/results/" + resultID}, {"POST", "/test-runs/" + runID + "/cancel"},
		{"POST", "/browser-sessions/" + sessionID + "/actions"}, {"GET", "/browser-sessions/" + sessionID + "/screenshot"},
		{"POST", "/browser-sessions/" + sessionID + "/save"}, {"DELETE", "/browser-sessions/" + sessionID},
	} {
		if code, body := dbtest.Do(t, mux, otto, r.method, base+r.path, form); code != http.StatusNotFound {
			t.Errorf("%s %s: %d %s, want 404", r.method, r.path, code, body)
		}
	}
	for _, path := range []string{"/issues/ENG-1/test-cases", "/test-runs/" + runID} {
		if code, body := dbtest.Do(t, mux, ann, "GET", base+path, nil); code != http.StatusOK {
			t.Errorf("admin %s: %d %s", path, code, body)
		}
	}
}
