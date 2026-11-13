// Package webhook sends a workspace's outbound webhooks: signed JSON POSTs when issues and comments change, queued by
// database triggers (migration 008) and delivered with retries by Run.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), validate.go checks input,
// model.go owns every SQL query and worker.go delivers.
package webhook

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const maxBody = 64 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	const base = "/api/workspaces/{slug}/webhooks"
	mux.HandleFunc("GET "+base, h.wrap(h.list))
	mux.HandleFunc("POST "+base, h.wrap(h.create))
	mux.HandleFunc("POST "+base+"/{id}", h.wrap(h.update))
	mux.HandleFunc("POST "+base+"/{id}/secret", h.wrap(h.rollSecret))
	mux.HandleFunc("POST "+base+"/{id}/test", h.wrap(h.test))
	mux.HandleFunc("DELETE "+base+"/{id}", h.wrap(h.delete))
	mux.HandleFunc("GET "+base+"/{id}/deliveries", h.wrap(h.deliveries))
	mux.HandleFunc("POST "+base+"/{id}/deliveries/{deliveryId}/redeliver", h.wrap(h.redeliver))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
