// Package gitlink connects a workspace to GitHub and GitLab through pasted webhooks: pull requests (merge requests)
// that mention an issue identifier are linked to that issue, and opening or merging one can move it.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), model.go owns every SQL
// query and parse.go reads identifiers and provider payloads.
package gitlink

import (
	"log/slog"
	"net/http"
)

const (
	maxBody = 64 << 10
	// maxDelivery caps a webhook delivery's body.
	maxDelivery = 1 << 20
)

// Register mounts the API routes on mux. The webhooks skip sign-in: GitHub's are signed, GitLab's carry the secret.
// ponytail: no rate limit on the webhooks; add per-token limiting if a hook gets abused.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/integrations", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces/{slug}/integrations/{provider}", h.wrap(h.connect))
	mux.HandleFunc("POST /api/workspaces/{slug}/integrations/{provider}/settings", h.wrap(h.settings))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/integrations/{provider}", h.wrap(h.disconnect))
	mux.HandleFunc("POST /api/hooks/github/{token}", h.hook(github))
	mux.HandleFunc("POST /api/hooks/gitlab/{token}", h.hook(gitlab))
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
