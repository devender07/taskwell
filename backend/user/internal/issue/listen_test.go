package issue

import (
	"context"
	"testing"
)

func TestNote(t *testing.T) {
	n, ok := parseNote(notePrefix("issue-1", noteAwareness) + "AQI=")
	if !ok || n.instance != instance || n.issueID != "issue-1" || n.kind != noteAwareness || n.arg != "AQI=" {
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
