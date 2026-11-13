package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// planningAPI is a fake API that records the last write's method, path and form.
func planningAPI(t *testing.T) (*httptest.Server, *string, *url.Values) {
	t.Helper()
	var hit string
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			_ = r.ParseForm()
			hit, form = r.Method+" "+r.URL.Path, r.PostForm
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/workspaces/acme/members":
			_, _ = w.Write([]byte(`{"members":[{"id":"u1","name":"Ada","email":"ada@example.com"},{"id":"u2","name":"Bob","email":"bob@example.com"}]}`))
		case "POST /api/workspaces/acme/projects/ENG/updates":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"up1","health":"at_risk","author":{"id":"u1","name":"Ada"},"created_at":"2026-11-07T10:00:00Z",
				"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Slipping a week"}]}]},"can_edit":true}`))
		case "POST /api/workspaces/acme/projects/ENG/members", "DELETE /api/workspaces/acme/projects/ENG/members/u2":
			_, _ = w.Write([]byte(`{"key":"ENG","name":"Engineering","private":true,"members":[{"id":"u1","name":"Ada"}]}`))
		case "POST /api/workspaces/acme/trash/ENG-9/restore":
			_, _ = w.Write([]byte(`{"identifier":"ENG-9","title":"Fix login","status":{"id":"s1","name":"Backlog"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hit, &form
}

func TestPostProjectUpdate(t *testing.T) {
	srv, hit, form := planningAPI(t)
	cs := connect(t, srv, "Bearer tth_test")
	text, isErr := call(t, cs, "post_project_update", map[string]any{"workspace": "acme", "project": "ENG",
		"health": " at_risk ", "body": "Slipping a week"})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	want := url.Values{"health": {"at_risk"},
		"body": {`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Slipping a week"}]}]}`}}
	if *hit != "POST /api/workspaces/acme/projects/ENG/updates" || len(*form) != len(want) ||
		!slices.Equal((*form)["health"], want["health"]) || !slices.Equal((*form)["body"], want["body"]) {
		t.Errorf("%s form %v, want %v", *hit, *form, want)
	}
	if !strings.Contains(text, `"text":"Slipping a week"`) || !strings.Contains(text, `"author":"Ada"`) {
		t.Errorf("result %s", text)
	}
}

func TestProjectMembersByEmail(t *testing.T) {
	srv, hit, form := planningAPI(t)
	cs := connect(t, srv, "Bearer tth_test")
	if text, isErr := call(t, cs, "add_project_members", map[string]any{"workspace": "acme", "project": "ENG",
		"members": []string{"BOB@example.com", "ada"}}); isErr {
		t.Fatalf("tool error: %s", text)
	}
	if got := (*form)["user_ids"]; *hit != "POST /api/workspaces/acme/projects/ENG/members" || !slices.Equal(got, []string{"u2", "u1"}) {
		t.Errorf("%s user_ids %q, want [u2 u1]", *hit, got)
	}
	if text, isErr := call(t, cs, "remove_project_member", map[string]any{"workspace": "acme", "project": "ENG",
		"member": "bob@example.com"}); isErr || *hit != "DELETE /api/workspaces/acme/projects/ENG/members/u2" {
		t.Errorf("remove: %s %s", *hit, text)
	}
	if text, isErr := call(t, cs, "remove_project_member", map[string]any{"workspace": "acme", "project": "ENG",
		"member": "eve@example.com"}); !isErr || !strings.Contains(text, "Ada <ada@example.com>") {
		t.Errorf("unknown member: error=%v %q", isErr, text)
	}
}

func TestRestoreIssue(t *testing.T) {
	srv, hit, _ := planningAPI(t)
	cs := connect(t, srv, "Bearer tth_test")
	text, isErr := call(t, cs, "restore_issue", map[string]any{"workspace": "acme", "identifier": "ENG-9"})
	if isErr || *hit != "POST /api/workspaces/acme/trash/ENG-9/restore" {
		t.Fatalf("%s: %s", *hit, text)
	}
	if !strings.Contains(text, `"url":"https://app.test/acme/issue/ENG-9"`) {
		t.Errorf("result %s", text)
	}
}
