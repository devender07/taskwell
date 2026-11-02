package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
)

// newToken returns a 256-bit URL-safe random token (sessions, reset links).
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return base64.RawURLEncoding.EncodeToString(b)
}

// apiTokenPrefix starts every personal API token, so leaked ones are easy to spot and Authenticate can tell them
// from other bearer credentials.
const apiTokenPrefix = "tth_"

// newCode returns a uniformly random 6-digit sign-in code.
func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// hash is what gets stored for any token or code; the raw value never hits the DB.
func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
