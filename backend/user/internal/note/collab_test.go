package note

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/collab"
	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// Frame types from collab.go.
const (
	frameReady  = 0
	frameUpdate = 1
	frameSave   = 4
)

type peer struct {
	mu     sync.Mutex
	frames [][]byte
}

func (p *peer) Send(f []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.frames = append(p.frames, f)
	return true
}
func (p *peer) Write(_ context.Context, f []byte) error { p.Send(f); return nil }
func (p *peer) Close()                                  {}

// ready returns the flags of the last ready frame sent.
func (p *peer) ready(t *testing.T) byte {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.frames) - 1; i >= 0; i-- {
		if p.frames[i][0] == frameReady {
			return p.frames[i][17]
		}
	}
	t.Fatal("no ready frame")
	return 0
}

func frame(typ byte, id string, payload string) []byte {
	b, _ := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return append(append([]byte{typ}, b...), payload...)
}

// TestNoteRoles: Ann shares a note with Mia to view and Bob to edit. Mia can't change the shared fields, add people
// or edit live, but keeps her own pin; only Ann changes roles. A live save frame stores the body, and a REST body
// write resets the live document.
func TestNoteRoles(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, bob, dan := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Dan")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{mia, bob, dan} {
		dbtest.Join(t, pool, wsID, u, "member")
	}
	mux := http.NewServeMux()
	wsH := workspace.New(pool, dbtest.Auth, nil, nil)
	wsH.Register(mux)
	h := New(pool, dbtest.Auth, wsH.Member)
	h.Register(mux)
	base := "/api/workspaces/" + slug + "/notes"
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
		}
		return body
	}

	n := do(ann, "POST", "", url.Values{"title": {"Plan"}, "body": {checklist}}, 201)
	id := n[strings.Index(n, `"id":"`)+6:][:36]
	if body := do(ann, "POST", "/"+id+"/members", url.Values{"user_id": {mia}, "role": {"owner"}}, 400); !strings.Contains(body, errRole) {
		t.Errorf("bad role: %s", body)
	}
	do(ann, "POST", "/"+id+"/members", url.Values{"user_id": {mia}, "role": {"view"}}, 200)
	if body := do(ann, "POST", "/"+id+"/members", url.Values{"user_id": {bob}}, 200); !strings.Contains(body, `"id":"`+bob+`","name":"Bob","role":"edit"`) ||
		!strings.Contains(body, `"role":"view"`) || !strings.Contains(body, `"can_edit":true`) {
		t.Errorf("members: %s", body)
	}

	// Mia views: 403 on the shared fields and on adding people, but her pin is hers.
	for _, f := range []url.Values{{"title": {"x"}}, {"body": {checklist}}, {"color": {"#abcdef"}}} {
		if body := do(mia, "POST", "/"+id, f, 403); !strings.Contains(body, errViewOnly) {
			t.Errorf("viewer %v: %s", f, body)
		}
	}
	if body := do(mia, "POST", "/"+id, url.Values{"pinned": {"1"}}, 200); !strings.Contains(body, `"can_edit":false`) || !strings.Contains(body, `"pinned":true`) {
		t.Errorf("viewer pin: %s", body)
	}
	do(mia, "POST", "/"+id+"/members", url.Values{"user_id": {dan}}, 403)

	// Roles: only the owner, never the owner's own, only members.
	do(bob, "POST", "/"+id+"/members/"+mia, url.Values{"role": {"edit"}}, 403)
	if body := do(ann, "POST", "/"+id+"/members/"+ann, url.Values{"role": {"view"}}, 400); !strings.Contains(body, "The owner can always edit.") {
		t.Errorf("demote owner: %s", body)
	}
	do(ann, "POST", "/"+id+"/members/"+dan, url.Values{"role": {"view"}}, 404)
	do(ann, "POST", "/"+id+"/members/"+mia, url.Values{"role": {"admin"}}, 400)
	if body := do(ann, "POST", "/"+id+"/members/"+bob, url.Values{"role": {"view"}}, 200); !strings.Contains(body, `"id":"`+bob+`","name":"Bob","role":"view"`) {
		t.Errorf("set role: %s", body)
	}
	do(bob, "POST", "/"+id, url.Values{"title": {"x"}}, 403)
	do(ann, "POST", "/"+id+"/members/"+bob, url.Values{"role": {"edit"}}, 200)

	// Live: Dan isn't a member; Mia joins read-only; Ann joins to edit and seeds from the body.
	if s, msg, err := h.Join(ctx, nil, dan, slug, id); s != nil || msg != errNoteNotFound || err != nil {
		t.Fatalf("outsider joined: %v %q %v", s, msg, err)
	}
	join := func(user string) (*collab.Session, *peer) {
		t.Helper()
		p := &peer{}
		s, msg, err := h.Join(ctx, p, user, slug, id)
		if s == nil || err != nil {
			t.Fatalf("%s join: %q %v", user, msg, err)
		}
		if err := s.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Leave(context.Background()) })
		return s, p
	}
	ms, mp := join(mia)
	if mp.ready(t) != 0 {
		t.Errorf("viewer ready flags %d, want read-only", mp.ready(t))
	}
	_, ap := join(ann)
	if ap.ready(t) != 3 {
		t.Errorf("owner ready flags %d, want edit and seed", ap.ready(t))
	}
	bs, _ := join(bob)
	stored := func() (n int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `select count(*) from note_doc_updates where note_id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	saved := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Live"}]}]}`
	for _, f := range [][]byte{frame(frameUpdate, id, "x"), frame(frameSave, id, saved)} {
		if err := ms.Frame(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	if stored() != 0 || strings.Contains(do(ann, "GET", "", nil, 200), "Live") {
		t.Error("a viewer's frames counted")
	}
	if err := bs.Frame(ctx, frame(frameUpdate, id, "x")); err != nil {
		t.Fatal(err)
	}
	if err := bs.Frame(ctx, frame(frameSave, id, `{"type":"doc","content":[{"type":"table"}]}`)); err != nil {
		t.Fatalf("invalid save: %v", err)
	}
	if err := bs.Frame(ctx, frame(frameSave, id, saved)); err != nil {
		t.Fatal(err)
	}
	if body := do(ann, "GET", "", nil, 200); stored() != 1 || !strings.Contains(body, "Live") || !strings.Contains(body, `"updated_by":{"id":"`+bob) {
		t.Errorf("save frame: %d stored, %s", stored(), body)
	}

	// A REST body write replaces the live document.
	do(ann, "POST", "/"+id, url.Values{"body": {checklist}}, 200)
	var gen int
	if err := pool.QueryRow(ctx, `select body_generation from notes where id = $1`, id).Scan(&gen); err != nil {
		t.Fatal(err)
	}
	if stored() != 0 || gen != 1 {
		t.Errorf("after a REST write: %d stored, generation %d", stored(), gen)
	}
}
