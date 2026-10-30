// Package stats serves the admin overview numbers.
//
// routes.go mounts the endpoint, controller.go handles HTTP and model.go owns every SQL query.
package stats

import (
	"net/http"

	"github.com/devender07/tether/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stats", web.Private(h.auth, h.get))
}
