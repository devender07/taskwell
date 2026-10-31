package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TOTP secrets are stored sealed: "v1:" + base64(nonce || AES-256-GCM ciphertext), with the user's id as associated
// data so a sealed secret can't be moved to another admin's row.
const sealPrefix = "v1:"

// MFAKey derives the AES-256 key from MFA_ENCRYPTION_KEY (or, in dev, the jwt_secret fallback).
func MFAKey(secret string) []byte {
	sum := sha256.Sum256([]byte("tether admin mfa\x00" + secret))
	return sum[:]
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("mfa key: %w", err)
	}
	return cipher.NewGCM(block)
}

// seal encrypts the user's TOTP secret for storage.
func seal(key []byte, userID, secret string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never returns an error (Go 1.24+)
	return sealPrefix + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(secret), []byte(userID))), nil
}

// unseal decrypts a stored TOTP secret; it fails on a plaintext one, a wrong key or another user's.
func unseal(key []byte, userID, stored string) (string, error) {
	raw, ok := strings.CutPrefix(stored, sealPrefix)
	if !ok {
		return "", errors.New("mfa secret isn't encrypted")
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode mfa secret: %w", err)
	}
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	if len(b) < g.NonceSize() {
		return "", errors.New("mfa secret is truncated")
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], []byte(userID))
	if err != nil {
		return "", errors.New("mfa secret doesn't decrypt: wrong MFA_ENCRYPTION_KEY?")
	}
	return string(plain), nil
}

// EncryptSecrets seals every TOTP secret still stored in plaintext and returns how many it sealed.
func EncryptSecrets(ctx context.Context, db *pgxpool.Pool, key []byte) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `select user_id::text, secret from admin_mfa where secret not like 'v1:%' for update`)
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
			sealed, err := seal(key, p.userID, p.secret)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `update admin_mfa set secret = $2 where user_id = $1`, p.userID, sealed); err != nil {
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
