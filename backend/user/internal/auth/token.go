package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/secret"
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

// NewAPIToken creates a personal API token named name for the user in tx and returns its secret; ErrTooManyTokens at
// the cap. The OAuth token endpoint mints its access tokens with it.
func NewAPIToken(ctx context.Context, tx pgx.Tx, userID, name string) (string, error) {
	tok := apiTokenPrefix + secret.NewToken()
	if _, err := insertAPIToken(ctx, tx, userID, name, secret.Hash(tok)); err != nil {
		return "", err
	}
	return tok, nil
}
