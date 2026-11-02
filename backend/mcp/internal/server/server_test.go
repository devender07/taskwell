package server

import (
	"context"
	"encoding/json"
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
