package aitest

import (
	"errors"
	"fmt"

	"github.com/devender07/taskwell/backend/user/internal/secret"
)

// Test account passwords are stored sealed (see the secret package), with "<workspace id>:<account id>" as
// associated data so a sealed password moved to another row or workspace doesn't open.

// devSecret stands in for TEST_SECRET_KEY outside production, so dev works without one.
const devSecret = "taskwell dev test secret, never used in production"

// Key derives the AES-256 key from TEST_SECRET_KEY, which production requires (32+ bytes).
func Key(s string, prod bool) ([]byte, error) {
	switch {
	case prod && len(s) < 32:
		return nil, errors.New("APP_ENV=production requires TEST_SECRET_KEY of at least 32 bytes (e.g. openssl rand -base64 48)")
	case s == "":
		s = devSecret
	}
	return secret.Key("taskwell test accounts", s), nil
}

func sealAD(workspaceID, accountID string) []byte { return []byte(workspaceID + ":" + accountID) }

// seal encrypts the account's password for storage.
func seal(key []byte, workspaceID, accountID, password string) (string, error) {
	return secret.Seal(key, sealAD(workspaceID, accountID), password)
}

// unseal decrypts a stored password; it fails on a wrong key or another row's or workspace's value.
func unseal(key []byte, workspaceID, accountID, stored string) (string, error) {
	p, err := secret.Open(key, sealAD(workspaceID, accountID), stored)
	if err != nil {
		return "", fmt.Errorf("test account password %w (TEST_SECRET_KEY)", err)
	}
	return p, nil
}
