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
	Promote       = "admin.promote"   // from the CLI; target: the email
	MFAReset      = "admin.mfa_reset" // from the CLI; target: the email
	SignIn        = "signin"
	SignInFailed  = "signin.failed"     // wrong password; target: the email tried
	MFAFailed     = "signin.mfa_failed" // wrong code after the right password
	MFAEnrolled   = "mfa.enrolled"
	RecoveryUsed  = "mfa.recovery_used"
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
		values ($1, coalesce((select email from users where id = $1), ''), $2, $3, $4)`,
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

// list returns up to limit entries older than before (an entry id; "" for the newest), newest first.
func (m *model) list(ctx context.Context, before string, limit int) ([]Entry, error) {
	var cursor *string
	if before != "" {
		cursor = &before
	}
	rows, err := m.db.Query(ctx, `
		select id::text, at, actor_id::text, actor_email, action, target, ip from admin_audit
		where $1::uuid is null or id < $1::uuid
		order by id desc limit $2`, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := row.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorEmail, &e.Action, &e.Target, &e.IP)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	return out, nil
}
