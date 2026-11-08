// Package directory lists workspaces and users for admins, sets (comps) a user's plan, and suspends or unsuspends
// users.
//
// routes.go mounts the endpoints, controller.go handles HTTP, paddle.go reads a subscription's plan and model.go owns
// every SQL query.
package directory

import (
	"net/http"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces", web.Private(h.auth, h.workspaces))
	mux.HandleFunc("GET /api/users", web.Private(h.auth, h.users))
	mux.HandleFunc("POST /api/users/{id}/suspend", web.Private(h.auth, h.suspend))
	mux.HandleFunc("POST /api/users/{id}/unsuspend", web.Private(h.auth, h.unsuspend))
	mux.HandleFunc("POST /api/users/{id}/plan", web.Private(h.auth, h.setPlan))
}
