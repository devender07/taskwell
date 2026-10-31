package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var resync = text(map[string]string{"type": "resync"})

// Listen fans out every instance's notifications on the realtime channel to the connections here until ctx ends,
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
		slog.Warn("realtime listener down", "err", err, "retry_in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 30*time.Second)
	}
}

// listen holds one connection out of the pool, listening on realtime, until it fails.
func (h *Handler) listen(ctx context.Context) (bool, error) {
	pc, err := h.model.db.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack() // a listening connection never goes back to the pool
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "listen realtime"); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	// Events sent while nothing listened are lost: every tab reloads what it shows.
	h.hub.toAll(resync)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait: %w", err)
		}
		h.deliver(ctx, n.Payload)
	}
}

// deliver sends a notification's event to its audience here, or the issue's viewers to its joined connections.
func (h *Handler) deliver(ctx context.Context, payload string) {
	var e envelope
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		slog.Warn("realtime bad notification", "err", err)
		return
	}
	aud, id, _ := strings.Cut(e.Aud, ":")
	switch aud {
	case "ws":
		h.hub.toWorkspace(id, e.Event)
	case "user":
		h.hub.toUser(id, e.Event)
	case "issue":
		h.presence(ctx, id)
		return
	default: // admin: the admin service's
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
