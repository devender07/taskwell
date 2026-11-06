// Package file implements the user service's uploads: attachments for descriptions and comments, avatars,
// and (through Upload) a project's files; and the workspace's storage use.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), model.go owns every SQL
// query and name.go cleans file names and builds URLs.
package file

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// formOverhead is what a multipart body may add around the file itself.
const formOverhead = 1 << 20

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/workspaces/{slug}/files", h.wrap(h.upload))
	mux.HandleFunc("GET /api/workspaces/{slug}/storage", h.wrap(h.storage))
	mux.HandleFunc("GET /api/files/{id}/{name}", h.wrap(h.serve))
	mux.HandleFunc("POST /api/me/avatar", h.wrap(h.setAvatar))
	mux.HandleFunc("DELETE /api/me/avatar", h.wrap(h.removeAvatar))
	mux.HandleFunc("GET /api/users/{id}/avatar", h.wrap(h.avatar))
}

// wrap answers 401 when signed out and turns a returned error into a logged 500. Handlers cap their own body size.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, 0, fn) }
