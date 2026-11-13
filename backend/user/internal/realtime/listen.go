package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
)

var resync = text(map[string]string{"type": "resync"})

// ResyncSpread is how long after a listener reconnects its connections are told to reload, each at a random moment.
const ResyncSpread = 5 * time.Second

// Listen fans out every instance's notifications on the realtime channel to the connections here until ctx ends,
// reconnecting with backoff when its Postgres connection drops.
func (h *Handler) Listen(ctx context.Context) {
	presence := make(chan string, presenceQueue)
	go h.sendPresence(ctx, presence)
	wait := time.Second
	for {
		listened, err := h.listen(ctx, presence)
		if ctx.Err() != nil {
			return
		}
		if listened {
			wait = time.Second
		}
		slog.Warn("realtime listener down", "err", err, "retry_in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// presenceQueue bounds the issues waiting for their viewers to be loaded and sent.
const presenceQueue = 256

// sendPresence sends the queued issues' viewers until ctx ends.
func (h *Handler) sendPresence(ctx context.Context, issues <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-issues:
			qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			h.presence(qctx, id)
			cancel()
		}
	}
}

// listen holds one connection out of the pool, listening on realtime, until it fails. Presence, the only delivery
// that queries, goes to the presence queue, so the listener never waits on the pool.
func (h *Handler) listen(ctx context.Context, presence chan<- string) (bool, error) {
	pc, err := h.model.db.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack() // a listening connection never goes back to the pool
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "listen realtime"); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	h.listening.Store(true)
	defer h.listening.Store(false)
	// Events sent while nothing listened are lost: every tab reloads what it shows, and config is read again. The
	// resyncs are spread out so a restart's tabs don't all refetch at once.
	for _, c := range h.hub.all() {
		time.AfterFunc(rand.N(ResyncSpread), func() { c.event(resync) })
	}
	h.configChanged()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait: %w", err)
		}
		h.deliver(ctx, n.Payload, presence)
	}
}

// deliver sends a notification's event to its audience here, or the issue's viewers to its joined connections.
func (h *Handler) deliver(ctx context.Context, payload string, presence chan<- string) {
	var e envelope
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		slog.Warn("realtime bad notification", "err", err)
		return
	}
	aud, id, _ := strings.Cut(e.Aud, ":")
	switch aud {
	case "ws":
		h.hub.toWorkspace(id, e.Skip, e.Event)
	case "user":
		h.hub.toUser(id, e.Skip, e.Event)
		if e.Close {
			h.revoked(id)
			h.hub.closeUser(id)
		}
	case "issue":
		select {
		case presence <- id:
		default: // ponytail: dropped, so viewers show stale until the issue's next presence change; dedupe queued ids if this logs
			slog.Warn("realtime presence queue full", "issue", id)
		}
		return
	default: // admin: the admin service's, but a config change applies here too
		var ev struct{ Kind string }
		if json.Unmarshal(e.Event, &ev) == nil && ev.Kind == "admin.config" {
			h.configChanged()
		}
		return
	}
	if e.WS == "" {
		return
	}
	user := ""
	if aud == "user" {
		user = id
	}
	for _, c := range h.hub.unsubscribe(e.WS, user) {
		go h.dropWorkspace(ctx, c, e.WS)
	}
}

func (h *Handler) configChanged() {
	if h.ConfigChanged != nil {
		h.ConfigChanged()
	}
}

// Listening reports whether the realtime LISTEN is connected.
func (h *Handler) Listening() bool { return h.listening.Load() }

// Sockets returns the number of open realtime sockets here.
func (h *Handler) Sockets() int { return h.hub.count() }
