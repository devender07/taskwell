package issue

import (
	"context"
	"encoding/base64"
	"slices"
	"testing"
	"time"
)

func TestNote(t *testing.T) {
	n, ok := parseNote(notePrefix("issue-1", noteFrame) + "AQI=")
	if !ok || n.instance != instance || n.issueID != "issue-1" || n.kind != noteFrame || n.arg != "AQI=" {
		t.Fatalf("round trip: %+v %v", n, ok)
	}
	if _, ok := parseNote("too short"); ok {
		t.Fatal("parsed a malformed note")
	}
}

func TestLeaveHandsOffSeed(t *testing.T) {
	hb := &hub{rooms: map[string]*room{}}
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

func TestInlineNote(t *testing.T) {
	id := "0192f2a1-7b3c-7d4e-8f90-a1b2c3d4e5f6"
	frame := tag(msgUpdate, id, []byte{1, 2, 3})
	n, ok := parseNote(inlineNote(id, frame))
	if !ok || n.kind != noteFrame || n.arg != base64.StdEncoding.EncodeToString(frame) {
		t.Fatalf("small update: %+v %v", n, ok)
	}
	if p := inlineNote(id, make([]byte, maxNote)); p != "" {
		t.Fatalf("big update inlined: %d bytes", len(p))
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
