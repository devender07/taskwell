package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/audit"
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// model owns every query of the admin auth package.
type model struct{ db *pgxpool.Pool }

// adminWithPassword returns the unsuspended platform admin with this (normalized) email and their password hash;
// the hash is nil when there is no such admin or they have no password.
func (m *model) adminWithPassword(ctx context.Context, email string) (User, *string, error) {
	var u User
	var hash *string
	err := m.db.QueryRow(ctx, `
		select id::text, name, email, password_hash from users
		where email = $1 and platform_admin and suspended_at is null`, email).Scan(&u.ID, &u.Name, &u.Email, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, nil
	}
	if err != nil {
		return User{}, nil, fmt.Errorf("find admin: %w", err)
	}
	return u, hash, nil
}

// admin returns the unsuspended platform admin with this ID, or nil.
func (m *model) admin(ctx context.Context, id string) (*User, error) {
	var u User
	err := m.db.QueryRow(ctx, `
		select id::text, name, email from users
		where id = $1 and platform_admin and suspended_at is null`, id).Scan(&u.ID, &u.Name, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find admin: %w", err)
	}
	return &u, nil
}

func (m *model) setting(ctx context.Context, key string) (string, error) {
	var v string
	err := m.db.QueryRow(ctx, `select value from system_config where key = $1`, key).Scan(&v)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read config %s: %w", key, err)
	}
	return v, nil
}

// failures counts key's failed sign-ins in the last failWindow (the user service's auth_failures table).
func (m *model) failures(ctx context.Context, key string) (int, error) {
	var n int
	if err := m.db.QueryRow(ctx, `select count(*) from auth_failures where key = $1 and at > now() - make_interval(secs => $2)`,
		key, failWindow.Seconds()).Scan(&n); err != nil {
		return 0, fmt.Errorf("count failures: %w", err)
	}
	return n, nil
}

// fail records a failed sign-in for key and drops every record older than failWindow.
func (m *model) fail(ctx context.Context, key string) error {
	if _, err := m.db.Exec(ctx, `
		with gone as (delete from auth_failures where at < now() - make_interval(secs => $2))
		insert into auth_failures (key) values ($1)`, key, failWindow.Seconds()); err != nil {
		return fmt.Errorf("record failure: %w", err)
	}
	return nil
}

// Promote makes the user with this email a platform admin and records it in the audit log. It returns nil when there
// is no such user or their email isn't verified (anyone can sign up with any address).
func Promote(ctx context.Context, db *pgxpool.Pool, email string) (*User, error) {
	return byEmailCLI(ctx, db, email, audit.Promote, `
		update users set platform_admin = true where email = lower(trim($1)) and email_verified_at is not null
		returning id::text, name, email`)
}

// ResetMFA removes the platform admin's second factor and ends their admin sign-ins, so their next sign-in enrols
// again, and records it in the audit log. It returns nil when there is no platform admin with this email.
func ResetMFA(ctx context.Context, db *pgxpool.Pool, email string) (*User, error) {
	return byEmailCLI(ctx, db, email, audit.MFAReset, `
		with u as (select id, name, email from users where email = lower(trim($1)) and platform_admin),
		     m as (delete from admin_mfa where user_id in (select id from u)),
		     s as (delete from admin_sessions where user_id in (select id from u))
		select id::text, name, email from u`)
}

// byEmailCLI runs a CLI command's query (taking the email, returning the user) and audits it, in one transaction.
func byEmailCLI(ctx context.Context, db *pgxpool.Pool, email, action, query string) (*User, error) {
	var u *User
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var got User
		err := tx.QueryRow(ctx, query, email).Scan(&got.ID, &got.Name, &got.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		u = &got
		return audit.Record(ctx, tx, "", action, got.Email, "cli")
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return u, nil
}

// --- sessions ---

// startSession creates a sign-in of the user lasting d, past MFA when mfaDone, and returns its id. Expired rows of
// anyone are dropped on the way.
func (m *model) startSession(ctx context.Context, userID string, mfaDone bool, d time.Duration) (string, error) {
	var id string
	err := m.db.QueryRow(ctx, `
		with gone as (delete from admin_sessions where expires_at < now())
		insert into admin_sessions (user_id, mfa_at, expires_at)
		values ($1, case when $2 then now() end, now() + make_interval(secs => $3))
		returning id::text`, userID, mfaDone, d.Seconds()).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("start admin session: %w", err)
	}
	return id, nil
}

// touch reports whether sid is a live, MFA-passed sign-in of the user (an unsuspended platform admin) used within
// idle, and marks it used now.
func (m *model) touch(ctx context.Context, sid, userID string, idle time.Duration) (bool, error) {
	tag, err := m.db.Exec(ctx, `
		update admin_sessions s set last_seen_at = now()
		from users u
		where s.id = $1 and s.user_id = $2 and u.id = s.user_id and u.platform_admin and u.suspended_at is null
		  and s.mfa_at is not null and s.expires_at > now() and s.last_seen_at > now() - make_interval(secs => $3)`,
		sid, userID, idle.Seconds())
	if err != nil {
		return false, fmt.Errorf("check admin session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// pending reports whether sid is a live sign-in of the user still waiting for its second factor.
func (m *model) pending(ctx context.Context, sid, userID string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `
		select exists(select 1 from admin_sessions s join users u on u.id = s.user_id
		              where s.id = $1 and s.user_id = $2 and s.mfa_at is null and s.expires_at > now()
		                and u.platform_admin and u.suspended_at is null)`, sid, userID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check pending admin session: %w", err)
	}
	return ok, nil
}

// endSession deletes the sign-in; nothing happens when it's gone already.
func (m *model) endSession(ctx context.Context, sid string) error {
	if _, err := m.db.Exec(ctx, `delete from admin_sessions where id = $1`, sid); err != nil {
		return fmt.Errorf("end admin session: %w", err)
	}
	return nil
}

// --- MFA ---

// enrolment returns the user's TOTP secret while enrolment is unconfirmed, replacing it with secret (each sign-in
// shows a fresh one until a code confirms it); "" once MFA is on.
func (m *model) enrolment(ctx context.Context, userID, secret string) (string, error) {
	var got string
	err := m.db.QueryRow(ctx, `
		insert into admin_mfa (user_id, secret) values ($1, $2)
		on conflict (user_id) do update set secret = excluded.secret where admin_mfa.enabled_at is null
		returning secret`, userID, secret).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("enrol mfa: %w", err)
	}
	return got, nil
}

// mfaResult is what checking a second factor found.
type mfaResult struct {
	ok           bool
	enrolled     bool     // the code confirmed enrolment just now
	recovery     []string // the new recovery codes, when enrolled
	usedRecovery bool     // a recovery code (now spent) was used
}

// checkMFA checks a TOTP code, or else a recovery code (only once MFA is on), against the user's factor. A TOTP match
// is remembered so it can't be replayed, and confirms a pending enrolment, which issues recovery codes.
func (m *model) checkMFA(ctx context.Context, userID, code, recovery string, now time.Time) (mfaResult, error) {
	var res mfaResult
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var secret string
		var enabled bool
		var lastStep int64
		var hashes []string
		err := tx.QueryRow(ctx, `select secret, enabled_at is not null, last_step, recovery_codes from admin_mfa
			where user_id = $1 for update`, userID).Scan(&secret, &enabled, &lastStep, &hashes)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if code == "" {
			h := hashRecovery(recovery)
			if !enabled || !slices.Contains(hashes, h) {
				return nil
			}
			res.ok, res.usedRecovery = true, true
			_, err := tx.Exec(ctx, `update admin_mfa set recovery_codes = array_remove(recovery_codes, $2) where user_id = $1`, userID, h)
			return err
		}
		step, ok, err := checkTOTP(secret, code, now, lastStep)
		if err != nil || !ok {
			return err
		}
		res.ok = true
		if !enabled {
			res.enrolled = true
			res.recovery, hashes = newRecoveryCodes()
		}
		_, err = tx.Exec(ctx, `update admin_mfa set last_step = $2, enabled_at = coalesce(enabled_at, now()), recovery_codes = $3
			where user_id = $1`, userID, step, hashes)
		return err
	})
	if err != nil {
		return mfaResult{}, fmt.Errorf("check mfa: %w", err)
	}
	return res, nil
}

// finishSignin replaces the pending sign-in sid with a full one lasting d and returns the new id; "" when sid is no
// longer pending.
func (m *model) finishSignin(ctx context.Context, sid, userID string, d time.Duration) (string, error) {
	var id string
	err := m.db.QueryRow(ctx, `
		with p as (delete from admin_sessions where id = $1 and user_id = $2 and mfa_at is null and expires_at > now()
		           returning user_id)
		insert into admin_sessions (user_id, mfa_at, expires_at)
		select user_id, now(), now() + make_interval(secs => $3) from p
		returning id::text`, sid, userID, d.Seconds()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("finish admin sign-in: %w", err)
	}
	return id, nil
}
