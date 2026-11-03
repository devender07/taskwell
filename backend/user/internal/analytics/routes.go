// Package analytics is a member's workload and how their work moved over the last 12 weeks, and the team's at a
// glance. Every number counts only issues in projects the viewer can see.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, check access, respond) and model.go owns every
// SQL query.
package analytics

import (
	"net/http"

	"github.com/devender07/tether/backend/user/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	const ws = "/api/workspaces/{slug}"
	mux.HandleFunc("GET "+ws+"/members/{id}/analytics", h.wrap(h.member))
	mux.HandleFunc("GET "+ws+"/analytics/team", h.wrap(h.team))
}

// wrap answers 401 when signed out and turns a returned error into a logged 500. Both endpoints are GETs, so the
// body cap is small.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, 1<<10, fn) }
