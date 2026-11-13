package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/shared/totp"
)

// EncryptSecrets seals every TOTP secret still stored in plaintext and returns how many it sealed.
func EncryptSecrets(ctx context.Context, db *pgxpool.Pool, key []byte) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select admin_id::text, secret from admin_mfa where secret not like 'v1:%' for update`)
		if err != nil {
			return err
		}
		type row struct{ userID, secret string }
		plain, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
			var x row
			return x, r.Scan(&x.userID, &x.secret)
		})
		if err != nil {
			return err
		}
		for _, p := range plain {
			sealed, err := totp.Seal(key, p.userID, p.secret)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `update admin_mfa set secret = $2 where admin_id = $1`, p.userID, sealed); err != nil {
				return err
			}
		}
		n = len(plain)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("encrypt mfa secrets: %w", err)
	}
	return n, nil
}
