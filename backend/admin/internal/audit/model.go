package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Actions recorded in admin_audit.
const (
	ConfigUpdate  = "config.update" // target: the key (never the value)
	UserSuspend   = "user.suspend"  // target: the user's email
	UserUnsuspend = "user.unsuspend"
	Promote       = "admin.promote"   // no longer recorded (admins have their own accounts); kept for old rows
	MFAReset      = "admin.mfa_reset" // from the CLI; target: the email
	AdminCreate   = "admin.create"    // target: the new admin's email
	AdminRemove   = "admin.remove"    // target: the removed admin's email
	SignIn        = "signin"
	SignInFailed  = "signin.failed"     // wrong password; target: the email tried
	MFAFailed     = "signin.mfa_failed" // wrong code after the right password
	MFAEnrolled   = "mfa.enrolled"
	RecoveryUsed  = "mfa.recovery_used"

	PlanSet = "user.plan_set" // target: "<email>: <plan>", plus " (comped)" or " (comped until 2026-11-24)"

	UsersExported    = "users.exported" // target: the search, "" for every user
	PaymentsExported = "payments.exported"
)

// Execer is a pool or a transaction: pass the transaction that makes the change so the record commits with it.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Record appends an entry. actorID may be "" (the CLI, or a failed sign-in); the actor's email is copied in, so the
// entry still reads right after the user is gone.
func Record(ctx context.Context, q Execer, actorID, action, target, ip string) error {
	var actor *string
	if actorID != "" {
		actor = &actorID
	}
	if _, err := q.Exec(ctx, `
		insert into admin_audit (actor_id, actor_email, action, target, ip)
		values ($1, coalesce((select email from admins where id = $1), ''), $2, $3, $4)`,
		actor, action, target, ip); err != nil {
		return fmt.Errorf("record audit %s: %w", action, err)
	}
	return nil
}

// Entry is one admin_audit row as GET /api/audit returns it.
type Entry struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	ActorID    *string   `json:"actor_id"`
	ActorEmail string    `json:"actor_email"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	IP         string    `json:"ip"`
}

type model struct{ db *pgxpool.Pool }

// list returns limit entries from offset, newest first, and how many there are in all.
func (m *model) list(ctx context.Context, offset, limit int) ([]Entry, int, error) {
	var total int
	if err := m.db.QueryRow(ctx, `select count(*) from admin_audit`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit: %w", err)
	}
	rows, err := m.db.Query(ctx, `
		select id::text, at, actor_id::text, actor_email, action, target, ip from admin_audit
		order by id desc limit $1 offset $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := row.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorEmail, &e.Action, &e.Target, &e.IP)
		return e, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list audit: %w", err)
	}
	return out, total, nil
}
