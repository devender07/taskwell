// Package project implements the user service's project API: list, create, open, update, archive and unarchive,
// a private project's members, plus each active project's issue counts for Home. A private project is seen only by
// its members and workspace admins (the can_see_project SQL function).
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond)
// and model.go owns every SQL query.
package project

import (
	"net/http"

	"github.com/devender07/tether/backend/user/internal/web"
)

// maxBody fits a 10,000-character description even when URL-encoded.
const maxBody = 128 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/projects", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects", h.wrap(h.create))
	mux.HandleFunc("GET /api/workspaces/{slug}/project-stats", h.wrap(h.stats))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}", h.wrap(h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}", h.wrap(h.update))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/archive", h.wrap(h.archive))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/unarchive", h.wrap(h.unarchive))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/people", h.wrap(h.people))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/members", h.wrap(h.addMembers))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/projects/{key}/members/{userId}", h.wrap(h.removeMember))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
