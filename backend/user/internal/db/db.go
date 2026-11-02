// Package db opens the Postgres pool and applies embedded migrations.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/shared/env"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open connects to Postgres. The pool has DB_MAX_CONNS connections (default 20; budget every instance's against
// max_connections), and every session gets DB_STATEMENT_TIMEOUT (default 30s) and DB_IDLE_TX_TIMEOUT (default 2min)
// so a stuck query or transaction can't hold a connection for good.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	maxConns, err := strconv.ParseInt(env.Get("DB_MAX_CONNS", "20"), 10, 32)
	if err != nil || maxConns < 4 {
		return nil, fmt.Errorf("DB_MAX_CONNS must be a number of at least 4") // two listeners hold one each
	}
	cfg.MaxConns = int32(maxConns)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = env.Get("DB_STATEMENT_TIMEOUT", "30s")
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = env.Get("DB_IDLE_TX_TIMEOUT", "2min")
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return pool, nil
}

// migrateLock is the session advisory lock that serializes concurrent migration runs.
const migrateLock = 7291

// Migrate applies the pending migrations/*.sql in filename order, recording each in schema_migrations.
// Each runs in its own transaction with lock_timeout MIGRATE_LOCK_TIMEOUT (default 10s), so a migration waiting on a
// busy table fails instead of queueing every query behind it. A file named *.notx.sql runs outside a transaction,
// one statement at a time (statements end with ";" at the end of a line), for create index concurrently; make each
// statement safe to rerun (drop index concurrently if exists first) since a failure leaves the earlier ones applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)

	pc, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migrate connection: %w", err)
	}
	conn := pc.Hijack() // its session settings and lock die with it
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, `set statement_timeout = 0`); err != nil {
		return fmt.Errorf("prepare migrate session: %w", err)
	}
	// Poll instead of blocking in pg_advisory_lock: create index concurrently waits for every open transaction,
	// including a blocked lock call, so another instance waiting here would deadlock with it.
	for {
		var got bool
		if err := conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, migrateLock).Scan(&got); err != nil {
			return fmt.Errorf("migrate lock: %w", err)
		}
		if got {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("migrate lock: %w", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
	if _, err := conn.Exec(ctx, `create table if not exists schema_migrations (version text primary key, applied_at timestamptz not null default now())`); err != nil {
		return fmt.Errorf("prepare schema_migrations: %w", err)
	}
	// ponytail: a database migrated before 001-043 were squashed into 001_init already has that schema; drop this once
	// every database has started on this version.
	if _, err := conn.Exec(ctx, `insert into schema_migrations (version) select '001_init.sql'
		where exists (select 1 from schema_migrations where version = '043_status_types.sql') on conflict do nothing`); err != nil {
		return fmt.Errorf("baseline squashed migrations: %w", err)
	}
	lockTimeout := env.Get("MIGRATE_LOCK_TIMEOUT", "10s")
	for _, name := range names {
		version := path.Base(name)
		var done bool
		if err := conn.QueryRow(ctx, `select exists(select 1 from schema_migrations where version = $1)`, version).Scan(&done); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if done {
			continue
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		if strings.HasSuffix(version, ".notx.sql") {
			err = applyNoTx(ctx, conn, version, string(sql))
		} else {
			err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `select set_config('lock_timeout', $1, true)`, lockTimeout); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, string(sql)); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `insert into schema_migrations (version) values ($1)`, version)
				return err
			})
		}
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		slog.Info("applied migration", "version", version)
	}
	return nil
}

// applyNoTx runs a non-transactional migration's statements one by one, then records it. They run without a
// lock_timeout: create index concurrently waits for running transactions but blocks no reads or writes meanwhile.
func applyNoTx(ctx context.Context, conn *pgx.Conn, version, sql string) error {
	for _, stmt := range statements(sql) {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%q: %w", stmt, err)
		}
	}
	_, err := conn.Exec(ctx, `insert into schema_migrations (version) values ($1)`, version)
	return err
}

// statements splits sql at each ";" that ends a line, dropping "--" comment lines.
// ponytail: line-based, so a ";" ending a line inside a string or a function body splits it; fine for index DDL.
func statements(sql string) []string {
	var out []string
	var cur strings.Builder
	for line := range strings.Lines(sql) {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}
		cur.WriteString(line)
		if strings.HasSuffix(t, ";") {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// IsUniqueViolation reports whether err is Postgres refusing a duplicate (unique_violation).
func IsUniqueViolation(err error) bool {
	pgErr := (*pgconn.PgError)(nil)
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
