// Package auth signs platform admins in to the admin API with their normal Tether email and password.
//
// routes.go mounts the endpoints, controller.go handles HTTP, model.go owns every SQL query
// and session.go issues and checks the admin JWT cookie.
package auth

import (
	"net/http"

	"github.com/devender07/tether/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/signin", web.Public(h.signin))
	mux.HandleFunc("POST /api/auth/signout", web.Public(h.signout))
	mux.HandleFunc("GET /api/me", web.Private(h.Authenticate, h.me))
}
