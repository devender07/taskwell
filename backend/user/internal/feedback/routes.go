// Package feedback is the workspace's customer feedback inbox: feedback sent through the public form or added by a
// member, linked to an issue or archived.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), validate.go checks input and
// model.go owns every SQL query.
package feedback

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

// maxBody fits a 5,000-character feedback with its email and name.
const maxBody = 64 << 10

// Register mounts the API routes on mux. The public form's endpoints skip sign-in; its POST is rate limited per IP.
func (h *Handler) Register(mux *http.ServeMux) {
	const ws = "/api/workspaces/{slug}"
	mux.HandleFunc("GET "+ws+"/feedback", h.wrap(h.list))
	mux.HandleFunc("GET "+ws+"/feedback/count", h.wrap(h.count))
	mux.HandleFunc("POST "+ws+"/feedback", h.wrap(h.create))
	mux.HandleFunc("POST "+ws+"/feedback/{id}", h.wrap(h.update))
	mux.HandleFunc("DELETE "+ws+"/feedback/{id}", h.wrap(h.delete))
	mux.HandleFunc("GET "+ws+"/issues/{identifier}/feedback", h.wrap(h.issueFeedback))
	mux.HandleFunc("GET "+ws+"/feedback-form", h.wrap(h.form))
	mux.HandleFunc("POST "+ws+"/feedback-form", h.wrap(h.setForm))
	mux.HandleFunc("GET /api/feedback-forms/{token}", web.Public(maxBody, h.publicForm))
	mux.HandleFunc("POST /api/feedback-forms/{token}", web.Public(maxBody, h.submit))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
