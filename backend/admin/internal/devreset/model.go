package devreset

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/audit"
)

type model struct{ db *pgxpool.Pool }

// kept are the tables a reset leaves alone, besides the default_ ones (workspace defaults).
var kept = []string{"system_config", "schema_migrations", "admins", "admin_mfa", "admin_sessions", "admin_audit"}

func (m *model) baseURL(ctx context.Context) (string, error) {
	var v string
	err := m.db.QueryRow(ctx, `select value from system_config where key = 'app_base_url'`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read app_base_url: %w", err)
	}
	return v, nil
}

// reset truncates every other table in one transaction and records it in the admin audit log. The workspace audit
// log refuses truncates, so its guard is off for the statement. Stored files' bytes stay in object storage.
func (m *model) reset(ctx context.Context, adminID, ip string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var tables string
		if err := tx.QueryRow(ctx, `select coalesce(string_agg(quote_ident(tablename), ', '), '') from pg_tables
			where schemaname = 'public' and tablename <> all($1) and tablename not like 'default\_%'`, kept).Scan(&tables); err != nil {
			return err
		}
		if tables == "" {
			return nil
		}
		if _, err := tx.Exec(ctx, `alter table workspace_audit disable trigger workspace_audit_no_truncate`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `truncate `+tables+` restart identity cascade`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `alter table workspace_audit enable trigger workspace_audit_no_truncate`); err != nil {
			return err
		}
		return audit.Record(ctx, tx, adminID, audit.DevReset, "user data", ip)
	})
	if err != nil {
		return fmt.Errorf("reset user data: %w", err)
	}
	return nil
}
