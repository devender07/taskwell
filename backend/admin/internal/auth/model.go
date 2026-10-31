package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// Promote makes the user with this email a platform admin. It returns nil when there is no such user or their email
// isn't verified (anyone can sign up with any address).
func Promote(ctx context.Context, db *pgxpool.Pool, email string) (*User, error) {
	var u User
	err := db.QueryRow(ctx, `
		update users set platform_admin = true where email = lower(trim($1)) and email_verified_at is not null
		returning id::text, name, email`,
		email).Scan(&u.ID, &u.Name, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("promote: %w", err)
	}
	return &u, nil
}
