package workspace

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// newToken returns a 256-bit URL-safe random token for invite links.
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return base64.RawURLEncoding.EncodeToString(b)
}

// hash is what gets stored for a token; the raw value never hits the DB.
func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
