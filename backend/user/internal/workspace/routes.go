// Package workspace implements the user service's workspace API: list, create, open, update and delete.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond)
// and model.go owns every SQL query.
package workspace

import (
	"log/slog"
	"net/http"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces", h.wrap(h.create))
	mux.HandleFunc("GET /api/workspaces/{slug}", h.wrap(h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}", h.wrap(h.update))
	mux.HandleFunc("DELETE /api/workspaces/{slug}", h.wrap(h.remove))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn func(w http.ResponseWriter, r *http.Request, userID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		userID, err := h.authenticate(r)
		if err == nil && userID == "" {
			writeErr(w, http.StatusUnauthorized, "Not signed in.")
			return
		}
		if err == nil {
			err = fn(w, r, userID)
		}
		if err != nil {
			slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			writeErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		}
	}
}
