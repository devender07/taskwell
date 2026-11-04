// Package auth implements the user service's email/password and email-code auth API.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond),
// model.go owns every SQL query, and session.go issues the access/refresh cookies and reads personal API tokens.
package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/signup", h.wrap(h.limitIP(h.signup)))
	mux.HandleFunc("POST /api/auth/signup/verify", h.wrap(h.limitIP(h.signupVerify)))
	mux.HandleFunc("POST /api/auth/signin", h.wrap(h.limitIP(h.signin)))
	mux.HandleFunc("POST /api/auth/otp/send", h.wrap(h.limitIP(h.otpSend)))
	mux.HandleFunc("POST /api/auth/otp/verify", h.wrap(h.limitIP(h.otpVerify)))
	mux.HandleFunc("POST /api/auth/mfa", h.wrap(h.limitIP(h.mfaVerify)))
	mux.HandleFunc("POST /api/auth/password/forgot", h.wrap(h.limitIP(h.forgot)))
	mux.HandleFunc("POST /api/auth/password/reset", h.wrap(h.limitIP(h.reset)))
	mux.HandleFunc("POST /api/auth/password/change", h.wrap(h.limitIP(h.changePassword)))
	// Under /api/auth so the refresh cookie comes along: changing the email keeps this session and ends the others.
	mux.HandleFunc("POST /api/auth/email/code", h.wrap(h.limitIP(h.emailCode)))
	mux.HandleFunc("POST /api/auth/email/change", h.wrap(h.limitIP(h.changeEmail)))
	mux.HandleFunc("POST /api/auth/refresh", h.wrap(h.refresh))
	mux.HandleFunc("POST /api/auth/signout", h.wrap(h.signout))
	mux.HandleFunc("GET /api/auth/google", h.wrap(h.googleStart))
	mux.HandleFunc("GET /api/auth/google/callback", h.wrap(h.googleCallback))
	mux.HandleFunc("GET /api/me", h.wrap(h.me))
	mux.HandleFunc("POST /api/me", h.wrap(h.updateMe))
	mux.HandleFunc("POST /api/me/password/code", h.wrap(h.limitIP(h.passwordCode)))
	mux.HandleFunc("GET /api/me/mfa", h.wrap(h.mfaStatus))
	mux.HandleFunc("POST /api/me/mfa/setup", h.wrap(h.limitIP(h.mfaSetup)))
	mux.HandleFunc("POST /api/me/mfa/enable", h.wrap(h.limitIP(h.mfaEnable)))
	mux.HandleFunc("POST /api/me/mfa/disable", h.wrap(h.limitIP(h.mfaDisable)))
	mux.HandleFunc("POST /api/me/mfa/recovery", h.wrap(h.limitIP(h.mfaRecovery)))
	mux.HandleFunc("GET /api/me/tokens", h.wrap(h.listTokens))
	mux.HandleFunc("POST /api/me/tokens", h.wrap(h.createToken))
	mux.HandleFunc("DELETE /api/me/tokens/{id}", h.wrap(h.deleteToken))
}

// cookieOnly reports whether path needs a cookie sign-in: a personal API token can't sign in, mint more tokens,
// change the password or two-factor.
func cookieOnly(path string) bool {
	return strings.HasPrefix(path, "/api/auth/") || strings.HasPrefix(path, "/api/me/password") ||
		strings.HasPrefix(path, "/api/me/tokens") || strings.HasPrefix(path, "/api/me/mfa")
}

// wrap caps the body size, turns bearer requests away from cookieOnly paths and turns a returned error into a
// logged 500.
func (h *Handler) wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearer(r); ok && cookieOnly(r.URL.Path) {
			web.Err(w, http.StatusForbidden, "Use the Taskwell app for this.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := fn(w, r); err != nil {
			slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			web.Err(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		}
	}
}
