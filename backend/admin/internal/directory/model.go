package directory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/audit"
	"github.com/devender07/taskwell/backend/admin/internal/realtime"
	"github.com/devender07/taskwell/backend/shared/paddle"
)

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Members   int       `json:"members"`
	Projects  int       `json:"projects"`
	Issues    int       `json:"issues"`
	CreatedAt time.Time `json:"created_at"`
	Plan      string    `json:"plan"` // the owner's, and whether it's comped
	Comped    bool      `json:"comped"`
}

type User struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	Workspaces   int        `json:"workspaces"`
	CreatedAt    time.Time  `json:"created_at"`
	Suspended    bool       `json:"suspended"`
	LastActiveAt *time.Time `json:"last_active_at"`
	Plan         string     `json:"plan"`
	Comped       bool       `json:"comped"`
	CompedUntil  *time.Time `json:"comped_until"`
}

// model owns every query of the directory package.
type model struct{ db *pgxpool.Pool }

// matches is true when q is empty or a case-insensitive substring of either column (no LIKE wildcards to escape).
const matches = `($1 = '' or strpos(lower(%s), lower($1)) > 0 or strpos(lower(%s), lower($1)) > 0)`

const workspaceColumns = `w.id::text, w.name, w.slug,
	(select count(*) from workspace_members where workspace_id = w.id),
	(select count(*) from projects where workspace_id = w.id),
	(select count(*) from issues where workspace_id = w.id),
	w.created_at, coalesce(o.plan, 'free'), coalesce(o.plan_comped, false)`

// workspaceFrom joins each workspace's owner, whose plan it has.
const workspaceFrom = `workspaces w left join users o on o.id = w.created_by`

func scanWorkspace(row pgx.Row) (Workspace, error) {
	var w Workspace
	err := row.Scan(&w.ID, &w.Name, &w.Slug, &w.Members, &w.Projects, &w.Issues, &w.CreatedAt, &w.Plan, &w.Comped)
	return w, err
}

// ponytail: counts and substring search scan every row; fine for thousands, add trigram indexes past that.
func (m *model) workspaces(ctx context.Context, q string, offset int) ([]Workspace, int, error) {
	where := fmt.Sprintf(matches, "w.name", "w.slug")
	var total int
	if err := m.db.QueryRow(ctx, `select count(*) from workspaces w where `+where, q).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count workspaces: %w", err)
	}
	rows, err := m.db.Query(ctx, `select `+workspaceColumns+` from `+workspaceFrom+` where `+where+`
		order by w.created_at desc, w.id desc limit $2 offset $3`, q, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list workspaces: %w", err)
	}
	ws, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Workspace, error) { return scanWorkspace(row) })
	if err != nil {
		return nil, 0, fmt.Errorf("list workspaces: %w", err)
	}
	return ws, total, nil
}

const userColumns = `u.id::text, u.name, u.email,
	(select count(*) from workspace_members where user_id = u.id),
	u.created_at, u.suspended_at is not null,
	(select max(coalesce(used_at, created_at)) from refresh_tokens where user_id = u.id),
	u.plan, u.plan_comped, u.plan_comped_until`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Workspaces, &u.CreatedAt, &u.Suspended, &u.LastActiveAt, &u.Plan, &u.Comped, &u.CompedUntil)
	return u, err
}

func (m *model) users(ctx context.Context, q string, offset int) ([]User, int, error) {
	where := fmt.Sprintf(matches, "u.name", "u.email")
	var total int
	if err := m.db.QueryRow(ctx, `select count(*) from users u where `+where, q).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}
	rows, err := m.db.Query(ctx, `select `+userColumns+` from users u where `+where+`
		order by u.created_at desc, u.id desc limit $2 offset $3`, q, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	us, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) { return scanUser(row) })
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	return us, total, nil
}

// setSuspended suspends or unsuspends the user, by the admin, and returns them, or nil when there is no such user.
// Suspending deletes their refresh tokens and closes their sockets, so they are signed out once their access token
// expires.
// The change is recorded in the audit log with the admin's ip.
func (m *model) setSuspended(ctx context.Context, adminID, ip, id string, suspended bool) (*User, error) {
	var u *User
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			update users set suspended_at = case when $2 then coalesce(suspended_at, now()) end where id = $1`, id, suspended)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if suspended {
			if _, err := tx.Exec(ctx, `delete from refresh_tokens where user_id = $1`, id); err != nil {
				return err
			}
			if err := realtime.PublishRevoked(ctx, tx, adminID, id); err != nil {
				return err
			}
		}
		got, err := scanUser(tx.QueryRow(ctx, `select `+userColumns+` from users u where u.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		u = &got
		action := audit.UserUnsuspend
		if suspended {
			action = audit.UserSuspend
		}
		if err := audit.Record(ctx, tx, adminID, action, got.Email, ip); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, "admin.users", adminID, "/users")
	})
	if err != nil {
		return nil, fmt.Errorf("set suspended: %w", err)
	}
	return u, nil
}

// setPlan sets the user's plan and whether it's comped (for days, 0: no end), by the admin, and returns them, or nil when there's no such
// user. Clearing the comp (comped false) ignores plan: the plan becomes that of the user's most recently updated
// mirrored Paddle subscription granting access (active, trialing, past_due, as the user API's grantsAccess), else Free. It's recorded in admin_audit, and a change of plan in the audit log of every workspace they own too, like
// Paddle's.
func (m *model) setPlan(ctx context.Context, adminID, ip, id, plan string, comped bool, days int) (*User, error) {
	var u *User
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx, `select plan from users where id = $1 for update`, id).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !comped {
			if err := tx.QueryRow(ctx, `select coalesce((select plan from paddle_subscriptions
				  where user_id = $1 and status = any($2)
				  order by paddle_updated_at desc limit 1), 'free')`, id, paddle.Granting).Scan(&plan); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `update users set plan = $2, plan_comped = $3, plan_comp_reminded_at = null,
			plan_comped_until = case when $3 and $4 > 0 then now() + make_interval(days => $4) end where id = $1`,
			id, plan, comped, days); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select id::text, slug from workspaces where created_by = $1`, id)
		if err != nil {
			return err
		}
		owned := map[string]string{}
		var wsID, slug string
		if _, err := pgx.ForEachRow(rows, []any{&wsID, &slug}, func() error { owned[wsID] = slug; return nil }); err != nil {
			return err
		}
		if from != plan {
			if _, err := tx.Exec(ctx, `insert into workspace_audit (workspace_id, action, detail)
				select id, 'billing.plan_changed', $2 from workspaces where created_by = $1`,
				id, map[string]string{"from": from, "to": plan}); err != nil {
				return err
			}
		}
		got, err := scanUser(tx.QueryRow(ctx, `select `+userColumns+` from users u where u.id = $1`, id))
		if err != nil {
			return err
		}
		u = &got
		target := got.Email + ": " + plan
		switch {
		case got.CompedUntil != nil:
			target += " (comped until " + got.CompedUntil.UTC().Format(time.DateOnly) + ")"
		case comped:
			target += " (comped)"
		}
		if err := audit.Record(ctx, tx, adminID, audit.PlanSet, target, ip); err != nil {
			return err
		}
		if err := realtime.PublishBilling(ctx, tx, id, owned); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, "admin.users", adminID, "/users", "/workspaces")
	})
	if err != nil {
		return nil, fmt.Errorf("set plan: %w", err)
	}
	return u, nil
}

// UserWorkspace is a workspace the user is a member of, on their detail page.
type UserWorkspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Role    string `json:"role"`
	Owner   bool   `json:"owner"`
	Members int    `json:"members"`
	Issues  int    `json:"issues"`
}

// UserSubscription is a mirrored Paddle subscription of the user's.
type UserSubscription struct {
	SubscriptionID string     `json:"subscription_id"`
	Plan           string     `json:"plan"`
	Status         string     `json:"status"`
	Interval       string     `json:"interval"`
	Currency       string     `json:"currency"`
	UnitAmount     *int64     `json:"unit_amount"`
	NextBilledAt   *time.Time `json:"next_billed_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// UserPayment is one of the user's Paddle transactions; amounts in minor units.
type UserPayment struct {
	TransactionID string    `json:"transaction_id"`
	Currency      string    `json:"currency"`
	Total         int64     `json:"total"`
	Status        string    `json:"status"`
	BilledAt      time.Time `json:"billed_at"`
}

// PlanChange is an entry of the user's plan history: an admin_audit entry about them, or a billing.plan_changed
// entry of a workspace they own. Actor is the staff email, nil for Paddle.
type PlanChange struct {
	At     time.Time `json:"at"`
	Actor  *string   `json:"actor"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// UserEvent is one of the user's account_events.
type UserEvent struct {
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	IP        *string   `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

// UserDetail is GET /api/users/{id}.
type UserDetail struct {
	User          User               `json:"user"`
	MFA           bool               `json:"mfa"`
	Workspaces    []UserWorkspace    `json:"workspaces"`
	Subscriptions []UserSubscription `json:"subscriptions"`
	Payments      []UserPayment      `json:"payments"`
	PlanHistory   []PlanChange       `json:"plan_history"`
	Events        []UserEvent        `json:"events"`
}

const detailPage = 50 // payments, plan history and events: the newest this many

// collect runs a list query with args, naming it in the error; never nil.
func collect[T any](ctx context.Context, db *pgxpool.Pool, what, sql string, scan func(pgx.CollectableRow) (T, error), args ...any) ([]T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	list, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if list == nil {
		list = []T{}
	}
	return list, nil
}

// user returns the user's detail page, or nil when there's no such user.
func (m *model) user(ctx context.Context, id string) (*UserDetail, error) {
	var d UserDetail
	var err error
	d.User, err = scanUser(m.db.QueryRow(ctx, `select `+userColumns+` from users u where u.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	if err := m.db.QueryRow(ctx, `select exists (select 1 from user_mfa where user_id = $1 and enabled_at is not null)`, id).
		Scan(&d.MFA); err != nil {
		return nil, fmt.Errorf("read two-factor: %w", err)
	}
	if d.Workspaces, err = collect(ctx, m.db, "user workspaces", `
		select w.id::text, w.name, w.slug, r.name, coalesce(w.created_by = m.user_id, false),
		       (select count(*) from workspace_members where workspace_id = w.id),
		       (select count(*) from issues where workspace_id = w.id)
		from workspace_members m join workspaces w on w.id = m.workspace_id join workspace_roles r on r.id = m.role_id
		where m.user_id = $1 order by w.name, w.id`,
		func(row pgx.CollectableRow) (UserWorkspace, error) {
			var v UserWorkspace
			err := row.Scan(&v.ID, &v.Name, &v.Slug, &v.Role, &v.Owner, &v.Members, &v.Issues)
			return v, err
		}, id); err != nil {
		return nil, err
	}
	if d.Subscriptions, err = collect(ctx, m.db, "user subscriptions", `
		select subscription_id, plan, status, coalesce(billing_interval, ''), coalesce(currency_code, ''), unit_amount,
		       next_billed_at, updated_at
		from paddle_subscriptions where user_id = $1 order by updated_at desc`,
		func(row pgx.CollectableRow) (UserSubscription, error) {
			var v UserSubscription
			err := row.Scan(&v.SubscriptionID, &v.Plan, &v.Status, &v.Interval, &v.Currency, &v.UnitAmount, &v.NextBilledAt, &v.UpdatedAt)
			return v, err
		}, id); err != nil {
		return nil, err
	}
	if d.Payments, err = collect(ctx, m.db, "user payments", `
		select transaction_id, currency_code, total, status, billed_at
		from paddle_transactions where user_id = $1 order by billed_at desc limit $2`,
		func(row pgx.CollectableRow) (UserPayment, error) {
			var v UserPayment
			err := row.Scan(&v.TransactionID, &v.Currency, &v.Total, &v.Status, &v.BilledAt)
			return v, err
		}, id, detailPage); err != nil {
		return nil, err
	}
	// An admin's plan change writes admin_audit and every owned workspace's audit in one transaction (the same now()),
	// so one entry per time stays: the admin_audit one, which names the staff member.
	if d.PlanHistory, err = collect(ctx, m.db, "user plan history", `
		select at, actor, action, detail from (
		  select distinct on (at) at, actor, action, detail from (
		    select at, nullif(actor_email, '') as actor, action, target as detail, 0 as src from admin_audit
		    where action like 'user.%' and (target = $2 or starts_with(target, $2 || ': '))
		    union all
		    select a.at, null, a.action, concat(a.detail->>'from', ' → ', a.detail->>'to'), 1
		    from workspace_audit a join workspaces w on w.id = a.workspace_id
		    where w.created_by = $1 and a.action = 'billing.plan_changed'
		  ) x order by at, src
		) y order by at desc limit $3`,
		func(row pgx.CollectableRow) (PlanChange, error) {
			var v PlanChange
			err := row.Scan(&v.At, &v.Actor, &v.Action, &v.Detail)
			return v, err
		}, id, d.User.Email, detailPage); err != nil {
		return nil, err
	}
	if d.Events, err = collect(ctx, m.db, "user events", `
		select action, detail, host(ip), user_agent, at from account_events where user_id = $1 order by id desc limit $2`,
		func(row pgx.CollectableRow) (UserEvent, error) {
			var v UserEvent
			err := row.Scan(&v.Action, &v.Detail, &v.IP, &v.UserAgent, &v.CreatedAt)
			return v, err
		}, id, detailPage); err != nil {
		return nil, err
	}
	return &d, nil
}

// UserCSV is a row of GET /api/users.csv.
type UserCSV struct {
	User
	MFA bool
}

// ponytail: every matching user in memory; stream the rows if the user count reaches the hundreds of thousands.
func (m *model) allUsers(ctx context.Context, q string) ([]UserCSV, error) {
	return collect(ctx, m.db, "export users", `select `+userColumns+`,
		exists (select 1 from user_mfa where user_id = u.id and enabled_at is not null)
		from users u where `+fmt.Sprintf(matches, "u.name", "u.email")+` order by u.created_at desc, u.id desc`,
		func(row pgx.CollectableRow) (UserCSV, error) {
			var u UserCSV
			err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Workspaces, &u.CreatedAt, &u.Suspended, &u.LastActiveAt, &u.Plan,
				&u.Comped, &u.CompedUntil, &u.MFA)
			return u, err
		}, q)
}
