package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/realtime"
)

const (
	maxAttempts = 5

	// reuseGrace lets a just-rotated token refresh again (two tabs at once, or a page that navigated away before the
	// rotated cookies arrived) instead of looking like theft.
	reuseGrace = 10 * time.Second

	purposeCode   = "signin_code"
	purposeReset  = "password_reset"
	purposeVerify = "email_verify"
	// purposePasswordSet is the code a signed-in account without a password needs to add one.
	purposePasswordSet = "password_set"
)

var errEmailTaken = errors.New("email already registered")

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// family is one sign-in: every refresh token rotated from it shares the id, the remember choice and the expiry.
type family struct {
	id        string // "" for a new sign-in
	remember  bool
	expiresAt time.Time
}

// model owns every query for users, sign-in codes, reset links and refresh tokens.
type model struct{ db *pgxpool.Pool }

// --- users ---

// startSignup records a sign-up awaiting its emailed code: it creates the unverified user without a password (or
// keeps the existing unverified one) and stores the code's hash with the name and password hash to apply once it is
// verified. Older pending codes stay usable: each is bound to the browser that asked for it. Returns errEmailTaken
// when a verified account has the email.
func (m *model) startSignup(ctx context.Context, name, email, passwordHash string, tokenHash []byte, minutes int) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var userID string
		var verified, inserted bool
		if err := tx.QueryRow(ctx, `
			insert into users (name, email) values ($1, $2)
			on conflict (email) do update set email = excluded.email
			returning id::text, email_verified_at is not null, xmax = 0`, name, email).Scan(&userID, &verified, &inserted); err != nil {
			return err
		}
		if verified {
			return errEmailTaken
		}
		if _, err := tx.Exec(ctx, `
			insert into auth_tokens (user_id, purpose, token_hash, expires_at, name, password_hash)
			values ($1, $2, $3, now() + make_interval(mins => $4), $5, $6)`,
			userID, purposeVerify, tokenHash, minutes, name, passwordHash); err != nil {
			return err
		}
		if inserted {
			return realtime.Publish(ctx, tx, realtime.AdminUsers(userID))
		}
		return nil
	})
	if errors.Is(err, errEmailTaken) {
		return err
	}
	if err != nil {
		return fmt.Errorf("start signup: %w", err)
	}
	return nil
}

// verifySignup uses the live sign-up code with tokenHash for the unverified account with email: it verifies the
// account, applies the code's name and password and voids its other codes. nil when there's no such code.
func (m *model) verifySignup(ctx context.Context, email string, tokenHash []byte) (*User, error) {
	var u *User
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var tokenID, userID, name, ph string
		err := tx.QueryRow(ctx, `
			select t.id::text, u.id::text, t.name, t.password_hash
			from auth_tokens t join users u on u.id = t.user_id
			where u.email = $1 and u.email_verified_at is null and t.purpose = $2 and t.token_hash = $3
			  and t.used_at is null and t.expires_at > now()
			for update of t, u`, email, purposeVerify, tokenHash).Scan(&tokenID, &userID, &name, &ph)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find signup code: %w", err)
		}
		var got User
		if err := tx.QueryRow(ctx, `
			update users set name = $2, password_hash = $3, email_verified_at = now() where id = $1
			returning id::text, name, email`, userID, name, ph).Scan(&got.ID, &got.Name, &got.Email); err != nil {
			return fmt.Errorf("verify user: %w", err)
		}
		if err := voidVerify(ctx, tx, userID); err != nil {
			return err
		}
		u = &got
		return realtime.Publish(ctx, tx, realtime.AdminUsers(userID))
	})
	return u, err
}

// markVerified records that u proved they own their email (an email code, a reset link or Google), and voids pending
// sign-up codes so a sign-up someone else started can't complete. first reports whether it wasn't verified: then
// whoever signed up chose the name, so it becomes name, or the email's local part when that's "", and u gets it.
func markVerified(ctx context.Context, tx pgx.Tx, u *User, name string) (first bool, err error) {
	err = tx.QueryRow(ctx, `
		update users set email_verified_at = now(), name = coalesce(nullif($2, ''), split_part(email, '@', 1))
		where id = $1 and email_verified_at is null returning name`, u.ID, name).Scan(&u.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verify email: %w", err)
	}
	return true, voidVerify(ctx, tx, u.ID)
}

func voidVerify(ctx context.Context, tx pgx.Tx, userID string) error {
	if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where user_id = $1 and purpose = $2 and used_at is null`,
		userID, purposeVerify); err != nil {
		return fmt.Errorf("void signup codes: %w", err)
	}
	return nil
}

// alive reports whether the sign-in (refresh token family) sid of the user still exists and the user isn't suspended.
func (m *model) alive(ctx context.Context, userID, sid string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `
		select exists(select 1 from refresh_tokens t join users u on u.id = t.user_id
		              where t.family_id = $1::uuid and t.user_id = $2::uuid and t.expires_at > now() and u.suspended_at is null)`,
		sid, userID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check session: %w", err)
	}
	return ok, nil
}

// --- failed attempts ---

// failures counts key's failed attempts in the last failWindow.
func (m *model) failures(ctx context.Context, key string) (int, error) {
	var n int
	if err := m.db.QueryRow(ctx, `select count(*) from auth_failures where key = $1 and at > now() - make_interval(secs => $2)`,
		key, failWindow.Seconds()).Scan(&n); err != nil {
		return 0, fmt.Errorf("count failures: %w", err)
	}
	return n, nil
}

// fail records a failed attempt for key and drops every record older than failWindow.
func (m *model) fail(ctx context.Context, key string) error {
	if _, err := m.db.Exec(ctx, `
		with gone as (delete from auth_failures where at < now() - make_interval(secs => $2))
		insert into auth_failures (key) values ($1)`, key, failWindow.Seconds()); err != nil {
		return fmt.Errorf("record failure: %w", err)
	}
	return nil
}

// userWithPassword returns the user and their password hash; hash is nil when the user is missing or has no password.
func (m *model) userWithPassword(ctx context.Context, email string) (User, *string, error) {
	var u User
	var hash *string
	err := m.db.QueryRow(ctx, `select id::text, name, email, password_hash from users where email = $1`, email).
		Scan(&u.ID, &u.Name, &u.Email, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, nil
	}
	if err != nil {
		return User{}, nil, fmt.Errorf("find user: %w", err)
	}
	return u, hash, nil
}

// userIDByEmail returns "" when there is no such user.
func (m *model) userIDByEmail(ctx context.Context, email string) (string, error) {
	var id string
	err := m.db.QueryRow(ctx, `select id::text from users where email = $1`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	return id, nil
}

// suspended reports whether a platform admin has suspended the user.
func (m *model) suspended(ctx context.Context, id string) (bool, error) {
	var s bool
	err := m.db.QueryRow(ctx, `select suspended_at is not null from users where id = $1`, id).Scan(&s)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read suspension: %w", err)
	}
	return s, nil
}

// Profile is the signed-in user as GET /api/me returns it.
type Profile struct {
	User
	HasPassword bool    `json:"has_password"`
	AvatarURL   *string `json:"avatar_url"`
}

// profile returns nil when there is no such user.
func (m *model) profile(ctx context.Context, id string) (*Profile, error) {
	var p Profile
	err := m.db.QueryRow(ctx,
		`select id::text, name, email, password_hash is not null, '/api/files/' || avatar_file_id || '/avatar'
		 from users where id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Email, &p.HasPassword, &p.AvatarURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &p, nil
}

// passwordHash returns the user's password hash; nil when they have none (or don't exist).
func (m *model) passwordHash(ctx context.Context, id string) (*string, error) {
	var hash *string
	err := m.db.QueryRow(ctx, `select password_hash from users where id = $1`, id).Scan(&hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read password: %w", err)
	}
	return hash, nil
}

// setPassword stores a new password hash and signs out every other session: all refresh tokens except the family
// that keepTokenHash (the caller's refresh token, may be nil) belongs to.
func (m *model) setPassword(ctx context.Context, id, passwordHash string, keepTokenHash []byte) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update users set password_hash = $2 where id = $1`, id, passwordHash); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`delete from refresh_tokens where user_id = $1 and family_id is distinct from
			   (select family_id from refresh_tokens where token_hash = $2 and user_id = $1)`, id, keepTokenHash); err != nil {
			return fmt.Errorf("sign out other sessions: %w", err)
		}
		return realtime.Publish(ctx, tx, realtime.SessionRevoked(id))
	})
}

// userForGoogle finds the user by Google subject, else links the account with the same (Google-verified) email,
// else creates one without a password. created reports the last case, or linking an account that wasn't verified.
func (m *model) userForGoogle(ctx context.Context, sub, email, name string) (u User, created bool, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select id::text, name, email from users where google_sub = $1`, sub).Scan(&u.ID, &u.Name, &u.Email)
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `update users set google_sub = $2 where email = $1 and google_sub is null returning id::text, name, email`,
			email, sub).Scan(&u.ID, &u.Name, &u.Email)
		if err == nil {
			created, err = markVerified(ctx, tx, &u, name)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		created = true
		if err := tx.QueryRow(ctx, `insert into users (name, email, google_sub, email_verified_at) values ($1, $2, $3, now()) returning id::text, name, email`,
			name, email, sub).Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminUsers(u.ID))
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, false, errEmailTaken // the email belongs to an account linked to a different Google account
	}
	if err != nil {
		return User{}, false, fmt.Errorf("google user: %w", err)
	}
	return u, created, nil
}

// updateName returns the updated user, or nil when there is no such user.
func (m *model) updateName(ctx context.Context, id, name string) (*User, error) {
	var u User
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `update users set name = $2 where id = $1 returning id::text, name, email`, id, name).
			Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return err
		}
		return realtime.PublishProfile(ctx, tx, id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update name: %w", err)
	}
	return &u, nil
}

// --- sign-in codes and reset links ---

// replaceToken voids the user's unused tokens of this purpose and stores a new one.
func (m *model) replaceToken(ctx context.Context, userID, purpose string, tokenHash []byte, minutes int) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where user_id = $1 and purpose = $2 and used_at is null`,
			userID, purpose); err != nil {
			return fmt.Errorf("void old tokens: %w", err)
		}
		if _, err := tx.Exec(ctx, `insert into auth_tokens (user_id, purpose, token_hash, expires_at) values ($1, $2, $3, now() + make_interval(mins => $4))`,
			userID, purpose, tokenHash, minutes); err != nil {
			return fmt.Errorf("insert token: %w", err)
		}
		return nil
	})
}

// useCode checks codeHash against the newest live sign-in code for email. A match uses it up, verifies the email and
// returns the user (first reports it wasn't verified before); a miss counts an attempt and returns nil.
func (m *model) useCode(ctx context.Context, email string, codeHash []byte) (u *User, first bool, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var tokenID string
		var stored []byte
		var cand User
		err := tx.QueryRow(ctx, `
			select t.id::text, t.token_hash, u.id::text, u.name, u.email
			from auth_tokens t join users u on u.id = t.user_id
			where u.email = $1 and t.purpose = $2 and t.used_at is null and t.expires_at > now() and t.attempts < $3
			order by t.created_at desc limit 1
			for update of t`, email, purposeCode, maxAttempts).
			Scan(&tokenID, &stored, &cand.ID, &cand.Name, &cand.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find code: %w", err)
		}
		if subtle.ConstantTimeCompare(stored, codeHash) != 1 {
			if _, err := tx.Exec(ctx, `update auth_tokens set attempts = attempts + 1 where id = $1`, tokenID); err != nil {
				return fmt.Errorf("count attempt: %w", err)
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where id = $1`, tokenID); err != nil {
			return fmt.Errorf("use code: %w", err)
		}
		if first, err = markVerified(ctx, tx, &cand, ""); err != nil {
			return err
		}
		u = &cand
		return nil
	})
	return u, first, err
}

// useUserCode checks codeHash against the user's newest live code of purpose. A match uses it up and returns true;
// a miss counts an attempt (the code dies after maxAttempts) and returns false.
func (m *model) useUserCode(ctx context.Context, userID, purpose string, codeHash []byte) (ok bool, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var tokenID string
		var stored []byte
		err := tx.QueryRow(ctx, `
			select id::text, token_hash from auth_tokens
			where user_id = $1 and purpose = $2 and used_at is null and expires_at > now() and attempts < $3
			order by created_at desc limit 1
			for update`, userID, purpose, maxAttempts).Scan(&tokenID, &stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find code: %w", err)
		}
		if subtle.ConstantTimeCompare(stored, codeHash) != 1 {
			if _, err := tx.Exec(ctx, `update auth_tokens set attempts = attempts + 1 where id = $1`, tokenID); err != nil {
				return fmt.Errorf("count attempt: %w", err)
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where id = $1`, tokenID); err != nil {
			return fmt.Errorf("use code: %w", err)
		}
		ok = true
		return nil
	})
	return ok, err
}

// resetPassword uses a live reset link, sets the new password hash, verifies the email (the link proves it) and
// revokes all the user's refresh tokens. It returns the account's email, or "" when the link is unknown, used or
// expired.
func (m *model) resetPassword(ctx context.Context, tokenHash []byte, passwordHash string) (string, error) {
	email := ""
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var tokenID, userID, addr string
		err := tx.QueryRow(ctx, `
			select t.id::text, t.user_id::text, u.email from auth_tokens t join users u on u.id = t.user_id
			where t.token_hash = $1 and t.purpose = $2 and t.used_at is null and t.expires_at > now()
			for update of t`, tokenHash, purposeReset).Scan(&tokenID, &userID, &addr)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find reset token: %w", err)
		}
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where id = $1`, tokenID); err != nil {
			return fmt.Errorf("use reset token: %w", err)
		}
		if _, err := tx.Exec(ctx, `update users set password_hash = $2 where id = $1`, userID, passwordHash); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		if _, err := tx.Exec(ctx, `delete from refresh_tokens where user_id = $1`, userID); err != nil {
			return fmt.Errorf("sign out sessions: %w", err)
		}
		if _, err := markVerified(ctx, tx, &User{ID: userID}, ""); err != nil {
			return err
		}
		email = addr
		return realtime.Publish(ctx, tx, realtime.SessionRevoked(userID))
	})
	return email, err
}

// --- refresh tokens ---

// insertRefresh stores a refresh token of family f (a new family when f.id is "") and returns the family's id.
func (m *model) insertRefresh(ctx context.Context, tokenHash []byte, userID string, f family) (string, error) {
	var familyID *string
	if f.id != "" {
		familyID = &f.id
	}
	var id string
	if err := m.db.QueryRow(ctx, `
		insert into refresh_tokens (token_hash, user_id, family_id, remember, expires_at)
		values ($1, $2, coalesce($3::uuid, uuidv7()), $4, $5) returning family_id::text`,
		tokenHash, userID, familyID, f.remember, f.expiresAt).Scan(&id); err != nil {
		return "", fmt.Errorf("insert refresh token: %w", err)
	}
	return id, nil
}

// rotateRefresh marks a live refresh token used and returns its user and family; ok is false when the token is
// unknown, expired or belongs to a suspended user. A token already rotated within reuseGrace is accepted again; after that, presenting it means
// it leaked, so the whole family is revoked.
func (m *model) rotateRefresh(ctx context.Context, tokenHash []byte) (userID string, f family, ok bool, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var usedAt *time.Time
		err := tx.QueryRow(ctx, `
			select t.user_id::text, t.family_id::text, t.remember, t.expires_at, t.used_at
			from refresh_tokens t join users u on u.id = t.user_id
			where t.token_hash = $1 and t.expires_at > now() and u.suspended_at is null
			for update of t`, tokenHash).Scan(&userID, &f.id, &f.remember, &f.expiresAt, &usedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find refresh token: %w", err)
		}
		if usedAt != nil {
			if time.Since(*usedAt) <= reuseGrace {
				ok = true
				return nil
			}
			slog.Warn("refresh token reuse, revoking family", "user_id", userID, "family_id", f.id)
			if _, err := tx.Exec(ctx, `delete from refresh_tokens where family_id = $1`, f.id); err != nil {
				return fmt.Errorf("revoke family: %w", err)
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `update refresh_tokens set used_at = now() where token_hash = $1`, tokenHash); err != nil {
			return fmt.Errorf("rotate refresh token: %w", err)
		}
		ok = true
		return nil
	})
	return userID, f, ok, err
}

// revokeFamily deletes every refresh token of the sign-in that tokenHash belongs to, and closes its user's sockets
// (the others reconnect).
func (m *model) revokeFamily(ctx context.Context, tokenHash []byte) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var userID string
		err := tx.QueryRow(ctx, `
			with d as (delete from refresh_tokens where family_id = (select family_id from refresh_tokens where token_hash = $1)
			           returning user_id)
			select user_id::text from d limit 1`, tokenHash).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.SessionRevoked(userID))
	})
	if err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return nil
}
