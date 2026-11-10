// Package onboarding is a new workspace's getting started checklist on Home (docs/growth-plan.md): which first steps
// are done, and dismissing it.
//
// routes.go mounts the endpoints, controller.go handles HTTP (check access, respond) and model.go owns every SQL query.
package onboarding

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	const ws = "/api/workspaces/{slug}/onboarding"
	mux.HandleFunc("GET "+ws, h.wrap(h.get))
	mux.HandleFunc("POST "+ws+"/dismiss", h.wrap(h.dismiss))
}

// wrap answers 401 when signed out and turns a returned error into a logged 500. Neither endpoint takes a body.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, 1<<10, fn) }
