package billing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/wsaudit"
)

// account is a workspace's billing state.
type account struct {
	Plan              string
	Comped            bool
	CustomerID        *string
	SubscriptionID    *string
	Status            *string
	Currency          *string
	RenewsAt          *time.Time
	CancelAtPeriodEnd bool
}

// model owns every query of the billing package.
type model struct{ db *pgxpool.Pool }

func (m *model) account(ctx context.Context, workspaceID string) (account, error) {
	var a account
	err := m.db.QueryRow(ctx,
		`select plan, plan_comped, stripe_customer_id, stripe_subscription_id, subscription_status, billing_currency,
		        renews_at, cancel_at_period_end
		   from workspaces where id = $1`, workspaceID).
		Scan(&a.Plan, &a.Comped, &a.CustomerID, &a.SubscriptionID, &a.Status, &a.Currency, &a.RenewsAt, &a.CancelAtPeriodEnd)
	if err != nil {
		return a, fmt.Errorf("read billing: %w", err)
	}
	return a, nil
}

// usage returns the workspace's members plus pending invites, and its stored bytes (live uploads included).
func (m *model) usage(ctx context.Context, workspaceID string) (seats int, bytes int64, err error) {
	err = m.db.QueryRow(ctx,
		`select (select count(*) from workspace_members where workspace_id = $1)
		      + (select count(*) from workspace_invites where workspace_id = $1 and expires_at > now()),
		        (select coalesce(sum(size), 0) from files where workspace_id = $1)
		      + (select coalesce(sum(size), 0) from uploads where workspace_id = $1)`, workspaceID).Scan(&seats, &bytes)
	if err != nil {
		return 0, 0, fmt.Errorf("billing usage: %w", err)
	}
	return seats, bytes, nil
}

// caller returns the user's email and timezone (their notification settings'; "" when unset).
func (m *model) caller(ctx context.Context, userID string) (email, timezone string, err error) {
	if err := m.db.QueryRow(ctx, `select u.email, coalesce(n.timezone, '') from users u
		 left join notification_settings n on n.user_id = u.id where u.id = $1`, userID).
		Scan(&email, &timezone); err != nil {
		return "", "", fmt.Errorf("read caller: %w", err)
	}
	return email, timezone, nil
}

// saveCustomer stores the Stripe customer id unless the workspace has one already, and returns the one it has.
func (m *model) saveCustomer(ctx context.Context, workspaceID, customerID string) (string, error) {
	var id string
	if err := m.db.QueryRow(ctx,
		`update workspaces set stripe_customer_id = coalesce(stripe_customer_id, $2) where id = $1
		 returning stripe_customer_id`, workspaceID, customerID).Scan(&id); err != nil {
		return "", fmt.Errorf("save customer: %w", err)
	}
	return id, nil
}

// linkCheckout records a completed checkout's customer (unless the workspace has one) and subscription; false when
// there's no such workspace.
func (m *model) linkCheckout(ctx context.Context, workspaceID, customerID, subscriptionID string) (bool, error) {
	tag, err := m.db.Exec(ctx,
		`update workspaces set stripe_customer_id = coalesce(stripe_customer_id, $2), stripe_subscription_id = $3
		  where id = $1`, workspaceID, customerID, subscriptionID)
	if err != nil {
		return false, fmt.Errorf("link checkout: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// keeps are the subscription statuses that keep the paid plan.
var keeps = []string{"active", "trialing", "past_due"}

// sync applies Stripe's current subscription s to the workspace of its customer; false when there's none. An ended
// subscription returns the workspace to Starter and clears it, unless the workspace has moved to another
// subscription since. A comped workspace keeps its plan. A plan change is audited as billing.plan_changed.
func (m *model) sync(ctx context.Context, s subscription) (bool, error) {
	keep := slices.Contains(keeps, s.Status)
	to := plan.Starter
	var renews *time.Time
	if keep {
		if len(s.Items.Data) == 0 {
			return false, fmt.Errorf("sync %s: no items", s.ID)
		}
		item := s.Items.Data[0]
		p, ok := plan.ByLookupKey(item.Price.LookupKey)
		if !ok {
			return false, fmt.Errorf("sync %s: unknown price lookup key %q", s.ID, item.Price.LookupKey)
		}
		to = p.ID
		if item.CurrentPeriodEnd > 0 {
			t := time.Unix(item.CurrentPeriodEnd, 0).UTC()
			renews = &t
		}
	}
	var currency *string
	if s.Currency == "usd" || s.Currency == "inr" {
		currency = &s.Currency
	}
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var id, slug, from string
		var comped bool
		var current *string
		err := tx.QueryRow(ctx,
			`select id::text, slug, plan, plan_comped, stripe_subscription_id from workspaces
			  where stripe_customer_id = $1 for update`, s.Customer).Scan(&id, &slug, &from, &comped, &current)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if !keep && current != nil && *current != s.ID {
			return nil // an older subscription ending
		}
		if comped {
			to = from
		}
		var subID, status *string
		if keep {
			subID, status = &s.ID, &s.Status
		}
		if _, err := tx.Exec(ctx,
			`update workspaces set plan = $2, stripe_subscription_id = $3, subscription_status = $4,
			        billing_currency = coalesce($5, billing_currency), renews_at = $6, cancel_at_period_end = $7
			  where id = $1`, id, to, subID, status, currency, renews, keep && s.CancelAtPeriodEnd); err != nil {
			return err
		}
		if to != from {
			if err := wsaudit.Record(ctx, tx, id, "", "billing.plan_changed", "", map[string]any{"from": from, "to": to}); err != nil {
				return err
			}
		}
		return changed(ctx, tx, id, slug)
	})
	if err != nil {
		return false, fmt.Errorf("sync subscription: %w", err)
	}
	return found, nil
}

// changed tells the workspace's tabs and the admin app that its billing changed.
func changed(ctx context.Context, tx pgx.Tx, workspaceID, slug string) error {
	ws := realtime.Workspace(slug)
	if err := realtime.Publish(ctx, tx, realtime.Event{Kind: "workspace.billing", Workspace: slug,
		Audience: realtime.ToWorkspace(workspaceID), Paths: []string{ws + "/billing", ws + "/storage"}}); err != nil {
		return err
	}
	return realtime.Publish(ctx, tx, realtime.AdminWorkspaces(""))
}

// recordCharge audits a dispute or refund on the customer's workspace; false when there's none.
func (m *model) recordCharge(ctx context.Context, customerID, action string, detail map[string]any) (bool, error) {
	var id string
	err := m.db.QueryRow(ctx, `select id::text from workspaces where stripe_customer_id = $1`, customerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("record %s: %w", action, err)
	}
	return true, wsaudit.Record(ctx, m.db, id, "", action, "", detail)
}
