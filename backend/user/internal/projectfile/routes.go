// Package projectfile implements a project's Files: folders and files uploaded there, and a read-only list of the
// attachments uploaded from the project's issues and comments. Uploads go through the file package.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond) and model.go owns every SQL
// query.
package projectfile

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	const base = "/api/workspaces/{slug}/projects/{key}"
	mux.HandleFunc("GET "+base+"/files", h.wrap(h.list))
	// Upload caps its own body.
	mux.HandleFunc("POST "+base+"/files", web.Wrap(h.authenticate, 0, h.create))
	mux.HandleFunc("POST "+base+"/files/{id}", h.wrap(h.updateFile))
	mux.HandleFunc("DELETE "+base+"/files/{id}", h.wrap(h.deleteFile))
	mux.HandleFunc("POST "+base+"/folders", h.wrap(h.createFolder))
	mux.HandleFunc("POST "+base+"/folders/{id}", h.wrap(h.updateFolder))
	mux.HandleFunc("DELETE "+base+"/folders/{id}", h.wrap(h.deleteFolder))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
