package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/secret"
)

// TestMyInvites: the invite prompt's list, accept and dismiss act only on the caller's own unexpired invites;
// dismissing hides the invite and leaves one invite notification however often it's repeated, and accepting joins
// the workspace and deletes that notification.
func TestMyInvites(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cara := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cara")
	m := &model{db: pool}
	mk := func(user, name string) Workspace {
		t.Helper()
		ws, err := m.create(ctx, user, name, dbtest.Name("my-invites-"))
		if err != nil {
			t.Fatal(err)
		}
		return ws
	}
	team, old, bobs, caras := mk(ann, "Team"), mk(ann, "Old"), mk(bob, "Bob's"), mk(cara, "Cara's")
	var email string
	if err := pool.QueryRow(ctx, `select email from users where id = $1`, bob).Scan(&email); err != nil {
		t.Fatal(err)
	}
	invite := func(ws *Workspace) string {
		t.Helper()
		role, err := m.role(ctx, ws.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		inv, err := m.invite(ctx, ws, email, ann, role.ID, nil, secret.Hash(secret.NewToken()), 7)
		if err != nil {
			t.Fatal(err)
		}
		return inv.ID
	}
	teamInv, oldInv := invite(&team), invite(&old)
	if _, err := pool.Exec(ctx, `update workspace_invites set expires_at = now() - interval '1 minute' where id = $1`, oldInv); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, nil, nil).Register(mux)
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, "/api/me/invites"+path, form)
		if code != want {
			t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
		}
		return body
	}
	list := func(user string) []PendingInvite {
		t.Helper()
		var invs []PendingInvite
		if err := json.Unmarshal([]byte(do(user, "GET", "", nil, 200)), &invs); err != nil {
			t.Fatal(err)
		}
		return invs
	}
	notes := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `select count(*) from notifications where user_id = $1 and kind = 'invite'`, bob).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if invs := list(bob); len(invs) != 1 || invs[0].ID != teamInv || invs[0].Workspace.Name != "Team" || invs[0].Role.Name != "Member" {
		t.Fatalf("Bob's invites %+v, want only Team's (Old's expired)", invs)
	}
	if invs := list(cara); len(invs) != 0 {
		t.Fatalf("Cara's invites %+v, want none", invs)
	}
	at := func(ws Workspace) url.Values { return url.Values{"workspace": {ws.Slug}} }
	do(cara, "POST", "/"+teamInv+"/accept", nil, 404)
	do(cara, "POST", "/"+teamInv+"/dismiss", at(caras), 404)
	do(bob, "POST", "/"+oldInv+"/accept", nil, 404)
	do(bob, "POST", "/"+oldInv+"/dismiss", at(bobs), 404)
	do(bob, "POST", "/not-a-uuid/dismiss", at(bobs), 404)
	do(bob, "POST", "/"+teamInv+"/dismiss", at(team), 404) // not a member of Team yet

	do(bob, "POST", "/"+teamInv+"/dismiss", at(bobs), 204)
	do(bob, "POST", "/"+teamInv+"/dismiss", at(bobs), 204)
	if n := notes(); n != 1 {
		t.Fatalf("invite notifications after two dismissals: %d, want 1", n)
	}
	if invs := list(bob); len(invs) != 0 {
		t.Fatalf("Bob's invites after dismissing: %+v, want none", invs)
	}

	var ws Workspace
	if err := json.Unmarshal([]byte(do(bob, "POST", "/"+teamInv+"/accept", nil, 200)), &ws); err != nil || ws.ID != team.ID || ws.Role.Name != "Member" {
		t.Fatalf("accepted %+v %v", ws, err)
	}
	if n := notes(); n != 0 {
		t.Fatalf("invite notifications after accepting: %d, want 0", n)
	}
	do(bob, "POST", "/"+teamInv+"/accept", nil, 404)
}
