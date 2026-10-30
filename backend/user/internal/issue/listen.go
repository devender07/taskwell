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
// "<instance> <issue id> u <update id>" for a stored update, "<instance> <issue id> a <base64 frame>" for awareness.
const (
	noteUpdate    = "u"
	noteAwareness = "a"
	maxNote       = 7900 // Postgres rejects payloads of 8000 bytes or more
)

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
	wait := time.Second
	for {
		listened, err := h.listen(ctx)
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

// listen holds one connection out of the pool, listening on collab, until it fails.
func (h *Handler) listen(ctx context.Context) (bool, error) {
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
		h.relay(ctx, n.Payload)
	}
}

// relay broadcasts another instance's frame to the connections here on its issue.
func (h *Handler) relay(ctx context.Context, payload string) {
	n, ok := parseNote(payload)
	if !ok || n.instance == instance || !h.hub.has(n.issueID) {
		return
	}
	var frame []byte
	switch n.kind {
	case noteUpdate:
		id, err := strconv.ParseInt(n.arg, 10, 64)
		if err != nil {
			return
		}
		data, err := h.model.docUpdate(ctx, id)
		if err != nil {
			slog.Error("collab relay failed", "issue", n.issueID, "err", err)
			return
		}
		if data == nil { // compacted away; clients get it from the compacted row on reload
			return
		}
		frame = append([]byte{msgUpdate}, data...)
	case noteAwareness:
		var err error
		if frame, err = base64.StdEncoding.DecodeString(n.arg); err != nil {
			return
		}
	default:
		return
	}
	h.hub.broadcast(n.issueID, nil, frame)
}
