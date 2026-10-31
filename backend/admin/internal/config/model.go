package config

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/audit"
	"github.com/devender07/tether/backend/admin/internal/realtime"
)

type Setting struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Secret      bool      `json:"secret"`
	HasValue    bool      `json:"has_value"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// mask fills HasValue and blanks a secret's value.
func (s *Setting) mask() {
	s.HasValue = s.Value != ""
	if s.Secret {
		s.Value = ""
	}
}

// model owns every system_config query.
type model struct{ db *pgxpool.Pool }

const columns = `key, value, secret, description, updated_at`

func scan(row pgx.Row) (*Setting, error) {
	var s Setting
	err := row.Scan(&s.Key, &s.Value, &s.Secret, &s.Description, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

func (m *model) list(ctx context.Context) ([]Setting, error) {
	rows, err := m.db.Query(ctx, `select `+columns+` from system_config order by key`)
	if err != nil {
		return nil, fmt.Errorf("list config: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Setting, error) {
		s, err := scan(row)
		return *s, err
	})
	if err != nil {
		return nil, fmt.Errorf("list config: %w", err)
	}
	return out, nil
}

// get returns nil when the key doesn't exist.
func (m *model) get(ctx context.Context, key string) (*Setting, error) {
	s, err := scan(m.db.QueryRow(ctx, `select `+columns+` from system_config where key = $1`, key))
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", key, err)
	}
	return s, nil
}

// set changes the value, by the admin from ip, and records the change (the key only) in the audit log.
func (m *model) set(ctx context.Context, adminID, ip, key, value string) (*Setting, error) {
	var s *Setting
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var err error
		s, err = scan(tx.QueryRow(ctx,
			`update system_config set value = $2, updated_at = now() where key = $1 returning `+columns, key, value))
		if err == nil && s == nil {
			err = errors.New("setting disappeared")
		}
		if err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, adminID, audit.ConfigUpdate, key, ip); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, "admin.config", adminID, "/config")
	})
	if err != nil {
		return nil, fmt.Errorf("update config %s: %w", key, err)
	}
	return s, nil
}
