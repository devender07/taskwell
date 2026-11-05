package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const issuer = "taskwell"

// claims are an access token's: the user is the subject and SID is the sign-in (refresh token family) it came from.
type claims struct {
	jwt.RegisteredClaims
	SID string `json:"sid,omitempty"`
}

// signAccess returns an HS256 JWT for userID's sign-in sid that expires after ttl.
func signAccess(secret []byte, userID, sid string, ttl time.Duration) (string, error) {
	now := time.Now()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}, SID: sid})
	s, err := t.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return s, nil
}

// parseAccess returns the claims of a valid, unexpired access token. The algorithm is pinned to HS256, and
// tokens with an audience (the admin app's, when it shares the dev secret) are rejected.
func parseAccess(secret []byte, token string) (*claims, error) {
	var claims claims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("access token has no subject")
	}
	if len(claims.Audience) > 0 {
		return nil, fmt.Errorf("access token has an audience (admin token?)")
	}
	return &claims, nil
}

// mfaAudience marks a pending sign-in token (the taskwell_mfa cookie): parseAccess refuses it, as it does any token
// with an audience.
const mfaAudience = "taskwell-mfa"

// mfaClaims are a pending sign-in's: the user is the subject, ID counts its wrong codes, and Remember is the "Remember
// me" choice to start the session with.
type mfaClaims struct {
	jwt.RegisteredClaims
	Remember bool `json:"remember,omitempty"`
}

// signMFA returns a token for a sign-in of userID waiting for its second step, valid for ttl.
func signMFA(secret []byte, userID string, remember bool, ttl time.Duration) (string, error) {
	now := time.Now()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, mfaClaims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID,
		Audience:  jwt.ClaimStrings{mfaAudience},
		ID:        rand.Text(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}, Remember: remember})
	s, err := t.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign mfa token: %w", err)
	}
	return s, nil
}

// parseMFA returns the claims of a valid, unexpired pending sign-in token.
func parseMFA(secret []byte, token string) (*mfaClaims, error) {
	var c mfaClaims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(mfaAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if c.Subject == "" || c.ID == "" {
		return nil, fmt.Errorf("mfa token has no subject or id")
	}
	return &c, nil
}

// EnsureJWTSecret fills an empty jwt_secret (the dev fallback when JWT_SECRET is unset) with 256 random bits. Safe to run from several instances at once.
func EnsureJWTSecret(ctx context.Context, db *pgxpool.Pool) error {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	_, err := db.Exec(ctx, `update system_config set value = $1, updated_at = now() where key = 'jwt_secret' and value = ''`,
		base64.RawStdEncoding.EncodeToString(b))
	if err != nil {
		return fmt.Errorf("ensure jwt secret: %w", err)
	}
	return nil
}
