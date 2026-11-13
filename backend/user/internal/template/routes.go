// Package template implements the user service's issue templates: values that prefill a new issue, and that, when a
// template repeats, create an issue from it in its project every N days, weeks or months (run.go).
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), form.go reads and checks the
// form, and model.go owns every SQL query.
package template

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// maxBody fits a 50,000-character description even when URL-encoded.
const maxBody = 1 << 20

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/templates", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces/{slug}/templates", h.wrap(h.create))
	mux.HandleFunc("POST /api/workspaces/{slug}/templates/{id}", h.wrap(h.update))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/templates/{id}", h.wrap(h.remove))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
