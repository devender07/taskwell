package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	accessCookie  = "tether_access"
	refreshCookie = "tether_refresh"
	googleCookie  = "tether_google" // state, PKCE verifier and remember flag for one Google round trip
)

// remember reads the "Remember me" checkbox; anything but an explicit yes means a short, browser-session sign-in.
func remember(r *http.Request) bool {
	switch r.FormValue("remember") {
	case "true", "on", "1":
		return true
	}
	return false
}

// Authenticate returns the user ID from a valid access token cookie, or "" when there is none.
func (h *Handler) Authenticate(r *http.Request) (string, error) {
	c, err := r.Cookie(accessCookie)
	if err != nil {
		return "", nil
	}
	secret, err := h.jwtSecret(r.Context())
	if err != nil {
		return "", err
	}
	userID, err := parseAccess(secret, c.Value)
	if err != nil {
		return "", nil // expired or forged: the client refreshes or signs in again
	}
	return userID, nil
}

// startSession begins a new sign-in that lasts refresh_ttl_hours with "Remember me", short_refresh_ttl_hours without.
func (h *Handler) startSession(ctx context.Context, w http.ResponseWriter, userID string, remember bool) error {
	key := "short_refresh_ttl_hours"
	if remember {
		key = "refresh_ttl_hours"
	}
	hours, err := h.cfg.GetInt(ctx, key)
	if err != nil {
		return err
	}
	return h.setTokens(ctx, w, userID, family{remember: remember, expiresAt: time.Now().Add(time.Duration(hours) * time.Hour)})
}

// setTokens sets an access token and a new refresh token for family f as cookies. Without "Remember me" both are
// browser-session cookies. The refresh cookie is scoped to /api/auth so it only travels to refresh and sign-out.
func (h *Handler) setTokens(ctx context.Context, w http.ResponseWriter, userID string, f family) error {
	accessMinutes, err := h.cfg.GetInt(ctx, "access_ttl_minutes")
	if err != nil {
		return err
	}
	secret, err := h.jwtSecret(ctx)
	if err != nil {
		return err
	}
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	accessTTL := time.Duration(accessMinutes) * time.Minute
	access, err := signAccess(secret, userID, accessTTL)
	if err != nil {
		return err
	}
	refresh := newToken()
	if err := h.model.insertRefresh(ctx, hash(refresh), userID, f); err != nil {
		return err
	}
	accessAge, refreshAge := 0, 0 // 0 = no Max-Age: the browser drops the cookie when it closes
	if f.remember {
		accessAge, refreshAge = int(accessTTL.Seconds()), int(time.Until(f.expiresAt).Seconds())
	}
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: access, Path: "/api", MaxAge: accessAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: refresh, Path: "/api/auth", MaxAge: refreshAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	return nil
}

func (h *Handler) clearCookies(ctx context.Context, w http.ResponseWriter) error {
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	for name, path := range map[string]string{accessCookie: "/api", refreshCookie: "/api/auth"} {
		http.SetCookie(w, &http.Cookie{Name: name, Path: path, MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	}
	return nil
}

func (h *Handler) jwtSecret(ctx context.Context) ([]byte, error) {
	s, err := h.cfg.Get(ctx, "jwt_secret")
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, errors.New("jwt_secret is empty")
	}
	return []byte(s), nil
}

func (h *Handler) secure(ctx context.Context) (bool, error) {
	base, err := h.cfg.Get(ctx, "app_base_url")
	return strings.HasPrefix(base, "https://"), err
}
