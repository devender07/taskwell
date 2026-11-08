package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/web"
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
		`select plan, plan_comped, paddle_customer_id, paddle_subscription_id, subscription_status, billing_currency,
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

// sync applies Paddle's current subscription s, whose plan in force is paid ("" when it isn't in force), to its
// workspace: the one in its custom_data, else the one with the subscription; false when there's none. A subscription
// no longer in force returns the workspace to Starter and clears it, unless the workspace has moved to another
// subscription since. A comped workspace keeps its plan. A plan change is audited as billing.plan_changed.
func (m *model) sync(ctx context.Context, s subscription, paid string) (bool, error) {
	keep := paid != ""
	to := plan.Starter
	var renews *time.Time
	if keep {
		to, renews = paid, s.NextBilledAt
	}
	var currency *string
	if c := strings.ToLower(s.CurrencyCode); c == "usd" || c == "inr" {
		currency = &c
	}
	where, arg := `paddle_subscription_id = $1`, s.ID
	if web.IsUUID(s.CustomData.WorkspaceID) {
		where, arg = `id = $1`, s.CustomData.WorkspaceID
	}
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var id, slug, from string
		var comped bool
		var current *string
		err := tx.QueryRow(ctx, `select id::text, slug, plan, plan_comped, paddle_subscription_id from workspaces
			  where `+where+` for update`, arg).Scan(&id, &slug, &from, &comped, &current)
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
		cancel := keep && s.ScheduledChange != nil && s.ScheduledChange.Action == "cancel"
		if _, err := tx.Exec(ctx,
			`update workspaces set plan = $2, paddle_subscription_id = $3, subscription_status = $4,
			        billing_currency = coalesce($5, billing_currency), renews_at = $6, cancel_at_period_end = $7,
			        paddle_customer_id = coalesce(nullif($8, ''), paddle_customer_id)
			  where id = $1`, id, to, subID, status, currency, renews, cancel, s.CustomerID); err != nil {
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

// recordAdjustment audits a refund or chargeback on the subscription's workspace; false when there's none.
func (m *model) recordAdjustment(ctx context.Context, subscriptionID, action string, detail map[string]any) (bool, error) {
	var id string
	err := m.db.QueryRow(ctx, `select id::text from workspaces where paddle_subscription_id = $1`, subscriptionID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("record %s: %w", action, err)
	}
	return true, wsaudit.Record(ctx, m.db, id, "", action, "", detail)
}
