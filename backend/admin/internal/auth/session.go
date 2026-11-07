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
	cookieName = "taskwell_admin"
	issuer     = "taskwell" // same issuer as the user service; the audience is what keeps the two apart
	audience   = "admin"
	ttl        = 8 * time.Hour    // a sign-in's absolute lifetime
	idle       = time.Hour        // a sign-in unused this long ends
	mfaTTL     = 10 * time.Minute // to enter the second factor after the password
)

// Authenticate returns the admin ID from a valid admin cookie, or "" when there is none. The cookie names an
// admin_sessions row that must have passed MFA and be under ttl old and idle, so signing out or deleting the admin
// takes effect on the next request. Each call counts as activity.
func (h *Handler) Authenticate(r *http.Request) (string, error) {
	userID, sid, err := h.token(r)
	if sid == "" {
		return "", err
	}
	live, err := h.model.touch(r.Context(), sid, userID, idle)
	if err != nil || !live {
		return "", err
	}
	return userID, nil
}

// token returns the user and session ids of a valid admin cookie; "" when there is none.
func (h *Handler) token(r *http.Request) (userID, sid string, err error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", "", nil
	}
	secret, err := h.jwtSecret(r.Context())
	if err != nil {
		return "", "", err
	}
	userID, sid, err = parseToken(secret, c.Value)
	if err != nil {
		return "", "", nil
	}
	return userID, sid, nil
}

// setCookie sets the cookie for session sid of the user, lasting d.
func (h *Handler) setCookie(ctx context.Context, w http.ResponseWriter, userID, sid string, d time.Duration) error {
	secret, err := h.jwtSecret(ctx)
	if err != nil {
		return err
	}
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	tok, err := signToken(secret, userID, sid, time.Now(), d)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/api", MaxAge: int(d.Seconds()),
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

// signToken returns an admin token for session sid (the JWT id) of the user, expiring after d.
func signToken(secret []byte, userID, sid string, now time.Time, d time.Duration) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID,
		Audience:  jwt.ClaimStrings{audience},
		ID:        sid,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(d)),
	})
	s, err := t.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("sign admin token: %w", err)
	}
	return s, nil
}

// parseToken returns the user and session ids of a valid admin token: HS256 only, and the "admin" audience is
// required, so the user service's access tokens (signed with the same secret in dev) don't work here.
func parseToken(secret []byte, token string) (userID, sid string, err error) {
	var claims jwt.RegisteredClaims
	_, err = jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", "", err
	}
	if claims.Subject == "" || claims.ID == "" {
		return "", "", errors.New("admin token has no subject or session")
	}
	return claims.Subject, claims.ID, nil
}

// jwtSecret is ADMIN_JWT_SECRET, or in dev (when it's unset) the user service's jwt_secret from system_config.
func (h *Handler) jwtSecret(ctx context.Context) ([]byte, error) {
	if h.secret != nil {
		return h.secret, nil
	}
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
