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
)

const (
	maxAttempts = 5

	// reuseGrace lets two tabs refresh at the same moment without the second one looking like token theft.
	reuseGrace = 10 * time.Second

	purposeCode  = "signin_code"
	purposeReset = "password_reset"
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

type refreshResult int

const (
	refreshInvalid refreshResult = iota // unknown, expired, or reused (family revoked)
	refreshRaced                        // already rotated moments ago by a parallel request
	refreshOK
)

// model owns every query for users, sign-in codes, reset links and refresh tokens.
type model struct{ db *pgxpool.Pool }

// --- users ---

// createUser inserts a user; email must already be normalized. Returns errEmailTaken on a duplicate.
func (m *model) createUser(ctx context.Context, name, email, passwordHash string) (User, error) {
	var u User
	err := m.db.QueryRow(ctx,
		`insert into users (name, email, password_hash) values ($1, $2, $3) returning id::text, name, email`,
		name, email, passwordHash).Scan(&u.ID, &u.Name, &u.Email)
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, errEmailTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	return u, nil
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

// userByID returns nil when there is no such user.
func (m *model) userByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := m.db.QueryRow(ctx, `select id::text, name, email from users where id = $1`, id).Scan(&u.ID, &u.Name, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &u, nil
}

// userForGoogle finds the user by Google subject, else links the account with the same (Google-verified) email,
// else creates one without a password.
func (m *model) userForGoogle(ctx context.Context, sub, email, name string) (User, error) {
	var u User
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select id::text, name, email from users where google_sub = $1`, sub).Scan(&u.ID, &u.Name, &u.Email)
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `update users set google_sub = $2 where email = $1 and google_sub is null returning id::text, name, email`,
			email, sub).Scan(&u.ID, &u.Name, &u.Email)
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return tx.QueryRow(ctx, `insert into users (name, email, google_sub) values ($1, $2, $3) returning id::text, name, email`,
			name, email, sub).Scan(&u.ID, &u.Name, &u.Email)
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, errEmailTaken // the email belongs to an account linked to a different Google account
	}
	if err != nil {
		return User{}, fmt.Errorf("google user: %w", err)
	}
	return u, nil
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

// useCode checks codeHash against the newest live sign-in code for email. A match uses it up and returns the user;
// a miss counts an attempt and returns nil.
func (m *model) useCode(ctx context.Context, email string, codeHash []byte) (*User, error) {
	var u *User
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
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
		u = &cand
		return nil
	})
	return u, err
}

// resetPassword uses a live reset link, sets the new password hash and revokes all the user's refresh tokens.
// Returns false when the link is unknown, used or expired.
func (m *model) resetPassword(ctx context.Context, tokenHash []byte, passwordHash string) (bool, error) {
	ok := false
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var tokenID, userID string
		err := tx.QueryRow(ctx, `
			select id::text, user_id::text from auth_tokens
			where token_hash = $1 and purpose = $2 and used_at is null and expires_at > now()
			for update`, tokenHash, purposeReset).Scan(&tokenID, &userID)
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
		ok = true
		return nil
	})
	return ok, err
}

// --- refresh tokens ---

func (m *model) insertRefresh(ctx context.Context, tokenHash []byte, userID string, f family) error {
	var familyID *string
	if f.id != "" {
		familyID = &f.id
	}
	if _, err := m.db.Exec(ctx, `
		insert into refresh_tokens (token_hash, user_id, family_id, remember, expires_at)
		values ($1, $2, coalesce($3::uuid, uuidv7()), $4, $5)`,
		tokenHash, userID, familyID, f.remember, f.expiresAt); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// rotateRefresh marks a live refresh token used and returns its user and family. Presenting an already rotated
// token after reuseGrace means it leaked, so the whole family is revoked.
func (m *model) rotateRefresh(ctx context.Context, tokenHash []byte) (string, family, refreshResult, error) {
	var userID string
	var f family
	result := refreshInvalid
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var usedAt *time.Time
		err := tx.QueryRow(ctx, `
			select user_id::text, family_id::text, remember, expires_at, used_at from refresh_tokens
			where token_hash = $1 and expires_at > now()
			for update`, tokenHash).Scan(&userID, &f.id, &f.remember, &f.expiresAt, &usedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find refresh token: %w", err)
		}
		if usedAt != nil {
			if time.Since(*usedAt) <= reuseGrace {
				result = refreshRaced
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
		result = refreshOK
		return nil
	})
	return userID, f, result, err
}

// revokeFamily deletes every refresh token of the sign-in that tokenHash belongs to.
func (m *model) revokeFamily(ctx context.Context, tokenHash []byte) error {
	if _, err := m.db.Exec(ctx,
		`delete from refresh_tokens where family_id = (select family_id from refresh_tokens where token_hash = $1)`,
		tokenHash); err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return nil
}
