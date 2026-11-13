// Package devreset empties a local database's user data from the admin app, for starting over in development. It
// is mounted only outside production, and refuses unless app_base_url is on localhost.
//
// routes.go mounts the endpoints, controller.go handles HTTP and model.go owns the SQL.
package devreset

import (
	"net/http"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

// Register mounts the API routes on mux. Call it only outside production.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/dev/reset", web.Private(h.auth, h.available))
	mux.HandleFunc("POST /api/dev/reset", web.Private(h.auth, h.reset))
}
