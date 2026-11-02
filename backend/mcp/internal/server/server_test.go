package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

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
	defer cs.Close()

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
