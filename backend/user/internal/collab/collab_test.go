package collab

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNote(t *testing.T) {
	n, ok := parseNote(notePrefix("issue-1", noteBatch) + "aAQI=")
	if !ok || n.instance != instance || n.docID != "issue-1" || n.kind != noteBatch || n.arg != "aAQI=" {
		t.Fatalf("round trip: %+v %v", n, ok)
	}
	if _, ok := parseNote("too short"); ok {
		t.Fatal("parsed a malformed note")
	}
}

func TestLeaveHandsOffSeed(t *testing.T) {
	hb := &Hub{rooms: map[string]*room{}}
	newClient := func(canEdit bool) *client { return &client{peer: &fakePeer{}, canEdit: canEdit} }
	seeder, reader, editor := newClient(true), newClient(false), newClient(true)
	for _, c := range []*client{seeder, reader, editor} {
		hb.join("i", c)
	}
	hb.seeding("i", seeder)
	if seeding, passed := hb.leave("i", seeder); !seeding || !passed || len(editor.peer.(*fakePeer).sent) != 1 || len(reader.peer.(*fakePeer).sent) != 0 {
		t.Fatalf("hand-off to the editor: seeding=%v passed=%v", seeding, passed)
	}
	hb.leave("i", reader)
	if seeding, passed := hb.leave("i", editor); !seeding || passed || hb.has("i") {
		t.Fatalf("last seeder leaves: seeding=%v passed=%v", seeding, passed)
	}
}

// fakePeer records what it's sent, with room for one frame.
type fakePeer struct{ sent [][]byte }

func (p *fakePeer) Send(frame []byte) bool {
	if len(p.sent) == 1 {
		return false
	}
	p.sent = append(p.sent, frame)
	return true
}

func (p *fakePeer) Write(_ context.Context, frame []byte) error {
	p.sent = append(p.sent, frame)
	return nil
}

func (p *fakePeer) Close() {}

func TestTag(t *testing.T) {
	f := tag(msgUpdate, "0192f2a1-7b3c-7d4e-8f90-a1b2c3d4e5f6", []byte{9})
	want := []byte{msgUpdate, 0x01, 0x92, 0xf2, 0xa1, 0x7b, 0x3c, 0x7d, 0x4e, 0x8f, 0x90, 0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 9}
	if string(f) != string(want) {
		t.Fatalf("tag = %x, want %x", f, want)
	}
}

func TestBatchNotes(t *testing.T) {
	id := "0192f2a1-7b3c-7d4e-8f90-a1b2c3d4e5f6"
	small := frameItem(tag(msgUpdate, id, []byte{1, 2, 3}))
	if small != "a"+base64.StdEncoding.EncodeToString(tag(msgUpdate, id, []byte{1, 2, 3})) {
		t.Fatalf("frameItem = %q", small)
	}
	if it := frameItem(make([]byte, maxNote)); it != "" {
		t.Fatalf("big frame inlined: %d bytes", len(it))
	}
	big := frameItem(make([]byte, maxItem*3/4-10)) // near the limit
	if big == "" {
		t.Fatal("frame under maxItem not inlined")
	}
	ps := batchNotes(id, []string{small, "u42", small, big})
	if len(ps) != 2 {
		t.Fatalf("got %d notes, want 2", len(ps))
	}
	var items []string
	for _, p := range ps {
		if len(p) > maxNote {
			t.Fatalf("note of %d bytes", len(p))
		}
		n, ok := parseNote(p)
		if !ok || n.kind != noteBatch || n.docID != id {
			t.Fatalf("bad note %.80q", p)
		}
		items = append(items, strings.Split(n.arg, ",")...)
	}
	if !slices.Equal(items, []string{small, "u42", small, big}) {
		t.Fatal("items lost or reordered")
	}
}

func TestOutboxCoalescesAwareness(t *testing.T) {
	h := &Hub{rooms: map[string]*room{}}
	s := &Session{h: h, c: &client{}, doc: Doc{ID: "i"}}
	s.out.awareAt = time.Now() // just sent one
	s.awareness([]byte{1})
	s.awareness([]byte{2})
	s.out.mu.Lock()
	due, aware := time.Until(s.out.due), s.out.aware
	s.out.timer.Stop()
	s.out.mu.Unlock()
	if string(aware) != string([]byte{2}) || due < awarenessEvery-50*time.Millisecond || due > awarenessEvery {
		t.Fatalf("aware=%v due in %v, want the latest, due in about %v", aware, due, awarenessEvery)
	}
	s.queue("u1")
	s.out.mu.Lock()
	due = time.Until(s.out.due)
	s.out.timer.Stop()
	s.out.mu.Unlock()
	if due > batchEvery {
		t.Fatalf("an update waits %v, want at most %v", due, batchEvery)
	}
}

func TestCompactableOnlyLoaded(t *testing.T) {
	c := &client{loaded: []int64{11, 12, 13, 15}} // 14 committed after the load
	if ids := c.compactable(15); !slices.Equal(ids, []int64{11, 12, 13, 15}) {
		t.Fatalf("compactable(15) = %v", ids)
	}
	if ids := c.compactable(15); len(ids) != 0 {
		t.Fatalf("second compaction = %v, want none", ids)
	}
	c.loaded = []int64{1, 2, 3}
	if ids := c.compactable(2); !slices.Equal(ids, []int64{1, 2}) {
		t.Fatalf("compactable(2) = %v", ids)
	}
}

func TestAllowRate(t *testing.T) {
	c := &client{}
	now := time.Now()
	n := 0
	for c.allow(now) {
		n++
	}
	if n != frameBurst {
		t.Fatalf("burst = %d, want %d", n, frameBurst)
	}
	later := now.Add(time.Second/frameRate + time.Millisecond)
	if !c.allow(later) || c.allow(later) {
		t.Fatal("one token should refill per 1/frameRate")
	}
}

func TestFrameDropsAwarenessTooBigToRelay(t *testing.T) {
	h := &Hub{rooms: map[string]*room{}}
	id := "00000000-0000-0000-0000-000000000001"
	s := &Session{h: h, c: &client{}, doc: Doc{ID: id}}
	if err := s.Frame(context.Background(), tag(msgAwareness, id, make([]byte, maxItem))); err != nil {
		t.Fatal(err)
	}
	if s.out.aware != nil {
		t.Fatal("oversized awareness kept")
	}
	if err := s.Frame(context.Background(), tag(msgAwareness, id, []byte{1})); err != nil {
		t.Fatal(err)
	}
	s.out.mu.Lock()
	defer s.out.mu.Unlock()
	if s.out.aware == nil {
		t.Fatal("small awareness dropped")
	}
	s.out.timer.Stop()
}

type recPeer struct{ frames [][]byte }

func (p *recPeer) Send(f []byte) bool                      { p.frames = append(p.frames, f); return true }
func (p *recPeer) Write(_ context.Context, f []byte) error { p.Send(f); return nil }
func (p *recPeer) Close()                                  {}

func TestResetRoomIgnoresTheOldDocument(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000001"
	h := &Hub{rooms: map[string]*room{}}
	p := &recPeer{}
	s := &Session{h: h, c: &client{peer: p, canEdit: true}, doc: Doc{ID: id}}
	h.join(id, s.c)
	h.seeding(id, s.c)
	h.resetRoom(id)
	if len(p.frames) != 1 || p.frames[0][0] != msgReset || len(p.frames[0]) != tagLen {
		t.Fatalf("frames %v, want one reset", p.frames)
	}
	if h.seedPending(id) {
		t.Fatal("a reset room still waits for its seed")
	}
	// The hub has no pool: storing the update would panic.
	if err := s.Frame(context.Background(), tag(msgUpdate, id, []byte{1})); err != nil {
		t.Fatal(err)
	}
	if seeding, _ := h.leave(id, s.c); seeding {
		t.Fatal("a reset client left as the seeder")
	}
}
