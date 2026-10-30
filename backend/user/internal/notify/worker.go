package notify

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// maxFailures ends a run after that many failed sends; the rest wait for the next run.
const maxFailures = 5

// Run emails notifications now and every interval until ctx ends: instant emails for notifications older than
// settle (so a quick status change collapses first), then the digests that are due.
func (h *Handler) Run(ctx context.Context, every, settle time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := h.sendEmails(ctx, settle); err != nil && ctx.Err() == nil {
			slog.Error("notification emails failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sendEmails is one run. Without SMTP settings everything waits for a later run.
// ponytail: a failing address is retried every run forever; add an attempt count when bounces show up.
func (h *Handler) sendEmails(ctx context.Context, settle time.Duration) error {
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
	hour, err := h.cfg.GetInt(ctx, "digest_hour_utc")
	if err != nil {
		return err
	}
	if err := h.model.skipUnwanted(ctx, settle); err != nil {
		return err
	}
	var errs []error
	failed := []string{} // not nil: pgx sends a nil slice as null, and "<> all(null)" matches nothing
	for len(errs) < maxFailures && ctx.Err() == nil {
		id, err := h.model.sendNext(ctx, settle, failed, func(to string, m message) error {
			subject, body := instantEmail(m, base)
			return h.mail.Send(ctx, to, subject, body)
		})
		if id == "" && err == nil {
			break
		}
		if err != nil {
			errs, failed = append(errs, err), append(failed, id)
		}
	}
	failed = []string{}
	for len(errs) < maxFailures && ctx.Err() == nil {
		userID, err := h.model.digestNext(ctx, hour, failed, func(to string, ms []message) error {
			subject, body := digestEmail(ms, base)
			return h.mail.Send(ctx, to, subject, body)
		})
		if userID == "" && err == nil {
			break
		}
		if err != nil {
			errs, failed = append(errs, err), append(failed, userID)
		}
	}
	return errors.Join(errs...)
}
