package note

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const checklist = `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":false},"content":[{"type":"paragraph","content":[{"type":"text","text":"Milk"}]}]}]}]}`

// TestNotes: Ann writes notes and shares one with Mia; each member's pin, archive and labels stay theirs, the owner
// deletes for everyone, others leave; guests and outsiders get nothing.
func TestNotes(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Bob")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, bob, "member")
	gus, out := dbtest.User(t, pool, "Gus"), dbtest.User(t, pool, "Out")
	dbtest.Join(t, pool, wsID, gus, "guest")

	mux := http.NewServeMux()
	wsH := workspace.New(pool, dbtest.Auth, nil, nil)
	wsH.Register(mux)
	New(pool, dbtest.Auth, wsH.Member).Register(mux)
	base := "/api/workspaces/" + slug + "/notes"
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}
	note := func(user, method, path string, form url.Values, want int) Note {
		t.Helper()
		var n Note
		if err := json.Unmarshal([]byte(do(user, method, path, form, want)), &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	list := func(user string) []Note {
		t.Helper()
		var ns []Note
		if err := json.Unmarshal([]byte(do(user, "GET", "", nil, 200)), &ns); err != nil {
			t.Fatal(err)
		}
		return ns
	}

	// Validation.
	for _, c := range []struct {
		form url.Values
		want string
	}{
		{url.Values{"title": {"  "}}, "Write something first."},
		{url.Values{"title": {"x"}, "body": {`{"type":"doc","content":[{"type":"taskList","content":[{"type":"paragraph"}]}]}`}}, "Note has unsupported content."},
		{url.Values{"title": {strings.Repeat("é", 201)}}, "Use 200 characters or fewer."},
		{url.Values{"title": {"x"}, "color": {"red"}}, "Choose a color from the list."},
		{url.Values{"title": {"x"}, "labels": {`["` + strings.Repeat("a", 31) + `"]`}}, errLabels},
		{url.Values{"title": {"x"}, "labels": {`[" "]`}}, errLabels},
		{url.Values{"title": {"x"}, "labels": {`not json`}}, errLabels},
	} {
		if body := do(ann, "POST", "", c.form, 400); !strings.Contains(body, c.want) {
			t.Errorf("%v: %s, want %q", c.form, body, c.want)
		}
	}
	many := make([]string, maxLabels+1)
	for i := range many {
		many[i] = "l" + strconv.Itoa(i)
	}
	tooMany, _ := json.Marshal(many)
	do(ann, "POST", "", url.Values{"title": {"x"}, "labels": {string(tooMany)}}, 400)

	// Create: title only, and a checklist body.
	plain := note(ann, "POST", "", url.Values{"title": {"  Groceries "}, "color": {"#ABCDEF"}, "labels": {`["Home", "home", " Errands "]`}}, 201)
	if plain.Title != "Groceries" || plain.Color != "#abcdef" || string(plain.Body) != "null" || !plain.CanDelete || plain.Owner.ID != ann ||
		len(plain.Members) != 0 || strings.Join(plain.Labels, ",") != "Home,Errands" || plain.UpdatedBy == nil || plain.UpdatedBy.ID != ann {
		t.Errorf("created %+v", plain)
	}
	shared := note(ann, "POST", "", url.Values{"body": {checklist}, "pinned": {"1"}}, 201)
	if !shared.Pinned || !strings.Contains(string(shared.Body), `"checked":false`) || shared.Title != "" {
		t.Errorf("checklist note %+v", shared)
	}
	if ns := list(ann); len(ns) != 2 || ns[0].ID != shared.ID {
		t.Errorf("ann's notes, pinned first: %+v", ns)
	}

	// Others don't see it: a member gets 404, a guest 403, an outsider the workspace's 404.
	if ns := list(mia); len(ns) != 0 {
		t.Errorf("mia sees %+v", ns)
	}
	do(mia, "POST", "/"+shared.ID, url.Values{"title": {"x"}}, 404)
	do(mia, "DELETE", "/"+shared.ID, nil, 404)
	do(mia, "POST", "/not-a-uuid", url.Values{"title": {"x"}}, 404)
	if body := do(gus, "GET", "", nil, 403); !strings.Contains(body, errGuest) {
		t.Errorf("guest: %s", body)
	}
	do(out, "GET", "", nil, 404)

	// Sharing: a non-guest member, once each.
	for _, u := range []string{gus, out, ann, "nope"} {
		if body := do(ann, "POST", "/"+shared.ID+"/members", url.Values{"user_id": {u}}, 400); !strings.Contains(body, errChoose) {
			t.Errorf("share with %s: %s", u, body)
		}
	}
	if n := note(ann, "POST", "/"+shared.ID+"/members", url.Values{"user_id": {mia}}, 200); len(n.Members) != 1 || n.Members[0].ID != mia {
		t.Errorf("shared %+v", n.Members)
	}
	do(ann, "POST", "/"+shared.ID+"/members", url.Values{"user_id": {mia}}, 400)

	// Mia sees it, not pinned for her; she ticks the item and pins, archives and labels it for herself.
	ns := list(mia)
	if len(ns) != 1 || ns[0].Pinned || ns[0].CanDelete || ns[0].Owner.ID != ann || len(ns[0].Members) != 1 {
		t.Fatalf("mia's notes %+v", ns)
	}
	ticked := strings.Replace(checklist, `"checked":false`, `"checked":true`, 1)
	n := note(mia, "POST", "/"+shared.ID, url.Values{"body": {ticked}, "pinned": {"1"}, "labels": {`["Mine"]`}}, 200)
	if !n.Pinned || n.UpdatedBy == nil || n.UpdatedBy.ID != mia || n.Labels[0] != "Mine" {
		t.Errorf("mia's update %+v", n)
	}
	if n := note(mia, "POST", "/"+shared.ID, url.Values{"archived": {"1"}}, 200); !n.Archived || n.Pinned {
		t.Errorf("archive unpins: %+v", n)
	}
	a := list(ann)[0]
	if a.ID != shared.ID || !a.Pinned || a.Archived || len(a.Labels) != 0 || !strings.Contains(string(a.Body), `"checked":true`) {
		t.Errorf("ann's view of the shared note %+v", a)
	}
	// Per-caller fields don't stamp the note.
	before := a.UpdatedAt
	note(mia, "POST", "/"+shared.ID, url.Values{"archived": {"0"}}, 200)
	if list(ann)[0].UpdatedAt != before {
		t.Error("archive changed updated_at")
	}
	// An empty body clears it.
	if n := note(ann, "POST", "/"+plain.ID, url.Values{"body": {""}, "color": {""}}, 200); string(n.Body) != "null" || n.Color != "" || n.Title != "Groceries" {
		t.Errorf("cleared %+v", n)
	}

	// Removing: never the owner; Mia can't remove Bob, but can leave; Ann can remove anyone.
	note(ann, "POST", "/"+shared.ID+"/members", url.Values{"user_id": {bob}}, 200)
	if body := do(mia, "DELETE", "/"+shared.ID+"/members/"+ann, nil, 400); !strings.Contains(body, "The owner can't be removed.") {
		t.Errorf("remove owner: %s", body)
	}
	do(mia, "DELETE", "/"+shared.ID+"/members/"+bob, nil, 403)
	do(ann, "DELETE", "/"+shared.ID+"/members/"+bob, nil, 204)
	do(bob, "POST", "/"+shared.ID, url.Values{"title": {"x"}}, 404)
	do(mia, "DELETE", "/"+shared.ID+"/members/"+mia, nil, 204)
	if ns := list(mia); len(ns) != 0 {
		t.Errorf("mia after leaving: %+v", ns)
	}

	// Delete: a member leaves, the owner deletes for everyone.
	note(ann, "POST", "/"+shared.ID+"/members", url.Values{"user_id": {mia}}, 200)
	do(mia, "DELETE", "/"+shared.ID, nil, 204)
	if ns := list(ann); len(ns) != 2 || len(ns[0].Members)+len(ns[1].Members) != 0 {
		t.Errorf("ann after mia left: %+v", ns)
	}
	note(ann, "POST", "/"+shared.ID+"/members", url.Values{"user_id": {mia}}, 200)
	do(ann, "DELETE", "/"+shared.ID, nil, 204)
	if len(list(mia)) != 0 || len(list(ann)) != 1 {
		t.Error("owner delete left the note")
	}

	// The cap: 50 people besides the owner.
	var ids []string
	for i := range maxMembers {
		u := dbtest.User(t, pool, "U"+strconv.Itoa(i))
		dbtest.Join(t, pool, wsID, u, "member")
		ids = append(ids, u)
	}
	for _, u := range ids {
		do(ann, "POST", "/"+plain.ID+"/members", url.Values{"user_id": {u}}, 200)
	}
	if body := do(ann, "POST", "/"+plain.ID+"/members", url.Values{"user_id": {mia}}, 400); !strings.Contains(body, "A note can be shared with up to 50 people.") {
		t.Errorf("cap: %s", body)
	}

	// Leaving the workspace takes Mia's own notes and her place on Ann's.
	do(ann, "DELETE", "/"+plain.ID+"/members/"+ids[0], nil, 204)
	note(ann, "POST", "/"+plain.ID+"/members", url.Values{"user_id": {mia}}, 200)
	mine := note(mia, "POST", "", url.Values{"title": {"Mia's"}}, 201)
	if code, body := dbtest.Do(t, mux, ann, "DELETE", "/api/workspaces/"+slug+"/members/"+mia, nil); code != 204 {
		t.Fatalf("remove member: %d %s", code, body)
	}
	var left int
	if err := pool.QueryRow(ctx, `select (select count(*) from notes where id = $1) + (select count(*) from note_members where user_id = $2)`,
		mine.ID, mia).Scan(&left); err != nil || left != 0 {
		t.Errorf("mia's notes after leaving: %d %v", left, err)
	}
}

// TestMigrateSelfChats runs 019_notes.sql's move on a seeded Notes chat (in a rolled-back transaction): its live
// top-level messages become the owner's notes, replies and deleted ones don't, and the chat goes.
func TestMigrateSelfChats(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	sql, err := os.ReadFile("../db/migrations/019_notes.sql")
	if err != nil {
		t.Fatal(err)
	}
	_, move, ok := strings.Cut(string(sql), "with moved as")
	if !ok {
		t.Fatal("019_notes.sql has no move")
	}
	ann := dbtest.User(t, pool, "Ann")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var wsID, chatID, topID string
	if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `select id::text from workspaces where created_by = $1`, ann).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `insert into chats (workspace_id, kind, pair, created_by) values ($1, 'self', $2::text, $2::uuid) returning id::text`,
		wsID, ann).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Buy milk"}]}]}`
	if err := tx.QueryRow(ctx, `insert into messages (chat_id, author_id, body, created_at) values ($1, $2, $3, '2025-01-02') returning id::text`,
		chatID, ann, body).Scan(&topID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into messages (chat_id, author_id, body, parent_id) values ($1, $2, $3, $4);
		`, chatID, ann, body, topID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into messages (chat_id, author_id, body, deleted_at) values ($1, $2, $3, now())`, chatID, ann, body); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "with moved as"+move); err != nil {
		t.Fatal(err)
	}
	var n, members, chats int
	var text, created string
	if err := tx.QueryRow(ctx, `select count(*), max(n.body->'content'->0->'content'->0->>'text'), max(n.created_at::date::text),
		  (select count(*) from note_members m join notes x on x.id = m.note_id where x.workspace_id = $1 and m.user_id = $2),
		  (select count(*) from chats where id = $3)
		from notes n where n.workspace_id = $1 and n.owner_id = $2 and n.title = ''`, wsID, ann, chatID).Scan(&n, &text, &created, &members, &chats); err != nil {
		t.Fatal(err)
	}
	if n != 1 || members != 1 || chats != 0 || text != "Buy milk" || created != "2025-01-02" {
		t.Errorf("moved %d notes (%d members, %q, %s), %d chats left", n, members, text, created, chats)
	}
}
