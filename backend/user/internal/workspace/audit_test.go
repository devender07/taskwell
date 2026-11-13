package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/wsaudit"
)

// TestAudit: inviting, creating, updating and deleting a role, changing a member's role and removing a member each
// write their entry (with the actor's copies and the client IP), and the list pages 50 at a time, filters by category
// and is only for audit.view.
func TestAudit(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cara := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cara")
	m := &model{db: pool}
	ws, err := m.create(ctx, ann, "Audit", dbtest.Name("audit-"))
	if err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, ws.ID, bob, "member")
	dbtest.Join(t, pool, ws.ID, cara, "member")
	var bobEmail, caraEmail string
	if err := pool.QueryRow(ctx, `select (select email from users where id = $1), (select email from users where id = $2)`,
		bob, cara).Scan(&bobEmail, &caraEmail); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, nil, nil).Register(mux)
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, "/api/workspaces/"+ws.Slug+path, form)
		if code != want {
			t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
		}
		return body
	}
	type page struct {
		Entries []wsaudit.Entry `json:"entries"`
		More    bool            `json:"more"`
	}
	list := func(query string) page {
		t.Helper()
		var p page
		if err := json.Unmarshal([]byte(do(ann, "GET", "/audit"+query, nil, 200)), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	detail := func(e wsaudit.Entry) string { return string(e.Detail) }

	member, err := m.role(ctx, ws.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.invite(ctx, &ws, "new@dbtest.test", ann, member.ID, nil, secret.Hash(secret.NewToken()), 7); err != nil {
		t.Fatal(err)
	}
	var role Role
	if err := json.Unmarshal([]byte(do(ann, "POST", "/roles", url.Values{"name": {"Triage"}, "permissions": {"issues.create"}}, 201)), &role); err != nil {
		t.Fatal(err)
	}
	do(ann, "POST", "/roles/"+role.ID, url.Values{"name": {"Triagers"}, "permissions": {"comments.create"}}, 200)
	do(ann, "POST", "/members/"+bob, url.Values{"role_id": {role.ID}}, 200)
	do(ann, "DELETE", "/roles/"+role.ID, nil, 204)
	do(ann, "DELETE", "/members/"+cara, nil, 204)

	want := []struct{ action, target, detail string }{
		{"member.removed", caraEmail, `{}`},
		{"role.deleted", "Triagers", `{}`},
		{"member.role_changed", bobEmail, `{"to":"Triagers","from":"Member"}`},
		{"role.updated", "Triagers", `{"added":["comments.create"],"removed":["issues.create"],"renamed_from":"Triage"}`},
		{"role.created", "Triage", `{"permissions":["issues.create"]}`},
		{"member.invited", "new@dbtest.test", `{"role":"Member","guest":false}`},
	}
	got := list("").Entries
	if len(got) != len(want) {
		t.Fatalf("entries %+v, want %d", got, len(want))
	}
	for i, w := range want {
		e := got[i]
		if e.Action != w.action || e.Target != w.target || detail(e) != w.detail {
			t.Errorf("entry %d: %s %q %s, want %s %q %s", i, e.Action, e.Target, detail(e), w.action, w.target, w.detail)
		}
		if e.Actor == nil || e.Actor.ID == nil || *e.Actor.ID != ann || e.Actor.Name != "Ann" {
			t.Errorf("entry %d actor %+v, want Ann", i, e.Actor)
		}
	}
	if got[0].IP != "192.0.2.1" || got[5].IP != "" { // httptest's RemoteAddr; the invite went straight to the model
		t.Errorf("ips %q %q", got[0].IP, got[5].IP)
	}

	if p := list("?category=roles"); len(p.Entries) != 3 || p.Entries[0].Action != "role.deleted" || p.More {
		t.Errorf("roles category: %+v", p)
	}
	if p := list("?category=members"); len(p.Entries) != 3 {
		t.Errorf("members category: %+v", p)
	}
	if p := list("?category=nope"); len(p.Entries) != len(want) {
		t.Errorf("unknown category isn't ignored: %d entries", len(p.Entries))
	}
	do(ann, "GET", "/audit?page=0", nil, 400)
	do(bob, "GET", "/audit", nil, 403)
	do(cara, "GET", "/audit", nil, 404) // removed

	for range wsaudit.PageSize {
		if err := wsaudit.Record(ctx, pool, ws.ID, ann, "project.archived", "WEB", nil); err != nil {
			t.Fatal(err)
		}
	}
	first := list("")
	if len(first.Entries) != wsaudit.PageSize || !first.More || first.Entries[0].Action != "project.archived" {
		t.Fatalf("first page: %d entries, more %v", len(first.Entries), first.More)
	}
	rest := list("?page=2")
	if len(rest.Entries) != len(want) || rest.More || rest.Entries[0].Action != "member.removed" {
		t.Fatalf("second page: %+v", rest)
	}

	if err := wsaudit.Record(ctx, pool, ws.ID, bob, "member.left", bobEmail, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `delete from users where id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	if a := list("").Entries[0].Actor; a == nil || a.ID != nil || a.Name != "Bob" || a.Email != bobEmail {
		t.Errorf("deleted actor %+v, want the copies with no id", a)
	}
	if err := wsaudit.Record(ctx, pool, ws.ID, "", "trash.emptied", "", map[string]any{"count": 2}); err != nil {
		t.Fatal(err)
	}
	if e := list("").Entries[0]; e.Actor != nil || detail(e) != `{"count":2}` {
		t.Errorf("entry by nobody: %+v %s", e.Actor, detail(e))
	}
}
