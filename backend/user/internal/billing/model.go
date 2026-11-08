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
	"github.com/devender07/taskwell/backend/user/internal/wsaudit"
)

// account is a user's billing state.
type account struct {
	Plan              string
	Comped            bool
	CustomerID        *string
	SubscriptionID    *string
	Status            *string
	Currency          *string
	Interval          *string
	RenewsAt          *time.Time
	CancelAtPeriodEnd bool
	Owned             int // workspaces the user created
}

// model owns every query of the billing package.
type model struct{ db *pgxpool.Pool }

func (m *model) account(ctx context.Context, userID string) (account, error) {
	var a account
	err := m.db.QueryRow(ctx,
		`select plan, plan_comped, paddle_customer_id, paddle_subscription_id, subscription_status, billing_currency,
		        billing_interval, renews_at, cancel_at_period_end, (select count(*) from workspaces where created_by = u.id)
		   from users u where id = $1`, userID).
		Scan(&a.Plan, &a.Comped, &a.CustomerID, &a.SubscriptionID, &a.Status, &a.Currency, &a.Interval, &a.RenewsAt,
			&a.CancelAtPeriodEnd, &a.Owned)
	if err != nil {
		return a, fmt.Errorf("read billing: %w", err)
	}
	return a, nil
}

// usage is a workspace's view of its owner's plan.
type usage struct {
	Plan               string
	OwnerID, OwnerName *string
	Projects, Seats    int
	Bytes              int64
}

// usage returns the workspace's owner and plan, its projects (archived included), its members plus pending invites,
// and its stored bytes (live uploads included).
func (m *model) usage(ctx context.Context, workspaceID string) (usage, error) {
	var u usage
	err := m.db.QueryRow(ctx,
		`select coalesce(o.plan, 'free'), o.id::text, o.name,
		        (select count(*) from projects where workspace_id = w.id),
		        (select count(*) from workspace_members where workspace_id = w.id)
		      + (select count(*) from workspace_invites where workspace_id = w.id and expires_at > now()),
		        (select coalesce(sum(size), 0) from files where workspace_id = w.id)
		      + (select coalesce(sum(size), 0) from uploads where workspace_id = w.id)
		   from workspaces w left join users o on o.id = w.created_by where w.id = $1`, workspaceID).
		Scan(&u.Plan, &u.OwnerID, &u.OwnerName, &u.Projects, &u.Seats, &u.Bytes)
	if err != nil {
		return u, fmt.Errorf("billing usage: %w", err)
	}
	return u, nil
}

// email returns the user's email.
func (m *model) email(ctx context.Context, userID string) (string, error) {
	var email string
	if err := m.db.QueryRow(ctx, `select email from users where id = $1`, userID).Scan(&email); err != nil {
		return "", fmt.Errorf("read email: %w", err)
	}
	return email, nil
}

// sync applies Paddle's current subscription s, whose plan in force is paid with interval ("" when it isn't in
// force), to its account: the one holding it, else user (the one its signed custom_data names; "" when unsigned)
// while that one has no subscription in force; false when there's none. A subscription no longer in force returns
// the account to Free and clears it, unless the account has moved to another subscription since. A comped account
// keeps its plan. A plan change is audited as billing.plan_changed in every workspace it owns.
func (m *model) sync(ctx context.Context, s subscription, paid, interval, user string) (bool, error) {
	keep := paid != ""
	to := plan.Free
	var renews *time.Time
	var subID, status, currency, cycle *string
	if keep {
		to, renews, subID, status = paid, s.NextBilledAt, &s.ID, &s.Status
		if c := strings.ToLower(s.CurrencyCode); c != "" {
			currency = &c
		}
		if interval == "month" || interval == "year" {
			cycle = &interval
		}
	}
	if user == "" {
		user = "00000000-0000-0000-0000-000000000000"
	}
	found := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var id, from string
		var comped bool
		var current *string
		err := tx.QueryRow(ctx, `select id::text, plan, plan_comped, paddle_subscription_id from users
			  where paddle_subscription_id = $1 or (id = $2 and paddle_subscription_id is null)
			  order by paddle_subscription_id = $1 desc nulls last limit 1 for update`, s.ID, user).
			Scan(&id, &from, &comped, &current)
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
		cancel := keep && s.ScheduledChange != nil && s.ScheduledChange.Action == "cancel"
		if _, err := tx.Exec(ctx,
			`update users set plan = $2, paddle_subscription_id = $3, subscription_status = $4, billing_currency = $5,
			        billing_interval = $6, renews_at = $7, cancel_at_period_end = $8,
			        paddle_customer_id = coalesce(nullif($9, ''), paddle_customer_id)
			  where id = $1`, id, to, subID, status, currency, cycle, renews, cancel, s.CustomerID); err != nil {
			return err
		}
		return planChanged(ctx, tx, id, from, to)
	})
	if err != nil {
		return false, fmt.Errorf("sync subscription: %w", err)
	}
	return found, nil
}

// planChanged runs in the transaction that changed the account's billing: a change of plan from → to is audited as
// billing.plan_changed (actor: the account) in every workspace it owns, and the account's tabs, those of its
// workspaces and the admin app are told.
func planChanged(ctx context.Context, tx pgx.Tx, userID, from, to string) error {
	rows, err := tx.Query(ctx, `select id::text, slug from workspaces where created_by = $1`, userID)
	if err != nil {
		return err
	}
	var ids, slugs []string
	var id, slug string
	if _, err := pgx.ForEachRow(rows, []any{&id, &slug}, func() error {
		ids, slugs = append(ids, id), append(slugs, slug)
		return nil
	}); err != nil {
		return err
	}
	for i, id := range ids {
		if from != to {
			if err := wsaudit.Record(ctx, tx, id, userID, "billing.plan_changed", "", map[string]any{"from": from, "to": to}); err != nil {
				return err
			}
		}
		ws := realtime.Workspace(slugs[i])
		if err := realtime.Publish(ctx, tx, realtime.Event{Kind: "workspace.billing", Workspace: slugs[i],
			Audience: realtime.ToWorkspace(id), Paths: []string{ws + "/billing", ws + "/storage"}}); err != nil {
			return err
		}
	}
	for _, e := range []realtime.Event{
		{Kind: "billing.changed", Audience: realtime.ToUser(userID), Paths: []string{"/billing"}},
		realtime.AdminUsers(""), realtime.AdminWorkspaces(""),
	} {
		if err := realtime.Publish(ctx, tx, e); err != nil {
			return err
		}
	}
	return nil
}
