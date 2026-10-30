// Package issue implements the user service's issue API: issues inside projects with their sub-issues and
// blocking links, the workspace's statuses and labels, and issue comments and activity.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond)
// and model.go owns every SQL query. doc.go checks rich-text descriptions and comments, events.go diffs changes into
// activity events and collab.go relays live edits. Changes record their notifications with notify.Record.
package issue

import (
	"log/slog"
	"net/http"
)

// maxBody fits a 50,000-character description even when URL-encoded.
const maxBody = 1 << 20

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/statuses", h.wrap(h.listStatuses))
	mux.HandleFunc("POST /api/workspaces/{slug}/statuses", h.wrap(h.createStatus))
	mux.HandleFunc("POST /api/workspaces/{slug}/statuses/order", h.wrap(h.orderStatuses))
	mux.HandleFunc("POST /api/workspaces/{slug}/statuses/{id}", h.wrap(h.updateStatus))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/statuses/{id}", h.wrap(h.deleteStatus))
	mux.HandleFunc("GET /api/workspaces/{slug}/labels", h.wrap(h.listLabels))
	mux.HandleFunc("POST /api/workspaces/{slug}/labels", h.wrap(h.createLabel))
	mux.HandleFunc("POST /api/workspaces/{slug}/labels/{id}", h.wrap(h.updateLabel))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/labels/{id}", h.wrap(h.deleteLabel))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/issues", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/issues", h.wrap(h.create))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues", h.wrap(h.search))
	mux.HandleFunc("GET /api/workspaces/{slug}/search", h.wrap(h.searchAll))
	mux.HandleFunc("GET /api/workspaces/{slug}/my-issues", h.wrap(h.myIssues))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}", h.wrap(h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}", h.wrap(h.update))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/issues/{identifier}", h.wrap(h.delete))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/links", h.wrap(h.link))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/issues/{identifier}/links/{other}", h.wrap(h.unlink))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/watch", h.wrap(h.watch(true)))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/issues/{identifier}/watch", h.wrap(h.watch(false)))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}/timeline", h.wrap(h.timeline))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/comments", h.wrap(h.createComment))
	mux.HandleFunc("POST /api/workspaces/{slug}/comments/{id}", h.wrap(h.updateComment))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/comments/{id}", h.wrap(h.deleteComment))
	// The WebSocket isn't wrapped: its connection is hijacked, so it does its own auth checks before upgrading.
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}/collab", h.collab)
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
