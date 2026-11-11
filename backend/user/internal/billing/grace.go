package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/plan"
)

// Run ends the comps that are over (plan_comped_until passed), resets the past_due markers and sends the billing
// reminders that are due (see remind) a minute after start (sooner when every is shorter), then every every, until ctx
// ends. Instances can run it side by side: each row is claimed with for update skip locked.
func (h *Handler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTimer(min(every, time.Minute))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		drain(ctx, "ending comped plan failed", h.endComp)
		if err := h.resetPastDue(ctx); err != nil && ctx.Err() == nil {
			slog.Error("resetting past due markers failed", "err", err)
		}
		drain(ctx, "billing reminder failed", h.remind)
		t.Reset(every)
	}
}

// drain runs step until it reports no more or fails, logging the failure as msg.
func drain(ctx context.Context, msg string, step func(context.Context) (bool, error)) {
	for ctx.Err() == nil {
		more, err := step(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error(msg, "err", err)
		}
		if !more || err != nil {
			return
		}
	}
}

// endComp ends one comp that's over, the way clearing it in the admin app does (the plan becomes the current
// subscription's, else Free; see recompute), and emails the account when that changes its plan. ended is false when
// none is over.
func (h *Handler) endComp(ctx context.Context) (ended bool, err error) {
	err = pgx.BeginFunc(ctx, h.model.db, func(tx pgx.Tx) error {
		var id, email, from, to string
		err := tx.QueryRow(ctx, `select id::text, email, plan from users where plan_comped and plan_comped_until <= now()
			order by plan_comped_until limit 1 for update skip locked`).Scan(&id, &email, &from)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ended = true
		if _, err := tx.Exec(ctx, `update users set plan_comped = false, plan_comped_until = null, plan_comp_reminded_at = null
			where id = $1`, id); err != nil {
			return err
		}
		if err := recompute(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select plan from users where id = $1`, id).Scan(&to); err != nil {
			return err
		}
		if to == from {
			return nil
		}
		return h.sendCompEnded(ctx, tx, email, from, to)
	})
	if err != nil {
		return false, fmt.Errorf("end comped plan: %w", err)
	}
	return ended, nil
}

// sendCompEnded queues the email telling the account its comped plan from ended and it's on to now.
func (h *Handler) sendCompEnded(ctx context.Context, tx pgx.Tx, email, from, to string) error {
	base, err := h.config(ctx, "app_base_url")
	if err != nil {
		return err
	}
	f, _ := plan.Get(from)
	n, _ := plan.Get(to)
	link := strings.TrimRight(base, "/") + "/pricing"
	subject := "Your Taskwell " + f.Name + " access has ended"
	intro := "The " + f.Name + " plan Taskwell gave you has ended. You're on " + n.Name + " now."
	html, err := mail.Render(mail.Email{Preview: intro, Heading: subject, Intro: intro, Button: "See plans", URL: link})
	if err != nil {
		return err
	}
	return h.mail.EnqueueHTML(ctx, tx, email, subject, intro+"\n\nSee plans:\n\n"+link+"\n", html)
}

// reminder is one kind of billing reminder email. claim selects one row due for it, locked (key, email, plan, date),
// and mark sets its marker on key ($1). In the texts, <Plan> is the plan's name and <date> the date ("Nov 24", UTC).
type reminder struct {
	claim, mark                  string
	subject, intro, button, path string
}

var reminders = []reminder{{
	claim: `select s.subscription_id, u.email, s.plan, s.next_billed_at from paddle_subscriptions s join users u on u.id = s.user_id
		where s.status = 'trialing' and s.next_billed_at between now() and now() + interval '3 days'
		  and s.scheduled_change_action is distinct from 'cancel' and s.trial_reminded_at is null
		order by s.next_billed_at limit 1 for update of s skip locked`,
	mark:    `update paddle_subscriptions set trial_reminded_at = now() where subscription_id = $1`,
	subject: "Your Taskwell <Plan> trial ends on <date>",
	intro:   "You'll be charged for <Plan> on <date> unless you cancel.",
	button:  "Manage billing", path: "/billing",
}, {
	// A trial started without a payment method: Paddle cancels it at its end unless one is added.
	claim: `select s.subscription_id, u.email, s.plan, s.trial_ends_at from paddle_subscriptions s join users u on u.id = s.user_id
		where s.status = 'trialing' and s.next_billed_at is null and s.trial_ends_at between now() and now() + interval '3 days'
		  and s.trial_reminded_at is null
		order by s.trial_ends_at limit 1 for update of s skip locked`,
	mark:    `update paddle_subscriptions set trial_reminded_at = now() where subscription_id = $1`,
	subject: "Your Taskwell <Plan> trial ends on <date>",
	intro:   "Add a payment method to keep <Plan> after <date>. Without one, your account moves to the Free plan and nothing is charged.",
	button:  "Add payment method", path: "/billing",
}, {
	claim: `select s.subscription_id, u.email, s.plan, now() from paddle_subscriptions s join users u on u.id = s.user_id
		where s.status = 'past_due' and s.past_due_emailed_at is null
		order by s.paddle_updated_at limit 1 for update of s skip locked`,
	mark:    `update paddle_subscriptions set past_due_emailed_at = now() where subscription_id = $1`,
	subject: "Your Taskwell payment failed",
	intro:   "We couldn't take your payment for <Plan>. Update your payment method to keep your plan.",
	button:  "Update payment method", path: "/billing",
}, {
	claim: `select id::text, email, plan, plan_comped_until from users
		where plan_comped and plan_comped_until between now() and now() + interval '3 days' and plan_comp_reminded_at is null
		order by plan_comped_until limit 1 for update skip locked`,
	mark:    `update users set plan_comp_reminded_at = now() where id = $1`,
	subject: "Your Taskwell <Plan> access ends on <date>",
	intro:   "The <Plan> plan Taskwell gave you ends on <date>. Choose a plan to keep its features.",
	button:  "See plans", path: "/pricing",
}}

// resetPastDue clears the payment failed marker of subscriptions no longer past due, so a later episode emails again.
func (h *Handler) resetPastDue(ctx context.Context) error {
	if _, err := h.model.db.Exec(ctx, `update paddle_subscriptions set past_due_emailed_at = null
		where status <> 'past_due' and past_due_emailed_at is not null`); err != nil {
		return fmt.Errorf("reset past due markers: %w", err)
	}
	return nil
}

// remind sends the first due reminder of reminders, queueing its email and setting its marker in one transaction;
// sent is false when none is due.
func (h *Handler) remind(ctx context.Context) (sent bool, err error) {
	for _, k := range reminders {
		err := pgx.BeginFunc(ctx, h.model.db, func(tx pgx.Tx) error {
			var key, email, planID string
			var at time.Time
			err := tx.QueryRow(ctx, k.claim).Scan(&key, &email, &planID, &at)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			sent = true
			if _, err := tx.Exec(ctx, k.mark, key); err != nil {
				return err
			}
			base, err := h.config(ctx, "app_base_url")
			if err != nil {
				return err
			}
			p, _ := plan.Get(planID)
			fill := strings.NewReplacer("<Plan>", p.Name, "<date>", at.UTC().Format("Jan 2")).Replace
			subject, intro := fill(k.subject), fill(k.intro)
			link := strings.TrimRight(base, "/") + k.path
			html, err := mail.Render(mail.Email{Preview: intro, Heading: subject, Intro: intro, Button: k.button, URL: link})
			if err != nil {
				return err
			}
			return h.mail.EnqueueHTML(ctx, tx, email, subject, intro+"\n\n"+k.button+":\n\n"+link+"\n", html)
		})
		if err != nil {
			return false, fmt.Errorf("send billing reminder: %w", err)
		}
		if sent {
			return true, nil
		}
	}
	return false, nil
}
