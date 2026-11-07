// Package export downloads a workspace's data as a zip of JSON, CSV and its files (docs/workspace-export-plan.md).
//
// routes.go mounts the endpoints, controller.go handles HTTP, token.go signs download links, zip.go writes the zip
// and model.go owns every SQL query.
package export

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// maxBody fits the start request's empty form.
const maxBody = 4 << 10

// Register mounts the API routes on mux. The download is public: its signed token is the credential.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/workspaces/{slug}/export", web.Wrap(h.authenticate, maxBody, h.start))
	mux.HandleFunc("GET /api/exports/{token}", web.Public(maxBody, h.download))
}
