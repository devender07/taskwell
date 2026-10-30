// Package sysconfig reads runtime settings from the system_config table on every use,
// so admin edits apply without a restart.
package sysconfig

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Get returns the value for key, or "" if the key is missing.
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.pool.QueryRow(ctx, `select value from system_config where key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read config %s: %w", key, err)
	}
	return v, nil
}

// GetInt returns key parsed as an int.
func (s *Store) GetInt(ctx context.Context, key string) (int, error) {
	v, err := s.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config %s is not an int: %w", key, err)
	}
	return n, nil
}
