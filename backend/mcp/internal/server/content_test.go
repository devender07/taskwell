package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
)

// contentAPI answers each "METHOD /path" with its JSON and records the form posted to it.
func contentAPI(t *testing.T, answers map[string]string) (*httptest.Server, map[string]url.Values) {
	t.Helper()
	got := map[string]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		body, ok := answers[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		got[key] = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func checkContentForm(t *testing.T, got, want url.Values) {
	t.Helper()
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			t.Errorf("form %s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("form has %d fields, want %d: %v", len(got), len(want), got)
	}
}

func TestCreateNote(t *testing.T) {
	const post = "POST /api/workspaces/acme/notes"
	srv, got := contentAPI(t, map[string]string{post: `{"id":"n1","title":"Groceries",
		"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Milk"}]}]},
		"color":"#abcdef","owner":{"id":"u1","name":"Ada"},"members":[],"pinned":true,"labels":["home"],"can_edit":true}`})
	cs := connect(t, srv, "Bearer tth_test")

	text, isErr := call(t, cs, "create_note", map[string]any{"workspace": "acme", "title": " Groceries ", "body": "Milk\nEggs\n\nBread",
		"color": "#abcdef", "pinned": true, "labels": []string{"home"}})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	checkContentForm(t, got[post], url.Values{"title": {"Groceries"}, "color": {"#abcdef"}, "pinned": {"1"}, "labels": {`["home"]`},
		"body": {`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Milk"},{"type":"hardBreak"},` +
			`{"type":"text","text":"Eggs"}]},{"type":"paragraph","content":[{"type":"text","text":"Bread"}]}]}`}})
	var out noteOut
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.Text != "Milk" || out.Owner != "Ada" ||
		out.URL != "https://app.test/acme/notes?note=n1" {
		t.Errorf("result %s: %v", text, err)
	}
}

func TestCreateView(t *testing.T) {
	const post = "POST /api/workspaces/acme/views"
	srv, got := contentAPI(t, map[string]string{post: `{"id":"v1","name":"Board","path":"/projects/ENG?view=board","shared":true,
		"owner":{"id":"u1","name":"Ada"},"can_edit":true}`})
	cs := connect(t, srv, "Bearer tth_test")

	// A full app link is saved as the path inside the workspace.
	text, isErr := call(t, cs, "create_view", map[string]any{"workspace": "acme", "name": "Board",
		"path": "https://app.test/acme/projects/ENG?view=board", "shared": true})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	checkContentForm(t, got[post], url.Values{"name": {"Board"}, "path": {"/projects/ENG?view=board"}, "shared": {"1"}})
	var out viewOut
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.URL != "https://app.test/acme/projects/ENG?view=board" {
		t.Errorf("result %s: %v", text, err)
	}
}

func TestUpdateFeedback(t *testing.T) {
	const post = "POST /api/workspaces/acme/feedback/f1"
	srv, got := contentAPI(t, map[string]string{post: `{"id":"f1","body":"Export is slow","email":"c@example.com","name":null,
		"source":"form","issue":{"identifier":"ENG-7","title":"Speed up export"},"archived":true,"created_by":null}`})
	cs := connect(t, srv, "Bearer tth_test")

	text, isErr := call(t, cs, "update_feedback", map[string]any{"workspace": "acme", "id": "f1", "issue": " ENG-7 ", "archived": true})
	if isErr {
		t.Fatalf("tool error: %s", text)
	}
	checkContentForm(t, got[post], url.Values{"issue": {"ENG-7"}, "archived": {"true"}})
	var out feedbackOut
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.Issue != "ENG-7 Speed up export" || !out.Archived {
		t.Errorf("result %s: %v", text, err)
	}

	if text, isErr := call(t, cs, "update_feedback", map[string]any{"workspace": "acme", "id": "f1"}); !isErr {
		t.Errorf("no fields: want an error, got %s", text)
	}
}
