// Package config lets admins read and edit system_config. Secret values never leave the server.
//
// routes.go mounts the endpoints, controller.go handles HTTP, model.go owns every SQL query
// and validate.go checks values of keys with a known format.
package config

import (
	"net/http"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/config", web.Private(h.auth, h.list))
	mux.HandleFunc("POST /api/config/{key}", web.Private(h.auth, h.update))
}
