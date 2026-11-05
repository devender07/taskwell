package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/notify"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

func text(s string) string {
	return `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + s + `"}]}]}`
}

func mention(id, label, char string) string {
	return `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hi "},` +
		`{"type":"mention","attrs":{"id":"` + id + `","label":"` + label + `","mentionSuggestionChar":"` + char + `"}}]}]}`
}

// TestChat runs the chat API against the scratch database (see dbtest): access per kind, direct and group chats,
// messages, threads, reactions, unread counts, read markers and the notifications chat messages make.
func TestChat(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cat, out := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cat"), dbtest.User(t, pool, "Out")
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
	dbtest.Join(t, pool, wsID, cat, "member")

	mux := http.NewServeMux()
	member := workspace.New(pool, dbtest.Auth, nil, nil).Member
	h := New(pool, dbtest.Auth, member)
	h.Register(mux)
	notify.New(pool, dbtest.Auth, member, nil, nil).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int, into any) {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s as %s: %d %s, want %d", method, path, user, code, body, want)
		}
		if into != nil {
			if err := json.Unmarshal([]byte(body), into); err != nil {
				t.Fatalf("%s %s: %v in %s", method, path, err, body)
			}
		}
	}
	errOf := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s as %s: %d %s, want %d", method, path, user, code, body, want)
		}
		return strings.TrimSpace(body)
	}
	chats := func(user string) []Chat {
		t.Helper()
		var cs []Chat
		do(user, "GET", "/chats", nil, 200, &cs)
		return cs
	}
	find := func(user, id string) Chat {
		t.Helper()
		for _, c := range chats(user) {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("%s doesn't see chat %s", user, id)
		return Chat{}
	}
	unreadCount := func(user string) int {
		t.Helper()
		var n struct{ Count int }
		do(user, "GET", "/chats/unread-count", nil, 200, &n)
		return n.Count
	}
	post := func(user, chatID, body, parent string) Message {
		t.Helper()
		var m Message
		do(user, "POST", "/chats/"+chatID+"/messages", url.Values{"body": {body}, "parent_id": {parent}}, 201, &m)
		return m
	}

	// The workspace chat comes first, created on demand.
	cs := chats(ann)
	if len(cs) != 1 || cs[0].Kind != "workspace" || string(cs[0].Members) != "[]" {
		t.Fatalf("ann's chats: %+v", cs)
	}
	general := cs[0].ID
	if again := chats(ann); len(again) != 1 || again[0].ID != general {
		t.Fatal("listing again made more chats")
	}
	errOf(out, "GET", "/chats", nil, 404)
	if got := errOf(out, "GET", "/chats/"+general, nil, 404); got != `{"error":"Workspace not found."}` {
		t.Errorf("outsider: %s", got)
	}

	// Direct chats: one per pair, whoever starts it.
	var d1, d2 Chat
	do(ann, "POST", "/chats/direct", url.Values{"user_id": {bob}}, 200, &d1)
	do(bob, "POST", "/chats/direct", url.Values{"user_id": {ann}}, 200, &d2)
	if d1.ID != d2.ID || d1.Kind != "direct" || d1.Name != "Bob" || d2.Name != "Ann" || len(chats(bob)) != 2 {
		t.Fatalf("direct: %+v %+v", d1, d2)
	}
	for _, u := range []string{out, ann, "nope"} {
		if got := errOf(ann, "POST", "/chats/direct", url.Values{"user_id": {u}}, 400); !strings.Contains(got, "Choose someone in this workspace.") {
			t.Errorf("direct to %s: %s", u, got)
		}
	}
	errOf(cat, "GET", "/chats/"+d1.ID, nil, 404)

	// Groups: create, rename, add, remove by the creator only, leave; the last one out deletes it.
	for form, want := range map[string]string{
		"member_ids=" + bob: "Name the group.",
		"name=" + strings.Repeat("a", 81) + "&member_ids=" + bob: "Keep the name under 80 characters.",
		"name=G":                   "Add at least one person.",
		"name=G&member_ids=" + out: "Someone you picked isn't in this workspace.",
	} {
		f, _ := url.ParseQuery(form)
		if got := errOf(ann, "POST", "/chats", f, 400); !strings.Contains(got, want) {
			t.Errorf("group %q: %s", form, got)
		}
	}
	var g Chat
	do(ann, "POST", "/chats", url.Values{"name": {" Design "}, "member_ids": {bob, bob, ann}}, 201, &g)
	if g.Name != "Design" || g.Kind != "group" || len(g.Members) == 0 || *g.CreatedBy != ann {
		t.Fatalf("group: %+v", g)
	}
	do(bob, "POST", "/chats/"+g.ID, url.Values{"name": {"Design team"}}, 200, &g)
	if g.Name != "Design team" {
		t.Errorf("rename: %s", g.Name)
	}
	errOf(ann, "POST", "/chats/"+general, url.Values{"name": {"x"}}, 403)
	errOf(ann, "POST", "/chats/"+d1.ID+"/members", url.Values{"user_ids": {cat}}, 403)
	errOf(cat, "POST", "/chats/"+g.ID, url.Values{"name": {"x"}}, 404)
	do(bob, "POST", "/chats/"+g.ID+"/members", url.Values{"user_ids": {cat}}, 200, &g)
	var ms []map[string]string
	_ = json.Unmarshal(g.Members, &ms)
	if len(ms) != 3 {
		t.Fatalf("members after add: %s", g.Members)
	}
	errOf(bob, "DELETE", "/chats/"+g.ID+"/members/"+cat, nil, 403)
	do(ann, "DELETE", "/chats/"+g.ID+"/members/"+cat, nil, 204, nil)
	errOf(cat, "GET", "/chats/"+g.ID, nil, 404)
	do(bob, "DELETE", "/chats/"+g.ID+"/members/"+bob, nil, 204, nil)
	do(ann, "GET", "/chats/"+g.ID, nil, 200, nil)
	do(ann, "DELETE", "/chats/"+g.ID+"/members/"+ann, nil, 204, nil)
	var left int
	if err := pool.QueryRow(ctx, `select count(*) from chats where id = $1`, g.ID).Scan(&left); err != nil || left != 0 {
		t.Errorf("empty group kept: %d %v", left, err)
	}

	// Messages page newest 50 before the cursor, oldest first.
	if _, err := pool.Exec(ctx, `insert into messages (chat_id, author_id, body) select $1, $2, $3 from generate_series(1, 55)`,
		general, cat, text("old")); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Messages []Message
		HasMore  bool `json:"has_more"`
	}
	do(ann, "GET", "/chats/"+general+"/messages", nil, 200, &page)
	if len(page.Messages) != 50 || !page.HasMore || page.Messages[0].ID > page.Messages[49].ID {
		t.Fatalf("first page: %d more=%v", len(page.Messages), page.HasMore)
	}
	do(ann, "GET", "/chats/"+general+"/messages?before="+page.Messages[0].ID, nil, 200, &page)
	if len(page.Messages) != 5 || page.HasMore {
		t.Fatalf("second page: %d more=%v", len(page.Messages), page.HasMore)
	}
	errOf(ann, "GET", "/chats/"+general+"/messages?before=x", nil, 400)
	if _, err := pool.Exec(ctx, `delete from messages where chat_id = $1`, general); err != nil {
		t.Fatal(err)
	}

	// Validation; issue mentions are fine and notify nobody; a mention of someone outside the chat becomes text.
	if got := errOf(ann, "POST", "/chats/"+general+"/messages", url.Values{"body": {""}}, 400); !strings.Contains(got, "Write a message.") {
		t.Errorf("empty: %s", got)
	}
	if got := errOf(ann, "POST", "/chats/"+general+"/messages", url.Values{"body": {text(strings.Repeat("a", 10001))}}, 400); !strings.Contains(got, "Message must be 10,000 characters or fewer.") {
		t.Errorf("long: %s", got)
	}
	post(ann, general, mention("ENG-1", "ENG-1", "#"), "")
	dm := post(ann, d1.ID, mention(cat, "Cat", "@"), "")
	if strings.Contains(string(dm.Body), `"mention"`) || !strings.Contains(string(dm.Body), "@Cat") {
		t.Errorf("mention outside the chat kept: %s", dm.Body)
	}
	countNotes := func(user, kind string) (n int, msgID string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `select count(*), coalesce(max(message_id::text), '') from notifications
			where user_id = $1 and kind = $2 and read_at is null`, user, kind).Scan(&n, &msgID); err != nil {
			t.Fatal(err)
		}
		return n, msgID
	}
	if n, _ := countNotes(cat, "chat_mention"); n != 0 {
		t.Errorf("cat notified from a chat she can't see: %d", n)
	}

	// Direct messages: unread for the other person, one collapsed "message" notification.
	dm2 := post(ann, d1.ID, text("second"), "")
	if c := find(bob, d1.ID); c.Unread != 2 || c.Mentioned || c.LastMessage == nil || c.LastMessage.Excerpt != "second" {
		t.Errorf("bob's direct chat: %+v", c)
	}
	if c := find(ann, d1.ID); c.Unread != 0 {
		t.Errorf("own messages unread: %d", c.Unread)
	}
	if n, id := countNotes(bob, "message"); n != 1 || id != dm2.ID {
		t.Errorf("message notifications: %d %s, want 1 %s", n, id, dm2.ID)
	}

	// A mention in the workspace chat: mentioned, counted, a chat_mention.
	at := post(ann, general, mention(bob, "Bob", "@"), "")
	if c := find(bob, general); c.Unread != 2 || !c.Mentioned {
		t.Errorf("bob's workspace chat: %+v", c)
	}
	if n := unreadCount(bob); n != 3 {
		t.Errorf("bob's unread count: %d, want 3 (2 direct + 1 mention)", n)
	}
	if n, _ := countNotes(bob, "chat_mention"); n != 1 {
		t.Errorf("chat mentions: %d", n)
	}
	var inbox struct{ Notifications []notify.Notification }
	do(bob, "GET", "/notifications", nil, 200, &inbox)
	for _, n := range inbox.Notifications {
		if string(n.Issue) != "null" || !strings.Contains(string(n.Chat), `"kind":`) {
			t.Errorf("chat notification JSON: issue %s chat %s", n.Issue, n.Chat)
		}
	}
	if len(inbox.Notifications) != 2 || !strings.Contains(string(inbox.Notifications[1].Chat), `"name":"Ann"`) {
		t.Errorf("inbox: %+v", inbox.Notifications)
	}

	// Reading moves forward only and reads the notifications up to it.
	do(bob, "POST", "/chats/"+d1.ID+"/read", nil, 204, nil)
	do(bob, "POST", "/chats/"+d1.ID+"/read", url.Values{"message_id": {dm.ID}}, 204, nil)
	if c := find(bob, d1.ID); c.Unread != 0 {
		t.Errorf("after reading: %d unread", c.Unread)
	}
	if n, _ := countNotes(bob, "message"); n != 0 {
		t.Errorf("message notification unread after reading: %d", n)
	}
	do(bob, "POST", "/chats/"+general+"/read", url.Values{"message_id": {at.ID}}, 204, nil)
	if n := unreadCount(bob); n != 0 {
		t.Errorf("unread after reading all: %d", n)
	}
	if n, _ := countNotes(bob, "chat_mention"); n != 0 {
		t.Errorf("chat mention unread after reading: %d", n)
	}

	// Threads: replies count on the root; a reply can't be replied to; replies from other chats are refused.
	root := post(ann, general, text("root"), "")
	reply := post(bob, general, text("reply"), root.ID)
	errOf(ann, "POST", "/chats/"+general+"/messages", url.Values{"body": {text("x")}, "parent_id": {reply.ID}}, 400)
	errOf(ann, "POST", "/chats/"+d1.ID+"/messages", url.Values{"body": {text("x")}, "parent_id": {root.ID}}, 400)
	var thread struct {
		Root    Message
		Replies []Message
	}
	do(cat, "GET", "/messages/"+root.ID+"/replies", nil, 200, &thread)
	if thread.Root.ReplyCount != 1 || len(thread.Replies) != 1 || !strings.Contains(string(thread.Root.Repliers), bob) {
		t.Errorf("thread: %+v", thread)
	}
	errOf(cat, "GET", "/messages/"+reply.ID+"/replies", nil, 404)
	errOf(cat, "GET", "/messages/"+dm.ID+"/replies", nil, 404)
	errOf(out, "GET", "/messages/"+root.ID+"/replies", nil, 404)

	// Pins: anyone in the chat, top-level only, 1/7/30 days, changed only by who pinned; the messages page lists them.
	var m Message
	errOf(bob, "POST", "/messages/"+root.ID+"/pin", url.Values{"days": {"3"}}, 400)
	errOf(bob, "POST", "/messages/"+reply.ID+"/pin", url.Values{"days": {"7"}}, 404)
	errOf(out, "POST", "/messages/"+root.ID+"/pin", url.Values{"days": {"7"}}, 404)
	do(bob, "POST", "/messages/"+root.ID+"/pin", url.Values{"days": {"7"}}, 200, &m)
	if m.PinnedUntil == nil || m.PinnedUntil.Sub(m.CreatedAt).Hours() < 24*7-1 {
		t.Errorf("pinned until: %v", m.PinnedUntil)
	}
	var pins struct{ Pinned []Message }
	do(cat, "GET", "/chats/"+general+"/messages", nil, 200, &pins)
	if len(pins.Pinned) != 1 || pins.Pinned[0].ID != root.ID {
		t.Errorf("pinned: %+v", pins.Pinned)
	}
	if !strings.Contains(string(m.PinnedBy), bob) {
		t.Errorf("pinned by: %s", m.PinnedBy)
	}
	errOf(ann, "DELETE", "/messages/"+root.ID+"/pin", nil, 403)
	errOf(ann, "POST", "/messages/"+root.ID+"/pin", url.Values{"days": {"1"}}, 403)
	do(bob, "DELETE", "/messages/"+root.ID+"/pin", nil, 200, &m)
	if m.PinnedUntil != nil || string(m.PinnedBy) != "null" {
		t.Errorf("unpinned: %v", m.PinnedUntil)
	}
	do(bob, "POST", "/messages/"+root.ID+"/pin", url.Values{"days": {"1"}}, 200, nil)

	// Reactions: twice is once; counts, mine and people.
	do(bob, "POST", "/messages/"+root.ID+"/reactions", url.Values{"emoji": {"👍"}}, 200, nil)
	do(bob, "POST", "/messages/"+root.ID+"/reactions", url.Values{"emoji": {"👍"}}, 200, nil)
	do(ann, "POST", "/messages/"+root.ID+"/reactions", url.Values{"emoji": {"👍"}}, 200, &m)
	if got := string(m.Reactions); got != `[{"emoji":"👍","count":2,"mine":true,"people":["Bob","You"]}]` {
		t.Errorf("reactions: %s", got)
	}
	errOf(ann, "POST", "/messages/"+root.ID+"/reactions", url.Values{"emoji": {" "}}, 400)
	do(ann, "DELETE", "/messages/"+root.ID+"/reactions?emoji="+url.QueryEscape("👍"), nil, 200, &m)
	if got := string(m.Reactions); got != `[{"emoji":"👍","count":1,"mine":false,"people":["Bob"]}]` {
		t.Errorf("after removing: %s", got)
	}

	// Edit and delete your own only; a deleted root keeps its replies.
	errOf(bob, "POST", "/messages/"+root.ID, url.Values{"body": {text("x")}}, 403)
	do(ann, "POST", "/messages/"+root.ID, url.Values{"body": {mention(cat, "Cat", "@")}}, 200, &m)
	if m.EditedAt == nil || !m.CanEdit {
		t.Errorf("edited: %+v", m)
	}
	if n, _ := countNotes(cat, "chat_mention"); n != 1 {
		t.Errorf("edit adding a mention: %d notifications", n)
	}
	errOf(bob, "DELETE", "/messages/"+root.ID, nil, 403)
	do(ann, "DELETE", "/messages/"+root.ID, nil, 204, nil)
	do(cat, "GET", "/messages/"+root.ID+"/replies", nil, 200, &thread)
	if !thread.Root.Deleted || string(thread.Root.Body) != "null" || thread.Root.CanEdit || len(thread.Replies) != 1 ||
		string(thread.Root.Reactions) != "[]" {
		t.Errorf("deleted root: %+v", thread.Root)
	}
	if n, _ := countNotes(cat, "chat_mention"); n != 0 {
		t.Errorf("deleted message's notification kept: %d", n)
	}
	errOf(ann, "DELETE", "/messages/"+root.ID, nil, 404)

	// Typing audiences: the workspace for the workspace chat, members elsewhere, nothing for outsiders.
	if aud, err := h.Audience(ctx, bob, general); err != nil || len(aud) != 1 || aud[0] != "ws:"+wsID {
		t.Errorf("workspace audience: %v %v", aud, err)
	}
	if aud, err := h.Audience(ctx, bob, d1.ID); err != nil || len(aud) != 2 {
		t.Errorf("direct audience: %v %v", aud, err)
	}
	for _, u := range []string{cat, out} {
		if aud, err := h.Audience(ctx, u, d1.ID); err != nil || aud != nil {
			t.Errorf("audience for %s: %v %v", u, aud, err)
		}
	}

	// Deleting a whole chat: the workspace chat never, a group only by its creator, a direct chat by either person.
	errOf(ann, "DELETE", "/chats/"+general, nil, 403)
	var dg, dd Chat
	do(ann, "POST", "/chats", url.Values{"name": {"Temp"}, "member_ids": {cat}}, 201, &dg)
	post(ann, dg.ID, text("bye"), "")
	if got := errOf(cat, "DELETE", "/chats/"+dg.ID, nil, 403); !strings.Contains(got, "Only the group's creator can delete it.") {
		t.Errorf("non-creator delete: %s", got)
	}
	do(ann, "DELETE", "/chats/"+dg.ID, nil, 204, nil)
	errOf(cat, "GET", "/chats/"+dg.ID, nil, 404)
	do(cat, "POST", "/chats/direct", url.Values{"user_id": {ann}}, 200, &dd)
	post(ann, dd.ID, text("hi"), "")
	do(cat, "DELETE", "/chats/"+dd.ID, nil, 204, nil)
	errOf(ann, "GET", "/chats/"+dd.ID, nil, 404)

	// Leaving the workspace ends access everywhere.
	if _, err := pool.Exec(ctx, `delete from workspace_members where workspace_id = $1 and user_id = $2`, wsID, bob); err != nil {
		t.Fatal(err)
	}
	errOf(bob, "GET", "/chats/"+d1.ID, nil, 404)
	if aud, err := h.Audience(ctx, bob, d1.ID); err != nil || aud != nil {
		t.Errorf("removed member's audience: %v %v", aud, err)
	}
}
