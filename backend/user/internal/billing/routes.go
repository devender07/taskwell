// Package billing is the workspace plans paid with Stripe: the plan list, a workspace's billing state, Checkout and
// the customer portal, and Stripe's webhook (docs/billing-plan.md). The plans and their limits are in package plan.
//
// routes.go mounts the endpoints, controller.go handles HTTP, stripe.go is the Stripe client and webhook signature
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
	mux.HandleFunc("GET /api/workspaces/{slug}/billing", web.Wrap(h.authenticate, maxBody, h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}/billing/checkout", web.Wrap(h.authenticate, maxBody, h.checkout))
	mux.HandleFunc("POST /api/workspaces/{slug}/billing/portal", web.Wrap(h.authenticate, maxBody, h.portal))
}
