// Package note implements notes: Keep-style notes with a title, a rich-text body (checklists included) and a color,
// which their owner can share with other members. Pin, archive and labels are each member's own.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), validate.go checks input and
// collab.go joins live editing of the body and model.go owns every SQL query.
package note

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// maxBody fits a 20,000-character body with its markup.
const maxBody = 1 << 20

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	const ws = "/api/workspaces/{slug}/notes"
	mux.HandleFunc("GET "+ws, h.wrap(h.list))
	mux.HandleFunc("POST "+ws, h.wrap(h.create))
	mux.HandleFunc("POST "+ws+"/{id}", h.wrap(h.update))
	mux.HandleFunc("DELETE "+ws+"/{id}", h.wrap(h.delete))
	mux.HandleFunc("POST "+ws+"/{id}/members", h.wrap(h.addMember))
	mux.HandleFunc("POST "+ws+"/{id}/members/{userId}", h.wrap(h.setRole))
	mux.HandleFunc("DELETE "+ws+"/{id}/members/{userId}", h.wrap(h.removeMember))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
