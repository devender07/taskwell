package collab

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

// Live-editing frames cross API instances as notifications on the store's Postgres channel:
// "<instance> <doc id> b <items>" carries a session's batched frames (see outbox), comma-separated, each
// "a<base64 tagged frame>" for awareness and updates small enough to fit, "u<update id>" for a stored update that
// doesn't; "<instance> <doc id> c " tells every instance, the sender too, to reset the document's editors (a REST
// write replaced the live document).
const (
	noteBatch = "b"
	noteReset = "c"
	maxNote   = 7500 // Postgres rejects payloads of 8000 bytes or more
)

// instance tells this process's notifications from other instances'.
var instance = rand.Text()

func notePrefix(docID, kind string) string { return instance + " " + docID + " " + kind + " " }

type note struct{ instance, docID, kind, arg string }

func parseNote(s string) (note, bool) {
	f := strings.SplitN(s, " ", 4)
	if len(f) != 4 {
		return note{}, false
	}
	return note{f[0], f[1], f[2], f[3]}, true
}

// batchNotes joins items into as few notifications for the document as fit in maxNote each. An item too long to fit
// in one is dropped: callers keep items under maxItem.
func batchNotes(docID string, items []string) []string {
	prefix := notePrefix(docID, noteBatch)
	var out []string
	var b strings.Builder
	for _, it := range items {
		if len(prefix)+len(it) > maxNote {
			continue
		}
		if b.Len() > 0 && b.Len()+1+len(it) > maxNote {
			out = append(out, b.String())
			b.Reset()
		}
		if b.Len() == 0 {
			b.WriteString(prefix)
		} else {
			b.WriteByte(',')
		}
		b.WriteString(it)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// maxItem is the longest batch item: a notification holding only it fits.
var maxItem = maxNote - len(notePrefix("00000000-0000-0000-0000-000000000000", noteBatch))

// frameItem is the batch item carrying frame itself; "" when it doesn't fit.
func frameItem(frame []byte) string {
	if 1+base64.StdEncoding.EncodedLen(len(frame)) > maxItem {
		return ""
	}
	return "a" + base64.StdEncoding.EncodeToString(frame)
}

// Listen relays other instances' live-editing frames to the connections here until ctx ends,
// reconnecting with backoff when its Postgres connection drops.
func (h *Hub) Listen(ctx context.Context) {
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

// listen holds one connection out of the pool, listening on the store's channel, until it fails. It relays inline frames itself
// and queues stored updates on loads, so it never waits on a query and Postgres's notification queue keeps draining.
func (h *Hub) listen(ctx context.Context, loads chan<- note) (bool, error) {
	pc, err := h.db.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack() // a listening connection never goes back to the pool
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "listen "+pgx.Identifier{h.store.Channel}.Sanitize()); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	h.listening.Store(true)
	defer h.listening.Store(false)
	// Notifications sent while nothing listened are lost: end every connection so its editor reloads.
	h.closeAll(realtime.ResyncSpread)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait: %w", err)
		}
		h.relay(n.Payload, loads)
	}
}

// relay broadcasts another instance's frames to the connections here on their document and queues stored updates'
// loads, or resets the document's editors. When the queue is full its editors here are closed, so they reload
// instead of missing the update.
func (h *Hub) relay(payload string, loads chan<- note) {
	n, ok := parseNote(payload)
	if !ok || !h.has(n.docID) {
		return
	}
	if n.kind == noteReset {
		h.resetRoom(n.docID)
		return
	}
	if n.kind != noteBatch || n.instance == instance {
		return
	}
	for it := range strings.SplitSeq(n.arg, ",") {
		switch {
		case strings.HasPrefix(it, "a"):
			if frame, err := base64.StdEncoding.DecodeString(it[1:]); err == nil {
				h.broadcast(n.docID, nil, frame)
			}
		case strings.HasPrefix(it, "u"):
			select {
			case loads <- note{n.instance, n.docID, "u", it[1:]}:
			default:
				slog.Warn("collab relay queue full", "doc", n.docID)
				h.closeRoom(n.docID)
				return
			}
		}
	}
}

// Listening reports whether the LISTEN on the store's channel is connected.
func (h *Hub) Listening() bool { return h.listening.Load() }

// loadUpdates loads and relays the queued stored updates until ctx ends. An update that's gone (compacted) or fails
// to load closes the document's editors here, so they reload rather than diverge.
func (h *Hub) loadUpdates(ctx context.Context, loads <-chan note) {
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
			data, err := h.docUpdate(qctx, id)
			cancel()
			if err != nil {
				slog.Error("collab relay failed", "doc", n.docID, "err", err)
			}
			if data == nil {
				h.closeRoom(n.docID)
				continue
			}
			h.broadcast(n.docID, nil, tag(msgUpdate, n.docID, data))
		}
	}
}
