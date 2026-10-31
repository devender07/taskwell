// Package file implements the user service's uploads: attachments for descriptions and comments, and avatars.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), model.go owns every SQL
// query and name.go cleans file names and builds URLs.
package file

import (
	"log/slog"
	"net/http"
)

// formOverhead is what a multipart body may add around the file itself.
const formOverhead = 1 << 20

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/workspaces/{slug}/files", h.wrap(h.upload))
	mux.HandleFunc("GET /api/files/{id}/{name}", h.wrap(h.serve))
	mux.HandleFunc("POST /api/me/avatar", h.wrap(h.setAvatar))
	mux.HandleFunc("DELETE /api/me/avatar", h.wrap(h.removeAvatar))
	mux.HandleFunc("GET /api/users/{id}/avatar", h.wrap(h.avatar))
}

// wrap answers 401 when signed out and turns a returned error into a logged 500. Handlers cap their own body size.
func (h *Handler) wrap(fn func(w http.ResponseWriter, r *http.Request, userID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := h.authenticate(r)
		if err == nil && userID == "" {
			writeErr(w, http.StatusUnauthorized, "Not signed in.")
			return
		}
		if err == nil {
			err = fn(w, r, userID)
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			writeErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		}
	}
}
