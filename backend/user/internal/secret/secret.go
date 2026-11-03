// Package secret seals values stored in the database: "v1:" + base64(nonce || AES-256-GCM ciphertext), with
// associated data binding the value to its row so a sealed value moved elsewhere doesn't open.
package secret

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

const prefix = "v1:"

// Key derives an AES-256 key for purpose from secret.
func Key(purpose, secret string) []byte {
	sum := sha256.Sum256([]byte(purpose + "\x00" + secret))
	return sum[:]
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plain for storage, bound to ad.
func Seal(key, ad []byte, plain string) (string, error) {
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce) // crypto/rand.Read never returns an error (Go 1.24+)
	return prefix + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), ad)), nil
}

// Open decrypts a sealed value; it fails on a wrong key or another row's value.
func Open(key, ad []byte, stored string) (string, error) {
	raw, ok := strings.CutPrefix(stored, prefix)
	if !ok {
		return "", errors.New("isn't sealed")
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	g, err := gcm(key)
	if err != nil {
		return "", err
	}
	if len(b) < g.NonceSize() {
		return "", errors.New("is truncated")
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], ad)
	if err != nil {
		return "", errors.New("doesn't decrypt: wrong key?")
	}
	return string(plain), nil
}

// NewToken is a 256-bit random token, URL-safe.
func NewToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Hash is the SHA-256 a token is stored as.
func Hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
