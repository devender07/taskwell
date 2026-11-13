// Package auth signs admins in to the admin API with their own account (the admins table, apart from app users)
// and password plus a TOTP second factor, into server-side sessions (admin_sessions), and lets admins add and remove
// admin accounts (admins.go).
//
// routes.go mounts the endpoints, controller.go handles HTTP, model.go owns every SQL query
// session.go issues and checks the admin cookie, and TOTP comes from shared/totp.
package auth

import (
	"net/http"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/signin", web.Public(h.signin))
	mux.HandleFunc("POST /api/auth/mfa", web.Public(h.mfa))
	mux.HandleFunc("POST /api/auth/signout", web.Public(h.signout))
	mux.HandleFunc("GET /api/me", web.Private(h.Authenticate, h.me))
	mux.HandleFunc("GET /api/admins", web.Private(h.Authenticate, h.listAdmins))
	mux.HandleFunc("POST /api/admins", web.Private(h.Authenticate, h.createAdmin))
	mux.HandleFunc("POST /api/admins/{id}/remove", web.Private(h.Authenticate, h.removeAdmin))
}
