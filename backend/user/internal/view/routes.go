// Package view implements saved views: an issue page's path and query (its filters and display options) saved under
// a name, private to its owner or shared with the workspace.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond) and model.go owns every SQL
// query.
package view

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// maxBody fits a 2,000-character path and a name.
const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	const ws = "/api/workspaces/{slug}"
	mux.HandleFunc("GET "+ws+"/views", h.wrap(h.list))
	mux.HandleFunc("POST "+ws+"/views", h.wrap(h.create))
	mux.HandleFunc("POST "+ws+"/views/{id}", h.wrap(h.update))
	mux.HandleFunc("DELETE "+ws+"/views/{id}", h.wrap(h.delete))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
