package aitest

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Test account passwords are stored sealed: "v1:" + base64(nonce || AES-256-GCM ciphertext), with
// "<workspace id>:<account id>" as associated data so a sealed password moved to another row or workspace doesn't open.
// Copied from the admin module's MFA sealing (its internal package can't be imported here).
const sealPrefix = "v1:"

// devSecret stands in for TEST_SECRET_KEY outside production, so dev works without one.
const devSecret = "tether dev test secret, never used in production"

// Key derives the AES-256 key from TEST_SECRET_KEY, which production requires (32+ bytes).
func Key(secret string, prod bool) ([]byte, error) {
	switch {
	case prod && len(secret) < 32:
		return nil, errors.New("APP_ENV=production requires TEST_SECRET_KEY of at least 32 bytes (e.g. openssl rand -base64 48)")
	case secret == "":
		secret = devSecret
	}
	sum := sha256.Sum256([]byte("tether test accounts\x00" + secret))
	return sum[:], nil
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("test secret key: %w", err)
	}
	return cipher.NewGCM(block)
}

func sealAD(workspaceID, accountID string) []byte { return []byte(workspaceID + ":" + accountID) }

// seal encrypts the account's password for storage.
func seal(key []byte, workspaceID, accountID, password string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never returns an error (Go 1.24+)
	return sealPrefix + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(password), sealAD(workspaceID, accountID))), nil
}

// unseal decrypts a stored password; it fails on a wrong key or another row's or workspace's value.
func unseal(key []byte, workspaceID, accountID, stored string) (string, error) {
	raw, ok := strings.CutPrefix(stored, sealPrefix)
	if !ok {
		return "", errors.New("test account password isn't sealed")
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode test account password: %w", err)
	}
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	if len(b) < g.NonceSize() {
		return "", errors.New("test account password is truncated")
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], sealAD(workspaceID, accountID))
	if err != nil {
		return "", errors.New("test account password doesn't decrypt: wrong TEST_SECRET_KEY?")
	}
	return string(plain), nil
}
