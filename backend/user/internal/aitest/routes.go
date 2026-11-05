// Package aitest implements testing on issues: test cases saved on an issue, manual test runs marked by a person,
// AI test runs, where the tester binary (cmd/tester) has Claude write the cases and drive a sandboxed Chrome
// against the workspace's registered test sites, and replay runs, where the tester replays recorded cases without
// Claude.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond) and model.go owns every SQL
// query. validate.go checks input, seal.go seals test account passwords, playwright.go exports recorded cases as a
// Playwright spec. The tester's side: runner.go (claim loop, case loop), replay.go (recorded scripts), session.go
// (browser sessions MCP clients drive step by step), claude.go
// (requests, tools, prompts), cdp.go (Chrome over the DevTools protocol), proxy.go (the egress proxy Chrome goes
// through) and pricing.go.
package aitest

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const maxBody = 1 << 20

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}/test-cases", h.wrap(h.listCases))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/test-cases", h.wrap(h.createCase))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/test-cases/order", h.wrap(h.orderCases))
	mux.HandleFunc("POST /api/workspaces/{slug}/test-cases/{id}", h.wrap(h.updateCase))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/test-cases/{id}", h.wrap(h.deleteCase))
	mux.HandleFunc("POST /api/workspaces/{slug}/test-cases/{id}/script", h.wrap(h.saveScript))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/test-cases/{id}/script", h.wrap(h.clearScript))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}/test-runs", h.wrap(h.listRuns))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/test-runs", h.wrap(h.createRun))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}/playwright", h.wrap(h.playwright))
	mux.HandleFunc("GET /api/workspaces/{slug}/test-runs/{id}", h.wrap(h.getRun))
	mux.HandleFunc("POST /api/workspaces/{slug}/test-runs/{id}/results/{result_id}", h.wrap(h.markResult))
	mux.HandleFunc("GET /api/workspaces/{slug}/test-runs/{id}/live", h.wrap(h.live))
	mux.HandleFunc("POST /api/workspaces/{slug}/test-runs/{id}/cancel", h.wrap(h.cancelRun))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/browser-sessions", h.wrap(h.createSession))
	mux.HandleFunc("POST /api/workspaces/{slug}/browser-sessions/{id}/actions", h.wrap(h.sessionAction))
	mux.HandleFunc("GET /api/workspaces/{slug}/browser-sessions/{id}/screenshot", h.wrap(h.sessionScreenshot))
	mux.HandleFunc("POST /api/workspaces/{slug}/browser-sessions/{id}/save", h.wrap(h.saveSession))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/browser-sessions/{id}", h.wrap(h.deleteSession))
	mux.HandleFunc("GET /api/workspaces/{slug}/test-sites", h.wrap(h.listSites))
	mux.HandleFunc("POST /api/workspaces/{slug}/test-sites", h.wrap(h.createSite))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/test-sites/{id}", h.wrap(h.deleteSite))
	mux.HandleFunc("GET /api/workspaces/{slug}/test-accounts", h.wrap(h.listAccounts))
	mux.HandleFunc("POST /api/workspaces/{slug}/test-accounts", h.wrap(h.saveAccount))
	mux.HandleFunc("POST /api/workspaces/{slug}/test-accounts/{id}", h.wrap(h.saveAccount))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/test-accounts/{id}", h.wrap(h.deleteAccount))
	mux.HandleFunc("GET /api/workspaces/{slug}/ai-usage", h.wrap(h.usage))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
