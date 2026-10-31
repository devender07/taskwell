// Package notify implements notifications: Record writes them (and new issue watchers) in the transaction of the
// change that causes them, the inbox and email settings API serves them, and Run emails them.
//
// routes.go mounts the endpoints, controller.go handles HTTP, model.go owns every SQL query, rules.go decides who
// gets what and words the emails, and worker.go sends them.
package notify

import (
	"log/slog"
	"net/http"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/notifications", h.wrap(h.list))
	mux.HandleFunc("GET /api/workspaces/{slug}/notifications/unread-count", h.wrap(h.unreadCount))
	mux.HandleFunc("POST /api/workspaces/{slug}/notifications/read", h.wrap(h.markRead))
	mux.HandleFunc("POST /api/workspaces/{slug}/notifications/unread", h.wrap(h.markUnread))
	mux.HandleFunc("GET /api/me/notification-settings", h.wrap(h.settings))
	mux.HandleFunc("POST /api/me/notification-settings", h.wrap(h.updateSettings))
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
