// Package billing is the account plans paid with Paddle: the plan list, an account's billing state, checkout, plan
// changes and the customer portal, a workspace's view of its owner's plan, and Paddle's webhook
// (docs/billing-plan.md). The plans and their limits are in package plan.
//
// routes.go mounts the endpoints, controller.go handles HTTP, paddle.go is the Paddle client and webhook signature
// check, and model.go owns every SQL query.
package billing

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	maxBody    = 4 << 10
	maxWebhook = 1 << 20
)

// Register mounts the API routes on mux. The plan list and the webhook are public; the webhook's signature is its
// credential.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/billing/plans", web.Public(maxBody, h.plans))
	mux.HandleFunc("POST /api/billing/webhook", web.Public(maxWebhook, h.webhook))
	mux.HandleFunc("GET /api/billing", web.Wrap(h.authenticate, maxBody, h.state))
	mux.HandleFunc("POST /api/billing/checkout", web.Wrap(h.authenticate, maxBody, h.checkout))
	mux.HandleFunc("POST /api/billing/change", web.Wrap(h.authenticate, maxBody, h.change))
	mux.HandleFunc("POST /api/billing/portal", web.Wrap(h.authenticate, maxBody, h.portal))
	mux.HandleFunc("GET /api/workspaces/{slug}/billing", web.Wrap(h.authenticate, maxBody, h.workspace))
}
