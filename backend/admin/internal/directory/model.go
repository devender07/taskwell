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
	u.plan, u.plan_comped`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Workspaces, &u.CreatedAt, &u.Suspended, &u.LastActiveAt, &u.Plan, &u.Comped)
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

// setPlan sets the user's plan and whether it's comped, by the admin, and returns them, or nil when there's no such
// user. Clearing the comp (comped false) ignores plan: the plan becomes that of the user's most recently updated
// mirrored Paddle subscription granting access (active, trialing, past_due, as the user API's grantsAccess), else Free. It's recorded in admin_audit, and a change of plan in the audit log of every workspace they own too, like
// Paddle's.
func (m *model) setPlan(ctx context.Context, adminID, ip, id, plan string, comped bool) (*User, error) {
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
				  where user_id = $1 and status in ('active', 'trialing', 'past_due')
				  order by paddle_updated_at desc limit 1), 'free')`, id).Scan(&plan); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `update users set plan = $2, plan_comped = $3 where id = $1`, id, plan, comped); err != nil {
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
		if comped {
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
