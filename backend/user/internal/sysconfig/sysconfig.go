// Package sysconfig reads runtime settings from the system_config table, caching each for up to ttl, so admin edits
// apply without a restart: within ttl, or at once when the admin app's config event reaches Invalidate.
package sysconfig

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ttl bounds how stale a value can be when an invalidation is missed (the listener was down).
const ttl = 10 * time.Second

type Store struct {
	pool  *pgxpool.Pool
	mu    sync.Mutex
	cache map[string]entry
	since time.Time // the last Invalidate: a read that started before it is stale
}

type entry struct {
	value string
	found bool
	at    time.Time
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, cache: map[string]entry{}} }

// Get returns the value for key, or "" if the key is missing.
func (s *Store) Get(ctx context.Context, key string) (string, error) {
	v, _, err := s.Lookup(ctx, key)
	return v, err
}

// Lookup returns the value for key and whether the key exists.
func (s *Store) Lookup(ctx context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	e, ok := s.cache[key]
	s.mu.Unlock()
	if ok && time.Since(e.at) < ttl {
		return e.value, e.found, nil
	}
	at := time.Now()
	var v string
	err := s.pool.QueryRow(ctx, `select value from system_config where key = $1`, key).Scan(&v)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("read config %s: %w", key, err)
	}
	found := err == nil
	s.mu.Lock()
	if at.After(s.since) {
		s.cache[key] = entry{v, found, at}
	}
	s.mu.Unlock()
	return v, found, nil
}

// Invalidate drops every cached value; the next Get of each reads the table.
func (s *Store) Invalidate() {
	s.mu.Lock()
	clear(s.cache)
	s.since = time.Now()
	s.mu.Unlock()
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
