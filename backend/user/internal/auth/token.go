package auth

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

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
