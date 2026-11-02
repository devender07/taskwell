// Package auth implements the user service's email/password and email-code auth API.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond),
// model.go owns every SQL query, and session.go issues the access/refresh cookies and reads personal API tokens.
package auth

import (
	"log/slog"
	"net/http"
	"strings"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/signup", h.wrap(h.limitIP(h.signup)))
	mux.HandleFunc("POST /api/auth/signup/verify", h.wrap(h.limitIP(h.signupVerify)))
	mux.HandleFunc("POST /api/auth/signin", h.wrap(h.limitIP(h.signin)))
	mux.HandleFunc("POST /api/auth/otp/send", h.wrap(h.limitIP(h.otpSend)))
	mux.HandleFunc("POST /api/auth/otp/verify", h.wrap(h.limitIP(h.otpVerify)))
	mux.HandleFunc("POST /api/auth/password/forgot", h.wrap(h.limitIP(h.forgot)))
	mux.HandleFunc("POST /api/auth/password/reset", h.wrap(h.limitIP(h.reset)))
	mux.HandleFunc("POST /api/auth/password/change", h.wrap(h.limitIP(h.changePassword)))
	mux.HandleFunc("POST /api/auth/refresh", h.wrap(h.refresh))
	mux.HandleFunc("POST /api/auth/signout", h.wrap(h.signout))
	mux.HandleFunc("GET /api/auth/google", h.wrap(h.googleStart))
	mux.HandleFunc("GET /api/auth/google/callback", h.wrap(h.googleCallback))
	mux.HandleFunc("GET /api/me", h.wrap(h.me))
	mux.HandleFunc("POST /api/me", h.wrap(h.updateMe))
	mux.HandleFunc("POST /api/me/password/code", h.wrap(h.limitIP(h.passwordCode)))
	mux.HandleFunc("GET /api/me/tokens", h.wrap(h.listTokens))
	mux.HandleFunc("POST /api/me/tokens", h.wrap(h.createToken))
	mux.HandleFunc("DELETE /api/me/tokens/{id}", h.wrap(h.deleteToken))
}

// cookieOnly reports whether path needs a cookie sign-in: a personal API token can't sign in, mint more tokens or
// change the password.
func cookieOnly(path string) bool {
	return strings.HasPrefix(path, "/api/auth/") || strings.HasPrefix(path, "/api/me/password") ||
		strings.HasPrefix(path, "/api/me/tokens")
}

// wrap caps the body size, turns bearer requests away from cookieOnly paths and turns a returned error into a
// logged 500.
func (h *Handler) wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearer(r); ok && cookieOnly(r.URL.Path) {
			writeErr(w, http.StatusForbidden, "Use the Tether app for this.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := fn(w, r); err != nil {
			slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			writeErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		}
	}
}
