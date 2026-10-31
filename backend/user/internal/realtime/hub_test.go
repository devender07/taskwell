package realtime

import (
	"context"
	"testing"
)

func newTestConn(userID string) *Conn {
	_, cancel := context.WithCancel(context.Background())
	return &Conn{id: userID + "-conn", userID: userID, events: make(chan []byte, 4), cancel: cancel}
}

func TestHub(t *testing.T) {
	hb := newHub()
	maya, demo, other := newTestConn("maya"), newTestConn("demo"), newTestConn("other")
	for _, c := range []*Conn{maya, demo, other} {
		hb.add(c)
	}
	hb.subscribe(maya, "acme")
	hb.subscribe(demo, "acme")
	hb.subscribe(other, "globex")

	hb.toWorkspace("acme", []byte("ws"))
	hb.toUser("maya", []byte("user"))
	if len(maya.events) != 2 || len(demo.events) != 1 || len(other.events) != 0 {
		t.Fatalf("delivered maya=%d demo=%d other=%d", len(maya.events), len(demo.events), len(other.events))
	}

	hb.subscribe(other, "acme") // replaces globex
	hb.toWorkspace("globex", []byte("ws"))
	if len(other.events) != 0 {
		t.Fatal("old subscription still delivers")
	}

	if cs := hb.unsubscribe("acme", "maya"); len(cs) != 1 || cs[0] != maya || maya.ws != "" {
		t.Fatalf("unsubscribe the user: %v", cs)
	}
	hb.toWorkspace("acme", []byte("ws"))
	if len(maya.events) != 2 || len(demo.events) != 2 {
		t.Fatalf("after removal maya=%d demo=%d", len(maya.events), len(demo.events))
	}

	hb.remove(demo)
	hb.toWorkspace("acme", []byte("ws"))
	hb.toUser("demo", []byte("user"))
	if len(demo.events) != 2 || len(hb.users["demo"]) != 0 {
		t.Fatal("removed connection still delivers")
	}
}
