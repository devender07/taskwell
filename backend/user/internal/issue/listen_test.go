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
	newClient := func(canEdit bool) *client {
		_, cancel := context.WithCancel(context.Background())
		return &client{send: make(chan []byte, 1), cancel: cancel, canEdit: canEdit}
	}
	seeder, reader, editor := newClient(true), newClient(false), newClient(true)
	for _, c := range []*client{seeder, reader, editor} {
		hb.join("i", c)
	}
	hb.seeding("i", seeder)
	if seeding, passed := hb.leave("i", seeder); !seeding || !passed || len(editor.send) != 1 || len(reader.send) != 0 {
		t.Fatalf("hand-off to the editor: seeding=%v passed=%v", seeding, passed)
	}
	hb.leave("i", reader)
	if seeding, passed := hb.leave("i", editor); !seeding || passed || hb.has("i") {
		t.Fatalf("last seeder leaves: seeding=%v passed=%v", seeding, passed)
	}
}
