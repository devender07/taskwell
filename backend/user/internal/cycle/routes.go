// Package cycle implements the user service's cycle API: a project's time-boxed cycles, each with its issues and
// progress, and moving a cycle's open issues to another one (rollover). Issues set their cycle through the issue API.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond)
// and model.go owns every SQL query.
package cycle

import (
	"log/slog"
	"net/http"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/cycles", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/cycles", h.wrap(h.create))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/cycles/{number}", h.wrap(h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/cycles/{number}", h.wrap(h.update))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/projects/{key}/cycles/{number}", h.wrap(h.delete))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/cycles/{number}/rollover", h.wrap(h.rollover))
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
			slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			writeErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		}
	}
}
