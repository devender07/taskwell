// Package defaults lets admins edit what a new workspace starts with: its roles, workflow (status types and
// statuses), labels and issue templates (docs/workspace-defaults-plan.md). Workspaces copy them once, at creation.
//
// routes.go mounts the endpoints, controller.go handles HTTP, model.go owns every SQL query and validate.go holds
// the user API's field rules.
package defaults

import (
	"net/http"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	for path, fn := range map[string]func(http.ResponseWriter, *http.Request, string) error{
		"GET /api/defaults":                      h.list,
		"POST /api/defaults/roles":               h.saveRole,
		"POST /api/defaults/roles/{id}":          h.saveRole,
		"DELETE /api/defaults/roles/{id}":        h.deleteRole,
		"POST /api/defaults/status-types":        h.saveType,
		"POST /api/defaults/status-types/{id}":   h.saveType,
		"DELETE /api/defaults/status-types/{id}": h.deleteType,
		"POST /api/defaults/statuses":            h.saveStatus,
		"POST /api/defaults/statuses/order":      h.reorderStatuses,
		"POST /api/defaults/statuses/{id}":       h.saveStatus,
		"DELETE /api/defaults/statuses/{id}":     h.deleteStatus,
		"POST /api/defaults/labels":              h.saveLabel,
		"POST /api/defaults/labels/{id}":         h.saveLabel,
		"DELETE /api/defaults/labels/{id}":       h.deleteLabel,
		"POST /api/defaults/templates":           h.saveTemplate,
		"POST /api/defaults/templates/{id}":      h.saveTemplate,
		"DELETE /api/defaults/templates/{id}":    h.deleteTemplate,
	} {
		mux.HandleFunc(path, web.Private(h.auth, fn))
	}
}
