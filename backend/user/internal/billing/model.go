package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/wsaudit"
)

// account is a user's billing state; the subscription fields are its current subscription's (see current), nil
// without one.
type account struct {
	Plan              string
	Comped            bool
	SubscriptionID    *string
	PriceID           *string
	Status            *string
	Currency          *string
	Interval          *string
	RenewsAt          *time.Time
	CancelAtPeriodEnd bool
	Owned             int // workspaces the user created
}

// current is the user's ($1) current subscription: its most recently updated one granting access ($2).
const current = `select * from paddle_subscriptions where user_id = $1 and status = any($2)
	order by paddle_updated_at desc limit 1`

// model owns every query of the billing package.
type model struct{ db *pgxpool.Pool }

func (m *model) account(ctx context.Context, userID string) (account, error) {
	var a account
	err := m.db.QueryRow(ctx,
		`select u.plan, u.plan_comped, s.subscription_id, s.price_id, s.status, s.currency_code, s.billing_interval, s.next_billed_at,
		        coalesce(s.scheduled_change_action = 'cancel', false), (select count(*) from workspaces where created_by = u.id)
		   from users u left join lateral (`+current+`) s on true where u.id = $1`, userID, granting).
		Scan(&a.Plan, &a.Comped, &a.SubscriptionID, &a.PriceID, &a.Status, &a.Currency, &a.Interval, &a.RenewsAt,
			&a.CancelAtPeriodEnd, &a.Owned)
	if err != nil {
		return a, fmt.Errorf("read billing: %w", err)
	}
	return a, nil
}

// portalTarget is the customer and subscription the user's portal opens on: its current subscription, else its most
// recently updated one (a paused or canceled one still has invoices and a resume); found is false without any.
func (m *model) portalTarget(ctx context.Context, userID string) (customerID, subID string, found bool, err error) {
	err = m.db.QueryRow(ctx, `select customer_id, subscription_id from paddle_subscriptions where user_id = $1
		order by status = any($2) desc, paddle_updated_at desc limit 1`, userID, granting).Scan(&customerID, &subID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read portal subscription: %w", err)
	}
	return customerID, subID, true, nil
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

// subscriptionRow is a subscription entity as it's mirrored.
type subscriptionRow struct {
	ID, CustomerID, Status, PriceID, ProductID, Plan string
	Interval, Currency, ScheduledAction              *string
	NextBilledAt, ScheduledAt                        *time.Time
	UpdatedAt                                        time.Time
}

// existingUser is $n as a user id when that user exists, else null.
func existingUser(n string) string {
	return `(select id from users where id = nullif(` + n + `, '')::uuid)`
}

// saveSubscription mirrors s unless the row is newer (then it's a no-op and applied is false). A subscription keeps
// the account it's linked to; an unlinked one is linked to link (a user id its signed custom_data names, "" for none)
// when that account has no other subscription granting access, and so is its customer. A subscription event for a
// customer not seen yet makes a placeholder customer. Then the linked account's plan is recomputed (see recompute).
func (m *model) saveSubscription(ctx context.Context, s subscriptionRow, link string) (applied bool, user *string, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if link != "" {
			var free bool
			err := tx.QueryRow(ctx, `select not exists (select 1 from paddle_subscriptions
				  where user_id = u.id and subscription_id <> $2 and status = any($3))
				  from users u where u.id = $1 for update`, link, s.ID, granting).Scan(&free)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !free) {
				link = ""
			} else if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `insert into paddle_customers (customer_id) values ($1) on conflict do nothing`, s.CustomerID); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `insert into paddle_subscriptions (subscription_id, customer_id, user_id, status, price_id,
			  product_id, plan, billing_interval, currency_code, next_billed_at, scheduled_change_action,
			  scheduled_change_at, paddle_updated_at)
			values ($1, $2, `+existingUser("$3")+`, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			on conflict (subscription_id) do update set customer_id = excluded.customer_id,
			  user_id = coalesce(paddle_subscriptions.user_id, excluded.user_id), status = excluded.status,
			  price_id = excluded.price_id, product_id = excluded.product_id, plan = excluded.plan,
			  billing_interval = excluded.billing_interval, currency_code = excluded.currency_code,
			  next_billed_at = excluded.next_billed_at, scheduled_change_action = excluded.scheduled_change_action,
			  scheduled_change_at = excluded.scheduled_change_at, paddle_updated_at = excluded.paddle_updated_at,
			  updated_at = now()
			where excluded.paddle_updated_at >= paddle_subscriptions.paddle_updated_at
			returning user_id::text`,
			s.ID, s.CustomerID, link, s.Status, s.PriceID, s.ProductID, s.Plan, s.Interval, s.Currency, s.NextBilledAt,
			s.ScheduledAction, s.ScheduledAt, s.UpdatedAt).Scan(&user)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // an older event than the row
		}
		if err != nil {
			return err
		}
		applied = true
		if user == nil {
			return nil
		}
		if _, err := tx.Exec(ctx, `update paddle_customers set user_id = coalesce(user_id, $2), updated_at = now()
			where customer_id = $1`, s.CustomerID, *user); err != nil {
			return err
		}
		return recompute(ctx, tx, *user)
	})
	if err != nil {
		return false, nil, fmt.Errorf("save subscription %s: %w", s.ID, err)
	}
	return applied, user, nil
}

// recompute sets the account's plan to its current subscription's, else Free; a comped account keeps its plan. A
// change is audited and published (see planChanged).
func recompute(ctx context.Context, tx pgx.Tx, userID string) error {
	var from, to string
	var comped bool
	if err := tx.QueryRow(ctx, `select plan, plan_comped from users where id = $1 for update`, userID).Scan(&from, &comped); err != nil {
		return err
	}
	to = from
	if !comped {
		err := tx.QueryRow(ctx, `select plan from (`+current+`) s`, userID, granting).Scan(&to)
		if errors.Is(err, pgx.ErrNoRows) {
			to = plan.Free
		} else if err != nil {
			return err
		}
	}
	if to != from {
		if _, err := tx.Exec(ctx, `update users set plan = $2 where id = $1`, userID, to); err != nil {
			return err
		}
	}
	return planChanged(ctx, tx, userID, from, to)
}

// saveCustomer mirrors c unless the row is newer; it fills in a placeholder.
func (m *model) saveCustomer(ctx context.Context, c customer) error {
	if _, err := m.db.Exec(ctx, `insert into paddle_customers (customer_id, email, paddle_updated_at) values ($1, $2, $3)
		on conflict (customer_id) do update set email = excluded.email, paddle_updated_at = excluded.paddle_updated_at,
		  updated_at = now()
		where paddle_customers.paddle_updated_at is null or excluded.paddle_updated_at >= paddle_customers.paddle_updated_at`,
		c.ID, c.Email, c.UpdatedAt); err != nil {
		return fmt.Errorf("save customer %s: %w", c.ID, err)
	}
	return nil
}

// linkCustomer makes sure the customer row exists and, when userID isn't "" and that account exists, links it to
// the account unless it's linked already.
func (m *model) linkCustomer(ctx context.Context, customerID, userID string) error {
	if _, err := m.db.Exec(ctx, `insert into paddle_customers (customer_id, user_id) values ($1, `+existingUser("$2")+`)
		on conflict (customer_id) do update set user_id = coalesce(paddle_customers.user_id, excluded.user_id), updated_at = now()`,
		customerID, userID); err != nil {
		return fmt.Errorf("link customer %s: %w", customerID, err)
	}
	return nil
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
