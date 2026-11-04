// Package issue implements the user service's issue API: issues inside projects with their sub-issues and
// blocking links, the workspace's statuses and labels, and issue comments and activity.
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond)
// and model.go owns every SQL query. doc.go checks rich-text descriptions and comments, events.go diffs changes into
// activity events, rank.go computes manual-order ranks and collab.go relays live edits (joined over the realtime socket). Changes record their notifications with notify.Record.
package issue

import (
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/web"
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
	mux.HandleFunc("GET /api/workspaces/{slug}/status-types", h.wrap(h.listTypes))
	mux.HandleFunc("POST /api/workspaces/{slug}/status-types", h.wrap(h.createType))
	mux.HandleFunc("POST /api/workspaces/{slug}/status-types/{id}", h.wrap(h.updateType))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/status-types/{id}", h.wrap(h.deleteType))
	mux.HandleFunc("GET /api/workspaces/{slug}/labels", h.wrap(h.listLabels))
	mux.HandleFunc("POST /api/workspaces/{slug}/labels", h.wrap(h.createLabel))
	mux.HandleFunc("POST /api/workspaces/{slug}/labels/{id}", h.wrap(h.updateLabel))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/labels/{id}", h.wrap(h.deleteLabel))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/issues", h.wrap(h.list))
	mux.HandleFunc("POST /api/workspaces/{slug}/projects/{key}/issues", h.wrap(h.create))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues", h.wrap(h.search))
	mux.HandleFunc("GET /api/workspaces/{slug}/search", h.wrap(h.searchAll))
	mux.HandleFunc("GET /api/workspaces/{slug}/my-issues", h.wrap(h.myIssues))
	mux.HandleFunc("GET /api/workspaces/{slug}/watching", h.wrap(h.watching))
	mux.HandleFunc("GET /api/workspaces/{slug}/projects/{key}/cycles/{number}/issues", h.wrap(h.cycleIssues))
	mux.HandleFunc("GET /api/workspaces/{slug}/activity", h.wrap(h.activity))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/bulk", h.wrap(h.bulk))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}", h.wrap(h.get))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}", h.wrap(h.update))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/issues/{identifier}", h.wrap(h.delete))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/move", h.wrap(h.move))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/links", h.wrap(h.link))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/issues/{identifier}/links/{other}", h.wrap(h.unlink))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/watch", h.wrap(h.watch(true)))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/issues/{identifier}/watch", h.wrap(h.watch(false)))
	mux.HandleFunc("GET /api/workspaces/{slug}/issues/{identifier}/timeline", h.wrap(h.timeline))
	mux.HandleFunc("POST /api/workspaces/{slug}/issues/{identifier}/comments", h.wrap(h.createComment))
	mux.HandleFunc("POST /api/workspaces/{slug}/import/csv", web.Wrap(h.authenticate, maxImportFile+1<<20, h.importCSV))
	mux.HandleFunc("POST /api/workspaces/{slug}/import/github", h.wrap(h.importGitHub))
	mux.HandleFunc("POST /api/workspaces/{slug}/comments/{id}", h.wrap(h.updateComment))
	mux.HandleFunc("DELETE /api/workspaces/{slug}/comments/{id}", h.wrap(h.deleteComment))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
