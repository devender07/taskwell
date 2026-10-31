// Package db opens the Postgres pool and applies embedded migrations.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open connects to Postgres and applies pending migrations. The pool has DB_MAX_CONNS connections (default 20;
// budget every instance's against max_connections), and every session gets DB_STATEMENT_TIMEOUT (default 30s) and
// DB_IDLE_TX_TIMEOUT (default 2min) so a stuck query or transaction can't hold a connection for good.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	maxConns, err := strconv.ParseInt(env("DB_MAX_CONNS", "20"), 10, 32)
	if err != nil || maxConns < 4 {
		return nil, fmt.Errorf("DB_MAX_CONNS must be a number of at least 4") // two listeners hold one each
	}
	cfg.MaxConns = int32(maxConns)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = env("DB_STATEMENT_TIMEOUT", "30s")
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = env("DB_IDLE_TX_TIMEOUT", "2min")
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// migrate applies migrations/*.sql in filename order inside one transaction,
// recording each in schema_migrations. An advisory lock serializes concurrent starts.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migrate: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx, `set local statement_timeout = 0; select pg_advisory_xact_lock(7291); create table if not exists schema_migrations (version text primary key, applied_at timestamptz not null default now())`); err != nil {
		return fmt.Errorf("prepare schema_migrations: %w", err)
	}
	for _, name := range names {
		var done bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from schema_migrations where version = $1)`, path.Base(name)).Scan(&done); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if done {
			continue
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations (version) values ($1)`, path.Base(name)); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		slog.Info("applied migration", "version", path.Base(name))
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
