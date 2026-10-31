package realtime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/coder/websocket"
)

// instance owns this process's viewer rows.
var instance = rand.Text()

func presenceMsg(identifier, issueID string, viewers json.RawMessage) []byte {
	return text(map[string]any{"type": "presence", "issue": identifier, "id": issueID, "viewers": viewers})
}

// presence sends the issue's current viewers to its joined connections here.
func (h *Handler) presence(ctx context.Context, issueID string) {
	cs := h.hub.joined(issueID)
	if len(cs) == 0 {
		return
	}
	vs, err := h.model.viewers(ctx, issueID)
	if err != nil {
		slog.Error("realtime presence failed", "issue", issueID, "err", err)
		return
	}
	for c, identifier := range cs {
		if !c.offer(frame{websocket.MessageText, presenceMsg(identifier, issueID, vs)}) {
			c.Close()
		}
	}
}

// Presence keeps this instance's viewer rows fresh and clears crashed instances' stale ones, every 30s until ctx ends.
func (h *Handler) Presence(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := h.model.refresh(ctx, instance); err != nil {
				slog.Error("realtime presence refresh failed", "err", err)
			}
		}
	}
}
