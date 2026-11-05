// Package project implements the user service's project API: list, create, open, update, archive and unarchive,
// a private project's members, milestones, project updates (written, with a health), each active project's issue
// counts for Home, and the roadmap. A private project is seen only by its members and workspace admins (the
// can_see_project SQL function).
//
// routes.go mounts the endpoints, controller.go and updates.go handle HTTP (parse, validate, respond)
// and model.go owns every SQL query.
package project

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
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
	mux.HandleFunc("DELETE /api/workspaces/{slug}/projects/{key}", h.wrap(h.remove))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/archive", h.wrap(h.archive))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/unarchive", h.wrap(h.unarchive))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/people", h.wrap(h.people))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/members", h.wrap(h.addMembers))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/projects/{key}/members/{userId}", h.wrap(h.removeMember))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/milestones", h.wrap(h.milestones))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/milestones", h.wrap(h.createMilestone))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/milestones/{id}", h.wrap(h.updateMilestone))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/projects/{key}/milestones/{id}", h.wrap(h.deleteMilestone))
	mux.HandleFunc("GET /api/workspaces/{slug}/roadmap", h.wrap(h.roadmap))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/updates", h.wrap(h.updates))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/updates", h.wrap(h.createUpdate))
	mux.HandleFunc("POST /api/workspaces/{slug}/project-updates/{id}", h.wrap(h.editUpdate))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/project-updates/{id}", h.wrap(h.deleteUpdate))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
