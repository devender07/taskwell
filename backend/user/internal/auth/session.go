package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/secret"
)

const (
	accessCookie  = "tether_access"
	refreshCookie = "tether_refresh"
	googleCookie  = "tether_google" // state, PKCE verifier and remember flag for one Google round trip
	signupCookie  = "tether_signup" // the nonce that binds a sign-up's code to the browser that signed up
)

// remember reads the "Remember me" checkbox; anything but an explicit yes means a short, browser-session sign-in.
func remember(r *http.Request) bool {
	switch r.FormValue("remember") {
	case "true", "on", "1":
		return true
	}
	return false
}

// Authenticate returns the user ID from a valid access token cookie, or "" when there is none or its sign-in has
// ended (signed out, password reset or changed elsewhere, user suspended). The sign-in's state is cached for
// aliveTTL; Forget clears it when a revocation is published. A request with a Bearer header is signed in by that
// personal API token alone, never the cookie.
func (h *Handler) Authenticate(r *http.Request) (string, error) {
	if tok, ok := bearer(r); ok {
		if !strings.HasPrefix(tok, apiTokenPrefix) {
			return "", nil
		}
		// ponytail: one indexed lookup per bearer request; cache like alive if MCP traffic shows up in DB load.
		return h.model.apiTokenUser(r.Context(), secret.Hash(tok))
	}
	c, err := h.access(r)
	if c == nil || c.SID == "" { // tokens from before sid existed: the client refreshes and gets one
		return "", err
	}
	if ok, err := h.live(r.Context(), c.Subject, c.SID); !ok {
		return "", err
	}
	return c.Subject, nil
}

// live is Alive through the cache.
func (h *Handler) live(ctx context.Context, userID, sid string) (bool, error) {
	now := time.Now()
	if alive, ok := h.alive.get(userID, sid, now); ok {
		return alive, nil
	}
	alive, err := h.model.alive(ctx, userID, sid)
	if err != nil {
		return false, err
	}
	h.alive.put(userID, sid, alive, now)
	return alive, nil
}

// Forget drops the cached state of the user's sign-ins, so their next request checks again. The realtime listener
// calls it for every session.revoked event, from any instance.
func (h *Handler) Forget(userID string) { h.alive.forget(userID, time.Now()) }

// bearer returns the request's Authorization: Bearer credential; ok is false when there is no Bearer header.
func bearer(r *http.Request) (tok string, ok bool) {
	scheme, tok, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	return strings.TrimSpace(tok), strings.EqualFold(scheme, "Bearer")
}

// access returns the claims of a valid access token cookie, or nil when there is none.
func (h *Handler) access(r *http.Request) (*claims, error) {
	c, err := r.Cookie(accessCookie)
	if err != nil {
		return nil, nil
	}
	key, err := h.jwtSecret(r.Context())
	if err != nil {
		return nil, err
	}
	cl, err := parseAccess(key, c.Value)
	if err != nil {
		return nil, nil // expired or forged: the client refreshes or signs in again
	}
	return cl, nil
}

// Session is Authenticate for the realtime socket, which outlives single requests: the access token must name a
// sign-in that is still live (not signed out, reset or suspended), and the socket ends when the token expires.
// The zero Session means not signed in. Cookie only: personal API tokens can't open it.
func (h *Handler) Session(r *http.Request) (realtime.Session, error) {
	c, err := h.access(r)
	if c == nil || c.SID == "" { // tokens from before sid existed: the client refreshes and gets one
		return realtime.Session{}, err
	}
	s := realtime.Session{UserID: c.Subject, ID: c.SID, Expires: c.ExpiresAt.Time}
	if ok, err := h.Alive(r.Context(), s); !ok {
		return realtime.Session{}, err
	}
	return s, nil
}

// Alive reports whether the sign-in still exists and its user isn't suspended.
func (h *Handler) Alive(ctx context.Context, s realtime.Session) (bool, error) {
	return h.model.alive(ctx, s.UserID, s.ID)
}

// errSuspended is returned by startSession for a suspended account; callers tell the user errSuspendedMsg.
var errSuspended = errors.New("account suspended")

const errSuspendedMsg = "This account is suspended."

// startSession begins a new sign-in that lasts refresh_ttl_hours with "Remember me", short_refresh_ttl_hours without.
// Every sign-in method goes through here, so it is where suspended accounts are turned away.
func (h *Handler) startSession(ctx context.Context, w http.ResponseWriter, userID string, remember bool) error {
	if suspended, err := h.model.suspended(ctx, userID); err != nil {
		return err
	} else if suspended {
		return errSuspended
	}
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
	key, err := h.jwtSecret(ctx)
	if err != nil {
		return err
	}
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	refresh := secret.NewToken()
	sid, err := h.model.insertRefresh(ctx, secret.Hash(refresh), userID, f)
	if err != nil {
		return err
	}
	accessTTL := time.Duration(accessMinutes) * time.Minute
	access, err := signAccess(key, userID, sid, accessTTL)
	if err != nil {
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

// jwtSecret is JWT_SECRET, or in dev (when it's unset) system_config's generated jwt_secret.
func (h *Handler) jwtSecret(ctx context.Context) ([]byte, error) {
	if h.secret != nil {
		return h.secret, nil
	}
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
