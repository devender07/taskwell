package issue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/devender07/tether/backend/user/internal/mail"
)

// notifyMentions emails each workspace member mentioned in doc but not in old, other than the actor; doc is the
// issue's description, or one of its comments when comment is true. It returns at once; the emails go out in the
// background.
// ponytail: one goroutine per save, no retry; move to a queue when mention volume grows (Phase 6 notifications).
func (h *Handler) notifyMentions(ctx context.Context, issueID, actorID string, old, doc []byte, comment bool) {
	before := mentions(old)
	ids := slices.DeleteFunc(mentions(doc), func(id string) bool { return slices.Contains(before, id) })
	if len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	go func() {
		defer cancel()
		if err := h.sendMentions(ctx, issueID, actorID, ids, comment); err != nil {
			slog.Error("mention email failed", "issue", issueID, "err", err)
		}
	}()
}

func (h *Handler) sendMentions(ctx context.Context, issueID, actorID string, ids []string, comment bool) error {
	mn, err := h.model.mentionTargets(ctx, issueID, actorID, ids)
	if err != nil || len(mn.emails) == 0 {
		return err
	}
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return err
	}
	subject := mn.actor + " mentioned you in " + mn.identifier
	where := ""
	if comment {
		where = "a comment on "
	}
	body := fmt.Sprintf("%s mentioned you in %s%s: %s\n\n%s/%s/issue/%s\n", mn.actor, where, mn.identifier, mn.title, strings.TrimRight(base, "/"), mn.slug, mn.identifier)
	var errs []error
	for _, to := range mn.emails {
		if err := h.mail.Send(ctx, to, subject, body); errors.Is(err, mail.ErrNotConfigured) {
			return nil
		} else if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
