package totp

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

// TOTP secrets are stored sealed: "v1:" + base64(nonce || AES-256-GCM ciphertext), with the user's id as associated
// data so a sealed secret can't be moved to another user's row.
const SealPrefix = "v1:"

// Key derives the AES-256 key from MFA_ENCRYPTION_KEY (or, in dev, the jwt_secret fallback).
func Key(secret string) []byte {
	sum := sha256.Sum256([]byte("taskwell admin mfa\x00" + secret))
	return sum[:]
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("mfa key: %w", err)
	}
	return cipher.NewGCM(block)
}

// Seal encrypts the user's TOTP secret for storage.
func Seal(key []byte, userID, secret string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never returns an error (Go 1.24+)
	return SealPrefix + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(secret), []byte(userID))), nil
}

// Unseal decrypts a stored TOTP secret; it fails on a plaintext one, a wrong key or another user's.
func Unseal(key []byte, userID, stored string) (string, error) {
	raw, ok := strings.CutPrefix(stored, SealPrefix)
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
