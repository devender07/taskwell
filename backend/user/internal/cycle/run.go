package cycle

import (
	"context"
	"log/slog"
	"time"
)

// Run records today's points of every current cycle for the burndown: a minute after start (sooner when every is
// shorter), then every interval, until ctx is done.
func (h *Handler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTimer(min(every, time.Minute))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := h.model.snapshotPoints(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Error("cycle points snapshot failed", "err", err)
		}
		t.Reset(every)
	}
}
