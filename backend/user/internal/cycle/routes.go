// Package cycle implements the user service's cycle API: a project's time-boxed cycles, each with its issues and
// progress, and moving a cycle's open issues to another one (rollover). Issues set their cycle through the issue API.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond)
// and model.go owns every SQL query.
package cycle

import (
	"net/http"

	"github.com/devender07/tether/backend/user/internal/web"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/cycles", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/cycles", h.wrap(h.create))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/cycles/{number}", h.wrap(h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/cycles/{number}", h.wrap(h.update))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/projects/{key}/cycles/{number}", h.wrap(h.delete))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/cycles/{number}/rollover", h.wrap(h.rollover))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
