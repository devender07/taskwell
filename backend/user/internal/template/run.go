package template

import (
	"context"
	"log/slog"
	"time"
)

// Run creates the due templates' issues a minute after start (sooner when every is shorter), then every every, until
// ctx ends. Instances can run it side by side: each template is claimed with for update skip locked, and one that's
// no longer due when claimed is skipped, so an occurrence makes one issue.
func (h *Handler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTimer(min(every, time.Minute))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if _, err := h.RunDue(ctx, utcToday()); err != nil && ctx.Err() == nil {
			slog.Error("recurring templates failed", "err", err)
		}
		t.Reset(every)
	}
}

// RunDue creates an issue for each template due on or before today and returns how many it created. A template that
// fails is logged and the rest still run.
func (h *Handler) RunDue(ctx context.Context, today time.Time) (int, error) {
	ids, err := h.model.due(ctx, today)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		created, err := h.model.occur(ctx, id, today)
		if err != nil {
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			slog.Error("recurring template failed", "template", id, "err", err)
		}
		if created {
			n++
		}
	}
	return n, nil
}
