package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	cookieName = "tether_admin"
	issuer     = "tether" // same issuer as the user service; the audience is what keeps the two apart
	audience   = "admin"
	ttl        = 8 * time.Hour
)

// Authenticate returns the user ID from a valid admin cookie, or "" when there is none. It also checks the
// account is still an unsuspended platform admin, so demoting or suspending takes effect on the next request.
func (h *Handler) Authenticate(r *http.Request) (string, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", nil
	}
	secret, err := h.jwtSecret(r.Context())
	if err != nil {
		return "", err
	}
	userID, err := parseToken(secret, c.Value)
	if err != nil {
		return "", nil
	}
	u, err := h.model.admin(r.Context(), userID)
	if err != nil || u == nil {
		return "", err
	}
	return userID, nil
}

func (h *Handler) setCookie(ctx context.Context, w http.ResponseWriter, userID string) error {
	secret, err := h.jwtSecret(ctx)
	if err != nil {
		return err
	}
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	tok, err := signToken(secret, userID, time.Now())
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/api", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	return nil
}

func (h *Handler) clearCookie(ctx context.Context, w http.ResponseWriter) error {
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/api", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	return nil
}

func signToken(secret []byte, userID string, now time.Time) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID,
		Audience:  jwt.ClaimStrings{audience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	})
	s, err := t.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign admin token: %w", err)
	}
	return s, nil
}

// parseToken returns the user ID of a valid admin token: HS256 only, and the "admin" audience is required,
// so the user service's access tokens (same secret, no audience) don't work here.
func parseToken(secret []byte, token string) (string, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", err
	}
	if claims.Subject == "" {
		return "", errors.New("admin token has no subject")
	}
	return claims.Subject, nil
}

func (h *Handler) jwtSecret(ctx context.Context) ([]byte, error) {
	s, err := h.model.setting(ctx, "jwt_secret")
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, errors.New("jwt_secret is empty; start the user service once to generate it")
	}
	return []byte(s), nil
}

func (h *Handler) secure(ctx context.Context) (bool, error) {
	base, err := h.model.setting(ctx, "app_base_url")
	return strings.HasPrefix(base, "https://"), err
}
