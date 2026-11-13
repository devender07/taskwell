package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestGuestLosesWorkspaceChat: a member who read the workspace chat (so has a chat_members row there) loses it on
// becoming a guest, while a group chat they're in keeps working.
func TestGuestLosesWorkspaceChat(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s as %s: %d %s, want %d", method, path, user, code, body, want)
		}
		return body
	}
	var cs []Chat
	if err := json.Unmarshal([]byte(do(bob, "GET", "/chats", nil, 200)), &cs); err != nil || len(cs) != 1 {
		t.Fatalf("bob's chats: %v %v", cs, err)
	}
	general := cs[0].ID
	do(ann, "POST", "/chats/"+general+"/messages", url.Values{"body": {text("plans")}}, 201)
	do(bob, "POST", "/chats/"+general+"/read", nil, 204)
	var dm Chat
	if err := json.Unmarshal([]byte(do(ann, "POST", "/chats", url.Values{"name": {"Design"}, "member_ids": {bob}}, 201)), &dm); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `update workspace_members set role_id = (select id from workspace_roles where workspace_id = $1 and builtin = 'guest')
		where workspace_id = $1 and user_id = $2`, wsID, bob); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(do(bob, "GET", "/chats", nil, 200)), &cs); err != nil || len(cs) != 1 || cs[0].ID != dm.ID {
		t.Fatalf("guest bob's chats: %+v %v, want only the group chat", cs, err)
	}
	do(bob, "GET", "/chats/"+general+"/messages", nil, 404)
	do(bob, "POST", "/chats/"+general+"/messages", url.Values{"body": {text("hi")}}, 404)
	do(bob, "GET", "/chats/"+dm.ID+"/messages", nil, 200)
}
