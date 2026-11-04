// Package audit appends admin actions to the admin_audit log (Record) and lists it for admins.
//
// routes.go mounts the endpoint, controller.go handles HTTP and model.go owns every SQL query.
package audit

import (
	"net/http"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audit", web.Private(h.auth, h.list))
}
