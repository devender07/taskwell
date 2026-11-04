package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/audit"
	"github.com/devender07/taskwell/backend/shared/totp"
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

// Enrolment is a new TOTP factor, printed once by the CLI: the setup key, the otpauth:// URL and the recovery codes.
type Enrolment struct {
	Secret, URL   string
	RecoveryCodes []string
}

// Promote makes the user with this email a platform admin and, when they have no second factor yet, enrols one; it
// records both in the audit log. The enrolment is nil when they had a factor already, and the user is nil when
// there is no such user or their email isn't verified (anyone can sign up with any address).
func Promote(ctx context.Context, db *pgxpool.Pool, key []byte, email string) (*User, *Enrolment, error) {
	return byEmailCLI(ctx, db, key, email, audit.Promote, false, `
		update users set platform_admin = true where email = lower(trim($1)) and email_verified_at is not null
		returning id::text, name, email`)
}

// ResetMFA replaces the platform admin's second factor with a new one and ends their admin sign-ins, and records it
// in the audit log. The user is nil when there is no platform admin with this email.
func ResetMFA(ctx context.Context, db *pgxpool.Pool, key []byte, email string) (*User, *Enrolment, error) {
	return byEmailCLI(ctx, db, key, email, audit.MFAReset, true, `
		with u as (select id, name, email from users where email = lower(trim($1)) and platform_admin),
		     s as (delete from admin_sessions where user_id in (select id from u))
		select id::text, name, email from u`)
}

// byEmailCLI runs a CLI command's query (taking the email, returning the user), enrols a new factor (replacing one
// only when replace is set) and audits it, in one transaction.
func byEmailCLI(ctx context.Context, db *pgxpool.Pool, key []byte, email, action string, replace bool, query string) (*User, *Enrolment, error) {
	var u *User
	var e *Enrolment
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
		if err := audit.Record(ctx, tx, "", action, got.Email, "cli"); err != nil {
			return err
		}
		secret := totp.NewSecret()
		sealed, err := totp.Seal(key, got.ID, secret)
		if err != nil {
			return err
		}
		codes, hashes := totp.NewRecoveryCodes()
		onConflict := `do nothing`
		if replace {
			onConflict = `do update set secret = excluded.secret, enabled_at = now(), last_step = 0,
			  recovery_codes = excluded.recovery_codes`
		}
		tag, err := tx.Exec(ctx, `insert into admin_mfa (user_id, secret, enabled_at, recovery_codes) values ($1, $2, now(), $3)
			on conflict (user_id) `+onConflict, got.ID, sealed, hashes)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		e = &Enrolment{Secret: secret, URL: totp.URL("Taskwell admin", got.Email, secret), RecoveryCodes: codes}
		return audit.Record(ctx, tx, "", audit.MFAEnrolled, got.Email, "cli")
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", action, err)
	}
	return u, e, nil
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

// hasMFA reports whether the user has a second factor (enrolled from the CLI).
func (m *model) hasMFA(ctx context.Context, userID string) (bool, error) {
	var ok bool
	if err := m.db.QueryRow(ctx, `select exists(select 1 from admin_mfa where user_id = $1 and enabled_at is not null)`,
		userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("check mfa: %w", err)
	}
	return ok, nil
}

// mfaResult is what checking a second factor found.
type mfaResult struct {
	ok           bool
	usedRecovery bool // a recovery code (now spent) was used
}

// checkMFA checks a TOTP code, or else a recovery code, against the user's factor. A TOTP match is remembered so it
// can't be replayed.
func (m *model) checkMFA(ctx context.Context, key []byte, userID, code, recovery string, now time.Time) (mfaResult, error) {
	var res mfaResult
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var sealed string
		var lastStep int64
		var hashes []string
		err := tx.QueryRow(ctx, `select secret, last_step, recovery_codes from admin_mfa
			where user_id = $1 and enabled_at is not null for update`, userID).Scan(&sealed, &lastStep, &hashes)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if code == "" {
			h := totp.HashRecovery(recovery)
			if !slices.Contains(hashes, h) {
				return nil
			}
			res.ok, res.usedRecovery = true, true
			_, err := tx.Exec(ctx, `update admin_mfa set recovery_codes = array_remove(recovery_codes, $2) where user_id = $1`, userID, h)
			return err
		}
		secret, err := totp.Unseal(key, userID, sealed)
		if err != nil {
			return err
		}
		step, ok, err := totp.Check(secret, code, now, lastStep)
		if err != nil || !ok {
			return err
		}
		res.ok = true
		_, err = tx.Exec(ctx, `update admin_mfa set last_step = $2 where user_id = $1`, userID, step)
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
