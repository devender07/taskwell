package issue

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Live-editing frames cross API instances as notifications on the Postgres channel collab:
// "<instance> <issue id> a <base64 tagged frame>" for awareness and updates small enough to fit,
// "<instance> <issue id> u <update id>" for a stored update that doesn't.
const (
	noteUpdate = "u"
	noteFrame  = "a"
	maxNote    = 7500 // Postgres rejects payloads of 8000 bytes or more
)

// inlineNote is the notification carrying the tagged frame itself; "" when it doesn't fit.
func inlineNote(issueID string, frame []byte) string {
	p := notePrefix(issueID, noteFrame)
	if len(p)+base64.StdEncoding.EncodedLen(len(frame)) > maxNote {
		return ""
	}
	return p + base64.StdEncoding.EncodeToString(frame)
}

// instance tells this process's notifications from other instances'.
var instance = rand.Text()

func notePrefix(issueID, kind string) string { return instance + " " + issueID + " " + kind + " " }

type note struct{ instance, issueID, kind, arg string }

func parseNote(s string) (note, bool) {
	f := strings.SplitN(s, " ", 4)
	if len(f) != 4 {
		return note{}, false
	}
	return note{f[0], f[1], f[2], f[3]}, true
}

// Listen relays other instances' live-editing frames to the connections here until ctx ends,
// reconnecting with backoff when its Postgres connection drops.
func (h *Handler) Listen(ctx context.Context) {
	loads := make(chan note, loadQueue)
	go h.loadUpdates(ctx, loads)
	wait := time.Second
	for {
		listened, err := h.listen(ctx, loads)
		if ctx.Err() != nil {
			return
		}
		if listened {
			wait = time.Second
		}
		slog.Warn("collab listener down", "err", err, "retry_in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// loadQueue bounds the stored updates waiting to be loaded and relayed.
const loadQueue = 256

// listen holds one connection out of the pool, listening on collab, until it fails. It relays inline frames itself
// and queues stored updates on loads, so it never waits on a query and Postgres's notification queue keeps draining.
func (h *Handler) listen(ctx context.Context, loads chan<- note) (bool, error) {
	pc, err := h.model.db.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack() // a listening connection never goes back to the pool
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "listen collab"); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	// Notifications sent while nothing listened are lost: end every connection so its editor reloads.
	h.hub.closeAll()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait: %w", err)
		}
		h.relay(n.Payload, loads)
	}
}

// relay broadcasts another instance's frame to the connections here on its issue, or queues a stored update's load.
// When the queue is full the issue's editors here are closed, so they reload instead of missing the update.
func (h *Handler) relay(payload string, loads chan<- note) {
	n, ok := parseNote(payload)
	if !ok || n.instance == instance || !h.hub.has(n.issueID) {
		return
	}
	switch n.kind {
	case noteFrame:
		if frame, err := base64.StdEncoding.DecodeString(n.arg); err == nil {
			h.hub.broadcast(n.issueID, nil, frame)
		}
	case noteUpdate:
		select {
		case loads <- n:
		default:
			slog.Warn("collab relay queue full", "issue", n.issueID)
			h.hub.closeRoom(n.issueID)
		}
	}
}

// loadUpdates loads and relays the queued stored updates until ctx ends. An update that's gone (compacted) or fails
// to load closes the issue's editors here, so they reload rather than diverge.
func (h *Handler) loadUpdates(ctx context.Context, loads <-chan note) {
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-loads:
			id, err := strconv.ParseInt(n.arg, 10, 64)
			if err != nil {
				continue
			}
			qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			data, err := h.model.docUpdate(qctx, id)
			cancel()
			if err != nil {
				slog.Error("collab relay failed", "issue", n.issueID, "err", err)
			}
			if data == nil {
				h.hub.closeRoom(n.issueID)
				continue
			}
			h.hub.broadcast(n.issueID, nil, tag(msgUpdate, n.issueID, data))
		}
	}
}
