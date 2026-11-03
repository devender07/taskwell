// Package oauth lets MCP clients get a personal API token without copying one: OAuth 2.1 authorization code with
// PKCE (S256), dynamic client registration and authorization server metadata (docs/mcp-oauth-plan.md).
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), validate.go checks client
// input and model.go owns every SQL query.
package oauth

import (
	"net/http"

	"github.com/devender07/tether/backend/user/internal/web"
)

const maxBody = 64 << 10

// Register mounts the routes on mux. Clients call register, token and the metadata without a session; the consent
// page's authorize calls need one.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", web.Public(maxBody, h.metadata))
	mux.HandleFunc("POST /api/oauth/register", web.Public(maxBody, h.register))
	mux.HandleFunc("GET /api/oauth/authorize", web.Wrap(h.signedIn, maxBody, h.check))
	mux.HandleFunc("POST /api/oauth/authorize", web.Wrap(h.signedIn, maxBody, h.authorize))
	mux.HandleFunc("POST /api/oauth/token", web.Public(maxBody, h.token))
}

// signedIn is authenticate for cookie sessions only: a personal API token can't mint another.
func (h *Handler) signedIn(r *http.Request) (string, error) {
	if r.Header.Get("Authorization") != "" {
		return "", nil
	}
	return h.authenticate(r)
}
