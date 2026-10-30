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

// Promote makes the user with this email a platform admin. It returns nil when there is no such user.
func Promote(ctx context.Context, db *pgxpool.Pool, email string) (*User, error) {
	var u User
	err := db.QueryRow(ctx, `update users set platform_admin = true where email = lower(trim($1)) returning id::text, name, email`,
		email).Scan(&u.ID, &u.Name, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("promote: %w", err)
	}
	return &u, nil
}
