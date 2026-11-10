package auth

import (
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/shared/totp"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
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
	// An email change needs both: a code sent to the current address and one sent to the new address.
	purposeEmailOld = "email_change_old"
	purposeEmailNew = "email_change_new"
	// purposeAccountDelete is the emailed code that confirms deleting the account.
	purposeAccountDelete = "account_delete"
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
type model struct {
	db *pgxpool.Pool
	// onCreated runs in the transaction that creates or first verifies an account (it creates the default
	// workspace), so a failure leaves no half-made account.
	onCreated func(ctx context.Context, tx pgx.Tx, userID, name, workspaceName string) error
}

// --- users ---

// startSignup records a sign-up awaiting its emailed code: it creates the unverified user without a password (or
// keeps the existing unverified one) and stores the code's hash with the name and password hash to apply once it is
// verified. Older pending codes stay usable: each is bound to the browser that asked for it. Returns errEmailTaken
// when a verified account has the email.
func (m *model) startSignup(ctx context.Context, name, email, passwordHash, workspaceName string, tokenHash []byte, minutes int) error {
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
			insert into auth_tokens (user_id, purpose, token_hash, expires_at, name, password_hash, workspace_name)
			values ($1, $2, $3, now() + make_interval(mins => $4), $5, $6, nullif($7, ''))`,
			userID, purposeVerify, tokenHash, minutes, name, passwordHash, workspaceName); err != nil {
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
		var tokenID, userID, name, ph, wsName string
		err := tx.QueryRow(ctx, `
			select t.id::text, u.id::text, t.name, t.password_hash, coalesce(t.workspace_name, '')
			from auth_tokens t join users u on u.id = t.user_id
			where u.email = $1 and u.email_verified_at is null and t.purpose = $2 and t.token_hash = $3
			  and t.used_at is null and t.expires_at > now()
			for update of t, u`, email, purposeVerify, tokenHash).Scan(&tokenID, &userID, &name, &ph, &wsName)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find signup code: %w", err)
		}
		var got User
		if err := tx.QueryRow(ctx, `
			update users set name = $2, password_hash = $3, email_verified_at = now(), terms_accepted_at = now() where id = $1
			returning id::text, name, email`, userID, name, ph).Scan(&got.ID, &got.Name, &got.Email); err != nil {
			return fmt.Errorf("verify user: %w", err)
		}
		if err := voidVerify(ctx, tx, userID); err != nil {
			return err
		}
		u = &got
		if err := m.onCreated(ctx, tx, got.ID, got.Name, wsName); err != nil {
			return err
		}
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
	HasPassword         bool       `json:"has_password"`
	AvatarURL           *string    `json:"avatar_url"`
	CanCreateWorkspaces bool       `json:"can_create_workspaces"`
	TermsAcceptedAt     *time.Time `json:"terms_accepted_at"`
}

// profile returns nil when there is no such user.
func (m *model) profile(ctx context.Context, id string) (*Profile, error) {
	var p Profile
	err := m.db.QueryRow(ctx,
		`select id::text, name, email, password_hash is not null, '/api/files/' || avatar_file_id || '/avatar',
		        can_create_workspace(id), terms_accepted_at
		 from users where id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Email, &p.HasPassword, &p.AvatarURL, &p.CanCreateWorkspaces, &p.TermsAcceptedAt)
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

// setPassword stores a new password hash, records action (password_changed or password_added) and signs out every
// other session: all refresh tokens except the family that keepTokenHash (the caller's refresh token, may be nil)
// belongs to.
func (m *model) setPassword(ctx context.Context, id, passwordHash string, keepTokenHash []byte, action string, md meta) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update users set password_hash = $2 where id = $1`, id, passwordHash); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		if err := addEvent(ctx, tx, id, action, "", md); err != nil {
			return err
		}
		return revokeOthers(ctx, tx, id, keepTokenHash)
	})
}

// revokeOthers signs out every session of the user but the family keepTokenHash (may be nil) belongs to.
func revokeOthers(ctx context.Context, tx pgx.Tx, id string, keepTokenHash []byte) error {
	if _, err := tx.Exec(ctx,
		`delete from refresh_tokens where user_id = $1 and family_id is distinct from
		   (select family_id from refresh_tokens where token_hash = $2 and user_id = $1)`, id, keepTokenHash); err != nil {
		return fmt.Errorf("sign out other sessions: %w", err)
	}
	return realtime.Publish(ctx, tx, realtime.SessionRevoked(id))
}

// userForGoogle finds the user by Google subject, else links the account with the same (Google-verified) email,
// else creates one without a password. Creating one, or linking an account that wasn't verified, runs onCreated.
func (m *model) userForGoogle(ctx context.Context, sub, email, name string) (u User, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `select id::text, name, email from users where google_sub = $1`, sub).Scan(&u.ID, &u.Name, &u.Email)
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		err = tx.QueryRow(ctx, `update users set google_sub = $2 where email = $1 and google_sub is null returning id::text, name, email`,
			email, sub).Scan(&u.ID, &u.Name, &u.Email)
		if err == nil {
			if first, err := markVerified(ctx, tx, &u, name); err != nil || !first {
				return err
			}
			// Signing up with Google: the button sits under the line that accepts the terms.
			if _, err := tx.Exec(ctx, `update users set terms_accepted_at = now() where id = $1`, u.ID); err != nil {
				return err
			}
			return m.onCreated(ctx, tx, u.ID, u.Name, "")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(ctx, `insert into users (name, email, google_sub, email_verified_at, terms_accepted_at)
			values ($1, $2, $3, now(), now()) returning id::text, name, email`,
			name, email, sub).Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return err
		}
		if err := m.onCreated(ctx, tx, u.ID, u.Name, ""); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminUsers(u.ID))
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, errEmailTaken // the email belongs to an account linked to a different Google account
	}
	if err != nil {
		return User{}, fmt.Errorf("google user: %w", err)
	}
	return u, nil
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
// returns the user (running onCreated when it wasn't verified before); a miss counts an attempt and returns nil.
func (m *model) useCode(ctx context.Context, email string, codeHash []byte) (u *User, err error) {
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
		first, err := markVerified(ctx, tx, &cand, "")
		if err != nil {
			return err
		}
		if first {
			if err := m.onCreated(ctx, tx, cand.ID, cand.Name, ""); err != nil {
				return err
			}
		}
		u = &cand
		return nil
	})
	return u, err
}

// useUserCode checks codeHash against the user's newest live code of purpose. A match uses it up and returns true;
// a miss counts an attempt (the code dies after maxAttempts) and returns false.
func (m *model) useUserCode(ctx context.Context, userID, purpose string, codeHash []byte) (ok bool, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		ok, err = useUserCode(ctx, tx, userID, purpose, codeHash)
		return err
	})
	return ok, err
}

// useUserCode is the method in tx; commit tx either way, so a miss still counts.
func useUserCode(ctx context.Context, tx pgx.Tx, userID, purpose string, codeHash []byte) (bool, error) {
	var tokenID string
	var stored []byte
	err := tx.QueryRow(ctx, `
		select id::text, token_hash from auth_tokens
		where user_id = $1 and purpose = $2 and used_at is null and expires_at > now() and attempts < $3
		order by created_at desc limit 1
		for update`, userID, purpose, maxAttempts).Scan(&tokenID, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find code: %w", err)
	}
	if subtle.ConstantTimeCompare(stored, codeHash) != 1 {
		if _, err := tx.Exec(ctx, `update auth_tokens set attempts = attempts + 1 where id = $1`, tokenID); err != nil {
			return false, fmt.Errorf("count attempt: %w", err)
		}
		return false, nil
	}
	if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where id = $1`, tokenID); err != nil {
		return false, fmt.Errorf("use code: %w", err)
	}
	return true, nil
}

// startEmailChange voids the user's pending email change and stores the codes for their current address (oldHash) and
// for newEmail (newHash), both remembering newEmail.
func (m *model) startEmailChange(ctx context.Context, userID, newEmail string, oldHash, newHash []byte, minutes int) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now()
			where user_id = $1 and purpose in ($2, $3) and used_at is null`, userID, purposeEmailOld, purposeEmailNew); err != nil {
			return fmt.Errorf("void email change: %w", err)
		}
		if _, err := tx.Exec(ctx, `insert into auth_tokens (user_id, purpose, token_hash, expires_at, new_email)
			select $1, p, h, now() + make_interval(mins => $6), $7 from unnest(array[$2, $3], array[$4::bytea, $5::bytea]) as t(p, h)`,
			userID, purposeEmailOld, purposeEmailNew, oldHash, newHash, minutes, newEmail); err != nil {
			return fmt.Errorf("store email change: %w", err)
		}
		return nil
	})
}

// changeEmail checks both codes of the user's pending email change. When both match it uses them, moves the account
// to the new address (verified: the code proves it), signs out every other session (all refresh tokens but the family
// of keepTokenHash) and returns the old and new addresses. A wrong code counts an attempt on it and is named in bad
// (old_code, new_code); no pending change makes both bad. errEmailTaken when the address was taken meanwhile.
func (m *model) changeEmail(ctx context.Context, userID string, oldHash, newHash, keepTokenHash []byte, md meta) (from, to string, bad []string, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		type code struct {
			field, purpose, id, email string
			hash, given               []byte
		}
		codes := []*code{{field: "old_code", purpose: purposeEmailOld, given: oldHash}, {field: "new_code", purpose: purposeEmailNew, given: newHash}}
		for _, c := range codes {
			err := tx.QueryRow(ctx, `select id::text, token_hash, new_email from auth_tokens
				where user_id = $1 and purpose = $2 and used_at is null and expires_at > now() and attempts < $3
				order by created_at desc limit 1 for update`, userID, c.purpose, maxAttempts).Scan(&c.id, &c.hash, &c.email)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("find email code: %w", err)
			}
			if c.id == "" || subtle.ConstantTimeCompare(c.hash, c.given) != 1 {
				bad = append(bad, c.field)
				if c.id != "" {
					if _, err := tx.Exec(ctx, `update auth_tokens set attempts = attempts + 1 where id = $1`, c.id); err != nil {
						return fmt.Errorf("count attempt: %w", err)
					}
				}
			}
		}
		if len(bad) > 0 || codes[0].email != codes[1].email {
			if len(bad) == 0 {
				bad = []string{"old_code", "new_code"} // from different requests: start over
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where id = any($1::uuid[])`,
			[]string{codes[0].id, codes[1].id}); err != nil {
			return fmt.Errorf("use email codes: %w", err)
		}
		if err := tx.QueryRow(ctx, `update users u set email = $2, email_verified_at = coalesce(email_verified_at, now())
			from (select email from users where id = $1) old where u.id = $1 returning old.email, u.email`,
			userID, codes[1].email).Scan(&from, &to); err != nil {
			return err
		}
		if err := addEvent(ctx, tx, userID, "email_changed", to, md); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`delete from refresh_tokens where user_id = $1 and family_id is distinct from
			   (select family_id from refresh_tokens where token_hash = $2 and user_id = $1)`, userID, keepTokenHash); err != nil {
			return fmt.Errorf("sign out other sessions: %w", err)
		}
		if err := realtime.Publish(ctx, tx, realtime.SessionRevoked(userID)); err != nil {
			return err
		}
		return realtime.PublishProfile(ctx, tx, userID)
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return "", "", nil, errEmailTaken
	}
	if err != nil {
		return "", "", nil, fmt.Errorf("change email: %w", err)
	}
	return from, to, bad, nil
}

// resetPassword uses a live reset link, sets the new password hash, verifies the email (the link proves it) and
// revokes all the user's refresh tokens. It returns the account's email, or "" when the link is unknown, used or
// expired.
func (m *model) resetPassword(ctx context.Context, tokenHash []byte, passwordHash string, md meta) (string, error) {
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
		if err := addEvent(ctx, tx, userID, "password_changed", "reset", md); err != nil {
			return err
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

// revokeFamily deletes every refresh token of the sign-in that tokenHash belongs to, records the sign-out and closes
// its user's sockets (the others reconnect).
func (m *model) revokeFamily(ctx context.Context, tokenHash []byte, md meta) error {
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
		if err := addEvent(ctx, tx, userID, "sign_out", "", md); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.SessionRevoked(userID))
	})
	if err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return nil
}

// --- personal API tokens ---

const maxAPITokens = 25

// ErrTooManyTokens is returned when the user already has maxAPITokens.
var ErrTooManyTokens = errors.New("api token limit reached")

// APIToken is a personal API token as the token endpoints return it; the secret itself is never stored.
type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// apiTokenUser returns the user ID of the token with the hash, or "" when it is unknown, revoked or its user is
// suspended. It marks the token used, at most once a minute.
func (m *model) apiTokenUser(ctx context.Context, tokenHash []byte) (string, error) {
	var userID string
	err := m.db.QueryRow(ctx, `
		with t as (select a.id, a.user_id from api_tokens a join users u on u.id = a.user_id
		           where a.token_hash = $1 and u.suspended_at is null),
		     touch as (update api_tokens set last_used_at = now()
		               where id = (select id from t) and (last_used_at is null or last_used_at < now() - interval '1 minute'))
		select user_id::text from t`, tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find api token: %w", err)
	}
	return userID, nil
}

// apiTokens returns the user's tokens, newest first.
func (m *model) apiTokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := m.db.Query(ctx, `
		select id::text, name, created_at, last_used_at from api_tokens where user_id = $1 order by created_at desc, id desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	tokens, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (APIToken, error) {
		var t APIToken
		err := row.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LastUsedAt)
		t.CreatedAt = t.CreatedAt.UTC()
		if t.LastUsedAt != nil {
			u := t.LastUsedAt.UTC()
			t.LastUsedAt = &u
		}
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	return tokens, nil
}

// insertAPIToken stores a token's hash and records it; ErrTooManyTokens when the user already has maxAPITokens.
func (m *model) insertAPIToken(ctx context.Context, userID, name string, tokenHash []byte, md meta) (*APIToken, error) {
	var t *APIToken
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if t, err = insertAPIToken(ctx, tx, userID, name, tokenHash); err != nil {
			return err
		}
		return addEvent(ctx, tx, userID, "token_created", name, md)
	})
	return t, err
}

// insertAPIToken is the method on q: the pool, or another package's transaction (see NewAPIToken).
// ponytail: two concurrent creates can both pass the count; lock the user row if the cap ever has to be exact.
func insertAPIToken(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, userID, name string, tokenHash []byte) (*APIToken, error) {
	t := APIToken{Name: name}
	err := q.QueryRow(ctx, `
		insert into api_tokens (user_id, name, token_hash)
		select $1, $2, $3 where (select count(*) from api_tokens where user_id = $1) < $4
		returning id::text, created_at`, userID, name, tokenHash, maxAPITokens).Scan(&t.ID, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTooManyTokens
	}
	if err != nil {
		return nil, fmt.Errorf("insert api token: %w", err)
	}
	t.CreatedAt = t.CreatedAt.UTC()
	return &t, nil
}

// deleteAPIToken revokes the user's token and records it; false when they have no token with the id.
func (m *model) deleteAPIToken(ctx context.Context, userID, id string, md meta) (bool, error) {
	tag, err := m.db.Exec(ctx, `
		with d as (delete from api_tokens where id = $1 and user_id = $2 returning user_id, name)
		insert into account_events (user_id, action, detail, ip, user_agent)
		select user_id, 'token_deleted', name, nullif($3, '')::inet, $4 from d`, id, userID, md.ip, md.ua)
	if err != nil {
		return false, fmt.Errorf("delete api token: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// --- two-factor ---

var (
	errMFAOn  = errors.New("two-factor is already on")
	errMFAOff = errors.New("two-factor is off")
)

// mfaStatus reports whether the user has two-factor on and how many recovery codes are left.
func (m *model) mfaStatus(ctx context.Context, userID string) (on bool, left int, err error) {
	err = m.db.QueryRow(ctx, `select enabled_at is not null, cardinality(recovery_codes) from user_mfa where user_id = $1`,
		userID).Scan(&on, &left)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("read two-factor: %w", err)
	}
	return on, left, nil
}

// startMFA stores a new sealed secret for enableMFA to confirm, replacing an unfinished setup; errMFAOn when
// two-factor is already on.
func (m *model) startMFA(ctx context.Context, userID, sealed string) error {
	tag, err := m.db.Exec(ctx, `
		insert into user_mfa (user_id, secret, recovery_codes) values ($1, $2, '{}')
		on conflict (user_id) do update set secret = excluded.secret, last_step = 0, created_at = now()
		where user_mfa.enabled_at is null`, userID, sealed)
	if err != nil {
		return fmt.Errorf("start two-factor: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errMFAOn
	}
	return nil
}

// enableMFA turns on the setup startMFA began when code matches its secret, storing the recovery code hashes; ok is
// false for a wrong code. errMFAOn when it's on already, errMFAOff when no setup was started.
func (m *model) enableMFA(ctx context.Context, key []byte, userID, code string, hashes []string, now time.Time, md meta) (ok bool, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var sealed string
		var on bool
		err := tx.QueryRow(ctx, `select secret, enabled_at is not null from user_mfa where user_id = $1 for update`, userID).
			Scan(&sealed, &on)
		if errors.Is(err, pgx.ErrNoRows) {
			return errMFAOff
		}
		if err != nil {
			return err
		}
		if on {
			return errMFAOn
		}
		secret, err := totp.Unseal(key, userID, sealed)
		if err != nil {
			return err
		}
		step, match, err := totp.Check(secret, code, now, 0)
		if err != nil || !match {
			return err
		}
		ok = true
		if _, err := tx.Exec(ctx, `update user_mfa set enabled_at = now(), last_step = $2, recovery_codes = $3 where user_id = $1`,
			userID, step, hashes); err != nil {
			return err
		}
		return addEvent(ctx, tx, userID, "mfa_enabled", "", md)
	})
	if err != nil && !errors.Is(err, errMFAOn) && !errors.Is(err, errMFAOff) {
		err = fmt.Errorf("enable two-factor: %w", err)
	}
	return ok, err
}

// mfaResult is what checking a second factor found.
type mfaResult struct {
	ok           bool
	usedRecovery bool // a recovery code (now spent) was used
}

// checkMFA checks a code from the authenticator app, or else a recovery code, against the user's factor. A match
// uses it up: the code's step is remembered so it can't be replayed, a recovery code is removed. errMFAOff when
// two-factor is off.
func checkMFA(ctx context.Context, tx pgx.Tx, key []byte, userID, code, recovery string, now time.Time) (mfaResult, error) {
	var sealed string
	var lastStep int64
	var hashes []string
	err := tx.QueryRow(ctx, `select secret, last_step, recovery_codes from user_mfa
		where user_id = $1 and enabled_at is not null for update`, userID).Scan(&sealed, &lastStep, &hashes)
	if errors.Is(err, pgx.ErrNoRows) {
		return mfaResult{}, errMFAOff
	}
	if err != nil {
		return mfaResult{}, err
	}
	if code == "" {
		h := totp.HashRecovery(recovery)
		if recovery == "" || !slices.Contains(hashes, h) {
			return mfaResult{}, nil
		}
		_, err := tx.Exec(ctx, `update user_mfa set recovery_codes = array_remove(recovery_codes, $2) where user_id = $1`, userID, h)
		return mfaResult{ok: true, usedRecovery: true}, err
	}
	secret, err := totp.Unseal(key, userID, sealed)
	if err != nil {
		return mfaResult{}, err
	}
	step, ok, err := totp.Check(secret, code, now, lastStep)
	if err != nil || !ok {
		return mfaResult{}, err
	}
	_, err = tx.Exec(ctx, `update user_mfa set last_step = $2 where user_id = $1`, userID, step)
	return mfaResult{ok: true}, err
}

// mfa runs checkMFA in a transaction and, on a match, then in the same transaction.
func (m *model) mfa(ctx context.Context, key []byte, userID, code, recovery string, then func(tx pgx.Tx) error) (mfaResult, error) {
	var res mfaResult
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		if res, err = checkMFA(ctx, tx, key, userID, code, recovery, time.Now()); err != nil || !res.ok || then == nil {
			return err
		}
		return then(tx)
	})
	if err != nil && !errors.Is(err, errMFAOff) {
		err = fmt.Errorf("check two-factor: %w", err)
	}
	return res, err
}

// disableMFA turns two-factor off when the code or recovery code matches, and signs out every other session (all but
// the sign-in keepSID; "" keeps none).
func (m *model) disableMFA(ctx context.Context, key []byte, userID, code, recovery, keepSID string, md meta) (bool, error) {
	res, err := m.mfa(ctx, key, userID, code, recovery, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `delete from user_mfa where user_id = $1`, userID); err != nil {
			return err
		}
		if err := addEvent(ctx, tx, userID, "mfa_disabled", "", md); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from refresh_tokens where user_id = $1 and family_id is distinct from $2::uuid`,
			userID, cmp.Or(keepSID, "00000000-0000-0000-0000-000000000000")); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.SessionRevoked(userID))
	})
	return res.ok, err
}

// replaceRecovery swaps the user's recovery codes for hashes when code (from the app) matches.
func (m *model) replaceRecovery(ctx context.Context, key []byte, userID, code string, hashes []string) (bool, error) {
	res, err := m.mfa(ctx, key, userID, code, "", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update user_mfa set recovery_codes = $2 where user_id = $1`, userID, hashes)
		return err
	})
	return res.ok, err
}

// --- account events, export and deletion ---

// meta is the request an account event records: the client IP ("" when it isn't one) and the browser.
type meta struct{ ip, ua string }

// addEvent records action on the user's account, on q: the pool, or the change's own transaction.
func addEvent(ctx context.Context, q interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}, userID, action, detail string, md meta) error {
	if _, err := q.Exec(ctx, `insert into account_events (user_id, action, detail, ip, user_agent)
		values ($1, $2, $3, nullif($4, '')::inet, $5)`, userID, action, detail, md.ip, md.ua); err != nil {
		return fmt.Errorf("record %s: %w", action, err)
	}
	return nil
}

// AccountEvent is one entry of GET /api/me/account-events.
type AccountEvent struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	IP        *string   `json:"ip"`
	UserAgent string    `json:"user_agent"`
	At        time.Time `json:"at"`
}

const eventsPage = 50

// accountEvents returns a page of the user's events, newest first, older than the event before unless it's "", and
// whether there are more.
func (m *model) accountEvents(ctx context.Context, userID, before string) ([]AccountEvent, bool, error) {
	rows, err := m.db.Query(ctx, `
		select id::text, action, detail, host(ip), user_agent, at from account_events
		where user_id = $1 and ($2 = '' or id < nullif($2, '')::uuid)
		order by id desc limit $3`, userID, before, eventsPage+1)
	if err != nil {
		return nil, false, fmt.Errorf("list account events: %w", err)
	}
	es, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AccountEvent, error) {
		var e AccountEvent
		err := row.Scan(&e.ID, &e.Action, &e.Detail, &e.IP, &e.UserAgent, &e.At)
		e.At = e.At.UTC()
		return e, err
	})
	if err != nil {
		return nil, false, fmt.Errorf("list account events: %w", err)
	}
	if len(es) > eventsPage {
		return es[:eventsPage], true, nil
	}
	return es, false, nil
}

// exportData is the user's personal data as GET /api/me/export answers it, but notification_settings: never secrets
// or hashes, and no workspace content. Sessions have no IP or browser: sign-ins don't store them (account_events does).
func (m *model) exportData(ctx context.Context, userID string) (map[string]json.RawMessage, error) {
	var b []byte
	err := m.db.QueryRow(ctx, `
		select json_build_object(
		  'exported_at', now(),
		  'profile', (select json_build_object('id', u.id, 'name', u.name, 'email', u.email, 'created_at', u.created_at,
		      'terms_accepted_at', u.terms_accepted_at,
		      'mfa_enabled', exists (select 1 from user_mfa f where f.user_id = u.id and f.enabled_at is not null),
		      'has_password', u.password_hash is not null, 'google_linked', u.google_sub is not null)
		    from users u where u.id = $1),
		  'workspaces', coalesce((select json_agg(json_build_object('slug', w.slug, 'name', w.name, 'role', r.name,
		      'joined_at', wm.created_at) order by w.name)
		    from workspace_members wm join workspaces w on w.id = wm.workspace_id join workspace_roles r on r.id = wm.role_id
		    where wm.user_id = $1), '[]'),
		  'api_tokens', coalesce((select json_agg(json_build_object('name', name, 'created_at', created_at,
		      'last_used_at', last_used_at) order by created_at)
		    from api_tokens where user_id = $1), '[]'),
		  'sessions', coalesce((select json_agg(json_build_object('created_at', s.created_at, 'last_used_at', s.last_used_at,
		      'ip', null, 'user_agent', '') order by s.created_at)
		    from (select min(created_at) created_at, max(created_at) last_used_at from refresh_tokens
		          where user_id = $1 and expires_at > now() group by family_id) s), '[]'),
		  'account_events', coalesce((select json_agg(json_build_object('action', action, 'detail', detail,
		      'ip', host(ip), 'user_agent', user_agent, 'at', at) order by id)
		    from account_events where user_id = $1), '[]'))`, userID).Scan(&b)
	if err != nil {
		return nil, fmt.Errorf("export personal data: %w", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("export personal data: %w", err)
	}
	return out, nil
}

// WorkspaceRef names a workspace that stops an account deletion.
type WorkspaceRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

var (
	// errSharedWorkspaces: the user is the only admin of workspaces that have other members.
	errSharedWorkspaces = errors.New("only admin of shared workspaces")
	// errPaidPlan: the user's plan is still billed.
	errPaidPlan   = errors.New("paid plan not cancelled")
	errDeleteCode = errors.New("wrong account deletion code")
)

// deleteAccount deletes the user with the account deletion code codeHash, in one transaction with the workspaces
// where they're the only member, and returns their email. It refuses, naming the workspaces, with errSharedWorkspaces
// or errPaidPlan, and with errDeleteCode for a wrong code (counting the attempt). Shared content stays, its author
// nulled by the foreign keys.
func (m *model) deleteAccount(ctx context.Context, userID string, codeHash []byte) (email string, refs []WorkspaceRef, err error) {
	var refused error
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `select email from users where id = $1 for update`, userID).Scan(&email); err != nil {
			return err
		}
		// Every workspace keeps an admin (keepingAdmin), so this is the user being its only one.
		rows, err := tx.Query(ctx, `
			select w.slug, w.name from workspaces w join workspace_members me on me.workspace_id = w.id and me.user_id = $1
			where exists (select 1 from workspace_members o where o.workspace_id = w.id and o.user_id <> $1)
			  and not exists (select 1 from workspace_members o join workspace_roles r on r.id = o.role_id
			                  where o.workspace_id = w.id and o.user_id <> $1 and r.builtin = 'admin')
			order by w.name`, userID)
		if err != nil {
			return err
		}
		if refs, err = pgx.CollectRows(rows, pgx.RowToStructByPos[WorkspaceRef]); err != nil {
			return err
		}
		if len(refs) > 0 {
			refused = errSharedWorkspaces
			return nil
		}
		// The plan is the account's (billing): a subscription that grants access and isn't cancelling is still billed.
		// Its workspaces are the ones the user made, and those where they're alone.
		var paid bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from paddle_subscriptions where user_id = $1
			and status in ('active', 'trialing', 'past_due') and scheduled_change_action is distinct from 'cancel')`, userID).Scan(&paid); err != nil {
			return err
		}
		if paid {
			refused = errPaidPlan
			rows, err := tx.Query(ctx, `
				select w.slug, w.name from workspaces w join workspace_members me on me.workspace_id = w.id and me.user_id = $1
				where w.created_by = $1 or not exists (select 1 from workspace_members o where o.workspace_id = w.id and o.user_id <> $1)
				order by w.name`, userID)
			if err != nil {
				return err
			}
			refs, err = pgx.CollectRows(rows, pgx.RowToStructByPos[WorkspaceRef])
			return err
		}
		if ok, err := useUserCode(ctx, tx, userID, purposeAccountDelete, codeHash); err != nil || !ok {
			refused = errDeleteCode
			return err // commit the counted attempt
		}
		rows, err = tx.Query(ctx, `
			select w.id::text, w.slug from workspaces w join workspace_members me on me.workspace_id = w.id and me.user_id = $1
			where not exists (select 1 from workspace_members o where o.workspace_id = w.id and o.user_id <> $1)`, userID)
		if err != nil {
			return err
		}
		alone, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (workspace.Workspace, error) {
			var ws workspace.Workspace
			err := row.Scan(&ws.ID, &ws.Slug)
			return ws, err
		})
		if err != nil {
			return err
		}
		for _, ws := range alone {
			if err := workspace.Delete(ctx, tx, &ws, userID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `delete from users where id = $1`, userID); err != nil {
			return err
		}
		if err := realtime.Publish(ctx, tx, realtime.SessionRevoked(userID)); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.AdminUsers(userID))
	})
	if err != nil {
		return "", nil, fmt.Errorf("delete account: %w", err)
	}
	return email, refs, refused
}
