package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// adminAPI is a fake API with a workspace's members, invites and roles; it records each other request's method,
// path and form.
func adminAPI(t *testing.T, got *[]string, form *url.Values) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/workspaces/acme/members":
			_, _ = w.Write([]byte(`{"members":[{"id":"u1","name":"Ada","email":"ada@example.com"},{"id":"u2","name":"Bob","email":"bob@example.com"}],
				"invites":[{"id":"i1","email":"cy@example.com","role":{"id":"r1","name":"Member"}}]}`))
		case "GET /api/workspaces/acme/roles":
			_, _ = w.Write([]byte(`[{"id":"r1","name":"Member","permissions":[]},{"id":"r2","name":"QA lead","permissions":["issues.edit"]}]`))
		default:
			_ = r.ParseForm()
			*got, *form = append(*got, r.Method+" "+r.URL.Path), r.PostForm
			switch r.Method {
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			default:
				_, _ = w.Write([]byte(`{"webhook":{"id":"w1","url":"https://hooks.test/x"},"secret":"whsec_abc"}`))
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAdminTools(t *testing.T) {
	var got []string
	var form url.Values
	cs := connect(t, adminAPI(t, &got, &form), "Bearer tth_test")

	text, isErr := call(t, cs, "change_member_role", map[string]any{"workspace": "acme", "member": "BOB", "role": "qa lead"})
	if isErr || !slices.Equal(got, []string{"POST /api/workspaces/acme/members/u2"}) || form.Get("role_id") != "r2" {
		t.Errorf("change_member_role: %q %v %v", text, got, form)
	}

	got = nil
	text, isErr = call(t, cs, "create_webhook", map[string]any{"workspace": "acme", "url": "https://hooks.test/x",
		"events": []string{"issue.created", "comment.created"}})
	if isErr || !strings.Contains(text, `"secret":"whsec_abc"`) || !slices.Equal(form["events"], []string{"issue.created", "comment.created"}) {
		t.Errorf("create_webhook: %q %v", text, form)
	}

	got = nil
	text, isErr = call(t, cs, "revoke_invite", map[string]any{"workspace": "acme", "email": "Cy@example.com"})
	if isErr || !slices.Equal(got, []string{"DELETE /api/workspaces/acme/invites/i1"}) {
		t.Errorf("revoke_invite: %q %v", text, got)
	}

	// An unknown role answers with the choices and calls nothing.
	got = nil
	text, isErr = call(t, cs, "change_member_role", map[string]any{"workspace": "acme", "member": "ada", "role": "Owner"})
	if !isErr || !strings.Contains(text, "Member, QA lead") || got != nil {
		t.Errorf("unknown role: %v %q %v", isErr, text, got)
	}
}
