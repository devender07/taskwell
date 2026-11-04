package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCreateIssue checks that create_issue resolves status, label and assignee names to ids and sends the API the
// form it expects, with the caller's token.
func TestCreateIssue(t *testing.T) {
	const token = "Bearer tth_test"
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			http.Error(w, `{"error":"Not signed in."}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/workspaces/acme/statuses":
			_, _ = w.Write([]byte(`[{"id":"s1","name":"Backlog"},{"id":"s2","name":"In Progress"}]`))
		case "GET /api/workspaces/acme/labels":
			_, _ = w.Write([]byte(`[{"id":"l1","name":"Bug"},{"id":"l2","name":"Frontend"},{"id":"l3","name":"Docs"}]`))
		case "GET /api/workspaces/acme/members":
			_, _ = w.Write([]byte(`{"members":[{"id":"u1","name":"Ada","email":"ada@example.com"},{"id":"u2","name":"Bob","email":"bob@example.com"}],"invites":[]}`))
		case "POST /api/workspaces/acme/projects/ENG/issues":
			_ = r.ParseForm()
			got = r.PostForm
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"identifier":"ENG-7","title":"Fix login","status":{"id":"s2","name":"In Progress"},
				"assignee":{"id":"u2","name":"Bob"},"priority":2,"labels":[{"id":"l1","name":"Bug"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	cs := connect(t, srv, token)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_issue", Arguments: map[string]any{
		"workspace": "acme", "project": "ENG", "title": " Fix login ", "description": "Steps:\n1. Open\n\nExpected: works",
		"status": "in progress", "assignee": "BOB@example.com", "priority": "High", "due_date": "2026-12-01",
		"labels": []string{"bug", "Frontend"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError {
		t.Fatalf("tool error: %s", text)
	}
	want := url.Values{"title": {"Fix login"}, "status_id": {"s2"}, "assignee_id": {"u2"}, "priority": {"2"},
		"due_date": {"2026-12-01"}, "label_ids": {"l1", "l2"},
		"description": {`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Steps:"},` +
			`{"type":"hardBreak"},{"type":"text","text":"1. Open"}]},{"type":"paragraph","content":[{"type":"text","text":"Expected: works"}]}]}`}}
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			t.Errorf("form %s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("form has %d fields, want %d: %v", len(got), len(want), got)
	}
	var out issueOut
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.URL != "https://app.test/acme/issue/ENG-7" || out.Priority != "high" {
		t.Errorf("result %s: %v", text, err)
	}

	// An unknown name answers with the choices.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "create_issue", Arguments: map[string]any{
		"workspace": "acme", "project": "ENG", "title": "x", "status": "Done"}})
	if err != nil {
		t.Fatal(err)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.Contains(text, "Backlog, In Progress") {
		t.Errorf("unknown status: error=%v %q", res.IsError, text)
	}
}

// connect serves the tools against the fake API with the token and returns a client connected to them.
func connect(t *testing.T, srv *httptest.Server, token string) *mcp.ClientSession {
	t.Helper()
	tl := &tools{api: &api{base: srv.URL, auth: token, client: srv.Client()}, app: "https://app.test"}
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := tl.server(nil).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call calls the tool and returns its text and whether it is an error.
func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

// TestCreateTestCases checks that create_test_cases posts each case's steps as repeated fields, in order, and that
// a failure names the case and the ones already created.
func TestCreateTestCases(t *testing.T) {
	var forms []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path != "POST /api/workspaces/acme/issues/ENG-1/test-cases" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		forms = append(forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("title") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":{"title":"Enter a title."}}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "c" + strconv.Itoa(len(forms)), "title": r.PostForm.Get("title"),
			"steps": r.PostForm["steps"], "expected": r.PostForm.Get("expected")})
	}))
	defer srv.Close()
	cs := connect(t, srv, "Bearer tth_test")

	text, isErr := call(t, cs, "create_test_cases", map[string]any{"workspace": "acme", "identifier": "ENG-1", "cases": []map[string]any{
		{"title": "Sign in", "steps": []string{"Open /signin", "Enter the code"}, "expected": "Home shows"},
		{"title": "Sign out", "steps": []string{"Click sign out"}, "expected": "Sign-in page shows"},
		{"title": "", "steps": []string{"x"}, "expected": "y"},
		{"title": "Never sent", "steps": []string{"x"}, "expected": "y"},
	}})
	if !isErr || !strings.Contains(text, `Case 3 "" failed: title: Enter a title.`) ||
		!strings.Contains(text, `Created 2: "Sign in" (c1), "Sign out" (c2).`) {
		t.Errorf("error=%v %q", isErr, text)
	}
	if len(forms) != 3 || !slices.Equal(forms[0]["steps"], []string{"Open /signin", "Enter the code"}) || forms[1].Get("title") != "Sign out" {
		t.Errorf("forms %v", forms)
	}
}

// TestGetTestRun checks that get_test_run with wait_seconds polls until the run finishes and makes screenshot paths
// app links.
func TestGetTestRun(t *testing.T) {
	pollEvery = time.Millisecond
	defer func() { pollEvery = 3 * time.Second }()
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path != "GET /api/workspaces/acme/test-runs/r1" {
			http.NotFound(w, r)
			return
		}
		polls++
		status, result := "running", "running"
		if polls == 2 {
			status, result = "failed", "failed"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","kind":"ai","issue":{"identifier":"ENG-1","title":"Login"},"status":"` + status + `",
			"cost_usd":0.12,"counts":{"failed":1},"results":[{"id":"x1","title":"Sign in","steps":["Open"],"expected":"Home",
			"status":"` + result + `","summary":"No button","reproduction":["Open /signin"],
			"log":[{"screenshot_url":"/api/files/f1/shot.jpg"},{"screenshot_url":null}]}]}`))
	}))
	defer srv.Close()
	cs := connect(t, srv, "Bearer tth_test")

	text, isErr := call(t, cs, "get_test_run", map[string]any{"workspace": "acme", "run_id": "r1", "wait_seconds": 5})
	var out runOut
	if err := json.Unmarshal([]byte(text), &out); isErr || err != nil {
		t.Fatalf("error=%v %q: %v", isErr, text, err)
	}
	if polls != 2 || out.Status != "failed" || out.URL != "https://app.test/acme/issue/ENG-1" || len(out.Results) != 1 ||
		!slices.Equal(out.Results[0].Screenshots, []string{"https://app.test/api/files/f1/shot.jpg"}) || out.Results[0].Summary != "No button" {
		t.Errorf("polls=%d result %s", polls, text)
	}
}

// TestTestsView checks the MCP Apps wiring: the view resource, the tools linked to it, and the screenshot loader
// fetching only Taskwell files, with the caller's token.
func TestTestsView(t *testing.T) {
	png := []byte("\x89PNG fake")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/files/f1/step.png" || r.Header.Get("Authorization") != "Bearer tth_x" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer srv.Close()
	cs := connect(t, srv, "Bearer tth_x")
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: testsViewURI})
	if err != nil {
		t.Fatal(err)
	}
	if c := res.Contents[0]; c.MIMEType != appMIMEType || !strings.Contains(c.Text, "ext-apps@") {
		t.Errorf("view resource = %q, %d bytes", c.MIMEType, len(c.Text))
	}
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	linked := map[string]string{}
	for _, tl := range tools.Tools {
		if ui, ok := tl.Meta["ui"].(map[string]any); ok {
			linked[tl.Name] = fmt.Sprint(ui["resourceUri"], ui["visibility"])
		}
	}
	want := map[string]string{"get_test_run": testsViewURI + "<nil>", "list_test_cases": testsViewURI + "<nil>",
		"get_test_screenshot": testsViewURI + "[app]"}
	if !maps.Equal(linked, want) {
		t.Errorf("linked tools = %v, want %v", linked, want)
	}

	shot, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_test_screenshot", Arguments: map[string]any{"url": "https://app.test/api/files/f1/step.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if img, ok := shot.Content[0].(*mcp.ImageContent); !ok || img.MIMEType != "image/png" || !bytes.Equal(img.Data, png) {
		t.Errorf("screenshot = %#v", shot.Content[0])
	}
	for _, u := range []string{"https://evil.test/api/files/f1/step.png", "https://app.test/api/me", "https://app.test/api/files/../me"} {
		if text, isErr := call(t, cs, "get_test_screenshot", map[string]any{"url": u}); !isErr {
			t.Errorf("get_test_screenshot(%s) = %s, want an error", u, text)
		}
	}
}

// TestSaveTestScript checks that save_test_script sends the steps and check as the script JSON field (and playwright
// only when given), and that get_playwright_spec answers the spec's text.
func TestSaveTestScript(t *testing.T) {
	const token = "Bearer tth_test"
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/workspaces/acme/test-cases/c1/script":
			_ = r.ParseForm()
			got = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"c1","title":"Sign in","steps":["Open"],"expected":"Home","recorded":true,"has_playwright":true}`))
		case "GET /api/workspaces/acme/issues/ENG-1/playwright":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("import { test, expect } from '@playwright/test';\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cs := connect(t, srv, token)

	text, isErr := call(t, cs, "save_test_script", map[string]any{"workspace": "acme", "case_id": "c1", "check": "Home",
		"steps": []map[string]any{{"tool": "navigate", "url": "https://app.test/login"},
			{"tool": "fill_password", "target": map[string]string{"css": "", "tag": "input", "name": "Password"}, "account": "QA"},
			{"tool": "assert_no_text", "text": "Error"}},
		"playwright": "await page.goto('https://app.test/login');"})
	if isErr || !strings.Contains(text, `"has_playwright":true`) {
		t.Fatalf("save: %v %s", isErr, text)
	}
	want := `{"check":"Home","steps":[{"tool":"navigate","url":"https://app.test/login"},` +
		`{"tool":"fill_password","target":{"css":"","tag":"input","name":"Password"},"account":"QA"},{"tool":"assert_no_text","text":"Error"}]}`
	if got.Get("script") != want || got.Get("playwright") != "await page.goto('https://app.test/login');" {
		t.Errorf("form %v", got)
	}
	if _, _ = call(t, cs, "save_test_script", map[string]any{"workspace": "acme", "case_id": "c1", "check": "Home",
		"steps": []map[string]any{{"tool": "navigate", "url": "https://app.test"}}}); got.Has("playwright") {
		t.Errorf("playwright sent when left out: %v", got)
	}
	text, isErr = call(t, cs, "get_playwright_spec", map[string]any{"workspace": "acme", "identifier": "ENG-1"})
	if isErr || text != "import { test, expect } from '@playwright/test';\n" {
		t.Errorf("spec: %v %q", isErr, text)
	}
}

// TestBrowserSession checks that browser_fill_password posts the action with its ref and account and answers the
// page as text, and that browser_save posts the case and check.
func TestBrowserSession(t *testing.T) {
	forms := map[string]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		forms[r.Method+" "+r.URL.Path] = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/workspaces/acme/browser-sessions/s1/actions":
			_, _ = w.Write([]byte(`{"page":{"url":"https://app.test/login","title":"Sign in","elements":"URL: https://app.test/login\n[2] input type=password \"Password\""},"steps":2}`))
		case "POST /api/workspaces/acme/browser-sessions/s1/save":
			_, _ = w.Write([]byte(`{"id":"c1","title":"Sign in","steps":["Open"],"expected":"Home","recorded":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cs := connect(t, srv, "Bearer tth_test")

	text, isErr := call(t, cs, "browser_fill_password", map[string]any{"workspace": "acme", "session_id": "s1", "ref": 2, "account": "QA"})
	if isErr || !strings.Contains(text, "Recorded steps: 2\n") || !strings.Contains(text, `[2] input type=password "Password"`) {
		t.Fatalf("fill_password: %v %s", isErr, text)
	}
	got := forms["POST /api/workspaces/acme/browser-sessions/s1/actions"]
	if want := (url.Values{"action": {"fill_password"}, "ref": {"2"}, "account": {"QA"}}); !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("action form %v, want %v", got, want)
	}
	text, isErr = call(t, cs, "browser_save", map[string]any{"workspace": "acme", "session_id": "s1", "case_id": "c1", "check": "Welcome"})
	if isErr || !strings.Contains(text, `"recorded":true`) {
		t.Fatalf("save: %v %s", isErr, text)
	}
	if got := forms["POST /api/workspaces/acme/browser-sessions/s1/save"]; got.Get("case_id") != "c1" || got.Get("check") != "Welcome" {
		t.Errorf("save form %v", got)
	}
}

// TestOAuthDiscovery checks that /mcp without a token points at the protected resource metadata, and what that says.
func TestOAuthDiscovery(t *testing.T) {
	w := httptest.NewRecorder()
	Handler("http://api.test", "https://app.test/", "https://api.app.test/", http.DefaultClient).ServeHTTP(w, httptest.NewRequest("POST", "/mcp", nil))
	if want := `Bearer resource_metadata="https://api.app.test/.well-known/oauth-protected-resource/mcp"`; w.Code != 401 || w.Header().Get("WWW-Authenticate") != want {
		t.Fatalf("no token: %d %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
	w = httptest.NewRecorder()
	Metadata("https://api.app.test/", "https://api.app.test")(w, httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/mcp", nil))
	want := `{"authorization_servers":["https://api.app.test"],"bearer_methods_supported":["header"],"resource":"https://api.app.test/mcp","scopes_supported":["taskwell"]}`
	if w.Code != 200 || w.Body.String() != want {
		t.Fatalf("metadata: %d %s", w.Code, w.Body)
	}
}

// TestReorderIssues checks that reorder_issues moves each listed issue right after the one before it, in order, and
// that list_issues asks for the manual order by default.
func TestReorderIssues(t *testing.T) {
	const token = "Bearer tth_test"
	var calls []string
	var sorts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = r.ParseForm()
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/move"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/workspaces/acme/issues/"), "/move")
			calls = append(calls, id+" after="+r.PostForm.Get("after")+" before="+r.PostForm.Get("before"))
			_, _ = fmt.Fprintf(w, `{"identifier":%q,"title":"x","status":{"id":"s1","name":"Todo"}}`, id)
		case r.URL.Path == "/api/workspaces/acme/projects/ENG/issues":
			sorts = append(sorts, r.URL.Query().Get("sort"))
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cs := connect(t, srv, token)

	text, isErr := call(t, cs, "reorder_issues", map[string]any{"workspace": "acme", "project": "ENG",
		"identifiers": []string{"ENG-3", "ENG-1", "7"}})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	if want := []string{"ENG-1 after=ENG-3 before=", "ENG-7 after=ENG-1 before="}; !slices.Equal(calls, want) {
		t.Errorf("move calls %q, want %q", calls, want)
	}
	var out []issueOut
	if err := json.Unmarshal([]byte(text), &out); err != nil || len(out) != 2 || out[0].Identifier != "ENG-1" || out[1].Identifier != "ENG-7" {
		t.Errorf("result %s: %v", text, err)
	}

	calls = nil
	if text, isErr := call(t, cs, "reorder_issues", map[string]any{"workspace": "acme", "project": "ENG",
		"identifier": "ENG-2", "before": "ENG-3"}); isErr {
		t.Fatalf("tool error: %s", text)
	}
	if want := []string{"ENG-2 after= before=ENG-3"}; !slices.Equal(calls, want) {
		t.Errorf("move calls %q, want %q", calls, want)
	}
	if text, isErr := call(t, cs, "reorder_issues", map[string]any{"workspace": "acme", "project": "ENG", "identifiers": []string{"ENG-2"}}); !isErr {
		t.Errorf("one identifier: %s, want an error", text)
	}

	call(t, cs, "list_issues", map[string]any{"workspace": "acme", "project": "ENG"})
	call(t, cs, "list_issues", map[string]any{"workspace": "acme", "project": "ENG", "sort": "Priority"})
	if want := []string{"manual", "priority"}; !slices.Equal(sorts, want) {
		t.Errorf("list sorts %q, want %q", sorts, want)
	}
}
