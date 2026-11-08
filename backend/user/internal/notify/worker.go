package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// Run adds due-date reminders and queues notification emails now and every interval until ctx ends: instant emails for notifications older than
// settle (so a quick status change collapses first), then the daily digests and weekly emails that are due. The mail worker sends them.
func (h *Handler) Run(ctx context.Context, every, settle time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := h.queueEmails(ctx, settle); err != nil && ctx.Err() == nil {
			slog.Error("notification emails failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// queueEmails is one run: it adds the due-date reminders, then queues emails. Without SMTP settings the emails wait for
// a later run.
func (h *Handler) queueEmails(ctx context.Context, settle time.Duration) error {
	hour, err := h.cfg.GetInt(ctx, "digest_hour_utc")
	if err != nil {
		return err
	}
	now := time.Now()
	if _, err := h.model.remindDue(ctx, now, hour); err != nil {
		return err
	}
	ok, err := h.mail.Configured(ctx)
	if err != nil {
		return err
	}
	if !ok {
		slog.Debug("notification emails waiting: smtp not configured")
		return nil
	}
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return err
	}
	if err := h.model.skipUnwanted(ctx, settle); err != nil {
		return err
	}
	defer h.mail.Wake() // Enqueue's own wake can come before the transaction commits
	for ctx.Err() == nil {
		more, err := h.model.queueNext(ctx, settle, func(tx pgx.Tx, to string, m message) error {
			subject, body := instantEmail(m, base)
			html, err := instantHTML(m, base)
			if err != nil {
				return err
			}
			return h.mail.EnqueueHTML(ctx, tx, to, subject, body, html)
		})
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	for ctx.Err() == nil {
		more, err := h.model.digestNext(ctx, hour, func(tx pgx.Tx, to string, ms []message) error {
			subject, body := digestEmail(ms, base)
			return h.mail.Enqueue(ctx, tx, to, subject, body)
		})
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	for ctx.Err() == nil {
		more, err := h.model.weeklyNext(ctx, now, hour, func(tx pgx.Tx, to string, lines []weeklyLine) error {
			subject, body := weeklyEmail(lines, base)
			return h.mail.Enqueue(ctx, tx, to, subject, body)
		})
		if err != nil || !more {
			return err
		}
	}
	return nil
}
