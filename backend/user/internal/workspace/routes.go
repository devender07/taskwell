// Package workspace implements the user service's workspace API: list, create, open, update and delete,
// plus roles, members and invites.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond)
// and model.go owns every SQL query.
package workspace

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces", h.wrap(h.create))
	mux.HandleFunc("GET /api/workspaces/{slug}", h.wrap(h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}", h.wrap(h.update))
	mux.HandleFunc("DELETE /api/workspaces/{slug}", h.wrap(h.remove))
	mux.HandleFunc("GET /api/workspaces/{slug}/members", h.wrap(h.members))
	mux.HandleFunc("POST /api/workspaces/{slug}/members/{userId}", h.wrap(h.changeMember))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/members/{userId}", h.wrap(h.removeMember))
	mux.HandleFunc("GET /api/workspaces/{slug}/roles", h.wrap(h.roles))
	mux.HandleFunc("POST /api/workspaces/{slug}/roles", h.wrap(h.createRole))
	mux.HandleFunc("POST /api/workspaces/{slug}/roles/{id}", h.wrap(h.updateRole))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/roles/{id}", h.wrap(h.deleteRole))
	mux.HandleFunc("GET /api/permissions", h.wrap(h.permissions))
	mux.HandleFunc("POST /api/workspaces/{slug}/invites", h.wrap(h.createInvite))
	mux.HandleFunc("POST /api/workspaces/{slug}/invites/{id}/resend", h.wrap(h.resendInvite))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/invites/{id}", h.wrap(h.cancelInvite))
	mux.HandleFunc("GET /api/invites/{token}", h.wrapPublic(h.showInvite))
	mux.HandleFunc("POST /api/invites/{token}/accept", h.wrap(h.acceptInvite))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }

// wrapPublic is wrap without authentication.
func (h *Handler) wrapPublic(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return web.Public(maxBody, fn)
}
