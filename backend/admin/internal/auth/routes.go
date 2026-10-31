// Package auth signs platform admins in to the admin API with their normal Tether email and password plus a TOTP
// second factor, into server-side sessions (admin_sessions).
//
// routes.go mounts the endpoints, controller.go handles HTTP, model.go owns every SQL query
// session.go issues and checks the admin cookie, and totp.go is RFC 6238.
package auth

import (
	"net/http"

	"github.com/devender07/tether/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/signin", web.Public(h.signin))
	mux.HandleFunc("POST /api/auth/mfa", web.Public(h.mfa))
	mux.HandleFunc("POST /api/auth/signout", web.Public(h.signout))
	mux.HandleFunc("GET /api/me", web.Private(h.Authenticate, h.me))
}
