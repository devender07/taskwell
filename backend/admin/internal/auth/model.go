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
	"github.com/devender07/taskwell/backend/admin/internal/realtime"
	"github.com/devender07/taskwell/backend/shared/totp"
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// model owns every query of the admin auth package.
type model struct{ db *pgxpool.Pool }

// adminWithPassword returns the admin with this (normalized) email and their password hash; the hash is nil when
// there is no such admin.
func (m *model) adminWithPassword(ctx context.Context, email string) (User, *string, error) {
	var u User
	var hash *string
	err := m.db.QueryRow(ctx, `
		select id::text, name, email, password_hash from admins where email = $1`, email).Scan(&u.ID, &u.Name, &u.Email, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, nil
	}
	if err != nil {
		return User{}, nil, fmt.Errorf("find admin: %w", err)
	}
	return u, hash, nil
}

// admin returns the admin with this ID, or nil.
func (m *model) admin(ctx context.Context, id string) (*User, error) {
	var u User
	err := m.db.QueryRow(ctx, `
		select id::text, name, email from admins where id = $1`, id).Scan(&u.ID, &u.Name, &u.Email)
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

// Enrolment is a new TOTP factor, shown once (by the CLI or to the admin who added the account): the setup key, the otpauth:// URL and the recovery codes.
type Enrolment struct {
	Secret        string   `json:"secret"`
	URL           string   `json:"url"`
	RecoveryCodes []string `json:"recovery_codes"`
}

// EnrolMFA enrols a second factor for the admin with this email when they have none yet, and records it in the
// audit log. The enrolment is nil when they had a factor already, and the admin is nil when there is no such admin.
func EnrolMFA(ctx context.Context, db *pgxpool.Pool, key []byte, email string) (*User, *Enrolment, error) {
	return byEmailCLI(ctx, db, key, email, "", false, `select id::text, name, email from admins where email = lower(trim($1))`)
}

// ResetMFA replaces the admin's second factor with a new one and ends their admin sign-ins, and records it
// in the audit log. The admin is nil when there is no admin with this email.
func ResetMFA(ctx context.Context, db *pgxpool.Pool, key []byte, email string) (*User, *Enrolment, error) {
	return byEmailCLI(ctx, db, key, email, audit.MFAReset, true, `
		with u as (select id, name, email from admins where email = lower(trim($1))),
		     s as (delete from admin_sessions where admin_id in (select id from u))
		select id::text, name, email from u`)
}

// byEmailCLI runs a CLI command's query (taking the email, returning the admin), enrols a new factor (replacing one
// only when replace is set) and audits it (action, when set, and the enrolment), in one transaction.
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
		if action != "" {
			if err := audit.Record(ctx, tx, "", action, got.Email, "cli"); err != nil {
				return err
			}
		}
		if e, err = enrol(ctx, tx, key, got.ID, got.Email, replace); err != nil || e == nil {
			return err
		}
		return audit.Record(ctx, tx, "", audit.MFAEnrolled, got.Email, "cli")
	})
	if err != nil {
		return nil, nil, fmt.Errorf("enrol mfa: %w", err)
	}
	return u, e, nil
}

// CurrentCode returns the 6-digit code the admin's authenticator app shows now, for local dev sign-in without a
// phone; "" when there is no such admin or they have no factor.
func CurrentCode(ctx context.Context, db *pgxpool.Pool, key []byte, email string, now time.Time) (string, error) {
	var id, sealed string
	err := db.QueryRow(ctx, `select a.id::text, f.secret from admins a join admin_mfa f on f.admin_id = a.id
		where a.email = lower(trim($1)) and f.enabled_at is not null`, email).Scan(&id, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read mfa: %w", err)
	}
	secret, err := totp.Unseal(key, id, sealed)
	if err != nil {
		return "", err
	}
	return totp.Code(secret, now.Unix()/30)
}

// enrol gives the admin a new TOTP factor in tx, replacing theirs only when replace is set; nil when they kept theirs.
func enrol(ctx context.Context, tx pgx.Tx, key []byte, id, email string, replace bool) (*Enrolment, error) {
	secret := totp.NewSecret()
	sealed, err := totp.Seal(key, id, secret)
	if err != nil {
		return nil, err
	}
	codes, hashes := totp.NewRecoveryCodes()
	onConflict := `do nothing`
	if replace {
		onConflict = `do update set secret = excluded.secret, enabled_at = now(), last_step = 0,
		  recovery_codes = excluded.recovery_codes`
	}
	tag, err := tx.Exec(ctx, `insert into admin_mfa (admin_id, secret, enabled_at, recovery_codes) values ($1, $2, now(), $3)
		on conflict (admin_id) `+onConflict, id, sealed, hashes)
	if err != nil || tag.RowsAffected() == 0 {
		return nil, err
	}
	return &Enrolment{Secret: secret, URL: totp.URL("Taskwell admin", email, secret), RecoveryCodes: codes}, nil
}

// --- admin accounts ---

// Account is an admins row as GET /api/admins lists it.
type Account struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Email      string     `json:"email"`
	CreatedAt  time.Time  `json:"created_at"`
	MFA        bool       `json:"mfa"`
	LastSeenAt *time.Time `json:"last_seen_at"`
}

const accountColumns = `a.id::text, a.name, a.email, a.created_at,
	exists(select 1 from admin_mfa f where f.admin_id = a.id and f.enabled_at is not null),
	(select max(s.last_seen_at) from admin_sessions s where s.admin_id = a.id and s.mfa_at is not null)`

func scanAccount(row pgx.Row) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.Name, &a.Email, &a.CreatedAt, &a.MFA, &a.LastSeenAt)
	return a, err
}

// accounts returns every admin, oldest first.
func (m *model) accounts(ctx context.Context) ([]Account, error) {
	rows, err := m.db.Query(ctx, `select `+accountColumns+` from admins a order by a.created_at, a.id`)
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Account, error) { return scanAccount(row) })
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	return out, nil
}

// passwordHash returns the admin's password hash, "" when there is no such admin.
func (m *model) passwordHash(ctx context.Context, id string) (string, error) {
	var h string
	err := m.db.QueryRow(ctx, `select password_hash from admins where id = $1`, id).Scan(&h)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read admin password: %w", err)
	}
	return h, nil
}

// createAccount adds an admin with their TOTP factor and audits it, in one transaction. The account is nil when the
// email is taken.
func (m *model) createAccount(ctx context.Context, key []byte, actorID, ip, name, email, hash string) (*Account, *Enrolment, error) {
	var a *Account
	var e *Enrolment
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `insert into admins (name, email, password_hash) values ($1, $2, $3)
			on conflict (email) do nothing returning id::text`, name, email, hash).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if e, err = enrol(ctx, tx, key, id, email, false); err != nil {
			return err
		}
		got, err := scanAccount(tx.QueryRow(ctx, `select `+accountColumns+` from admins a where a.id = $1`, id))
		if err != nil {
			return err
		}
		a = &got
		if err := audit.Record(ctx, tx, actorID, audit.AdminCreate, email, ip); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, "admin.admins", actorID, "/admins")
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create admin: %w", err)
	}
	return a, e, nil
}

// removeAccount deletes the admin (their sessions and factor go with them) and audits it; false when there is none.
func (m *model) removeAccount(ctx context.Context, actorID, ip, id string) (bool, error) {
	var found bool
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var email string
		err := tx.QueryRow(ctx, `delete from admins where id = $1 returning email`, id).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if err := audit.Record(ctx, tx, actorID, audit.AdminRemove, email, ip); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, "admin.admins", actorID, "/admins")
	})
	if err != nil {
		return false, fmt.Errorf("remove admin: %w", err)
	}
	return found, nil
}

// --- sessions ---

// startSession creates a sign-in of the user lasting d, past MFA when mfaDone, and returns its id. Expired rows of
// anyone are dropped on the way.
func (m *model) startSession(ctx context.Context, userID string, mfaDone bool, d time.Duration) (string, error) {
	var id string
	err := m.db.QueryRow(ctx, `
		with gone as (delete from admin_sessions where expires_at < now())
		insert into admin_sessions (admin_id, mfa_at, expires_at)
		values ($1, case when $2 then now() end, now() + make_interval(secs => $3))
		returning id::text`, userID, mfaDone, d.Seconds()).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("start admin session: %w", err)
	}
	return id, nil
}

// touch reports whether sid is a live, MFA-passed sign-in of the admin used within idle, and marks it used now.
func (m *model) touch(ctx context.Context, sid, userID string, idle time.Duration) (bool, error) {
	tag, err := m.db.Exec(ctx, `
		update admin_sessions s set last_seen_at = now()
		where s.id = $1 and s.admin_id = $2
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
		select exists(select 1 from admin_sessions
		              where id = $1 and admin_id = $2 and mfa_at is null and expires_at > now())`, sid, userID).Scan(&ok)
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
	if err := m.db.QueryRow(ctx, `select exists(select 1 from admin_mfa where admin_id = $1 and enabled_at is not null)`,
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
			where admin_id = $1 and enabled_at is not null for update`, userID).Scan(&sealed, &lastStep, &hashes)
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
			_, err := tx.Exec(ctx, `update admin_mfa set recovery_codes = array_remove(recovery_codes, $2) where admin_id = $1`, userID, h)
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
		_, err = tx.Exec(ctx, `update admin_mfa set last_step = $2 where admin_id = $1`, userID, step)
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
		with p as (delete from admin_sessions where id = $1 and admin_id = $2 and mfa_at is null and expires_at > now()
		           returning admin_id)
		insert into admin_sessions (admin_id, mfa_at, expires_at)
		select admin_id, now(), now() + make_interval(secs => $3) from p
		returning id::text`, sid, userID, d.Seconds()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("finish admin sign-in: %w", err)
	}
	return id, nil
}
