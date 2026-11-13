package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Listen sends the admin-audience notifications on the realtime channel to the admin tabs here until ctx ends,
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
	pc, err := h.db.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire: %w", err)
	}
	conn := pc.Hijack() // a listening connection never goes back to the pool
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "listen realtime"); err != nil {
		return false, fmt.Errorf("listen: %w", err)
	}
	// Events sent while nothing listened are lost: every tab reloads what it shows.
	h.toAll(text(map[string]string{"type": "resync"}))
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, fmt.Errorf("wait: %w", err)
		}
		var e envelope
		if err := json.Unmarshal([]byte(n.Payload), &e); err != nil {
			slog.Warn("realtime bad notification", "err", err)
			continue
		}
		if e.Aud == audience {
			h.toAll(e.Event)
		}
	}
}
