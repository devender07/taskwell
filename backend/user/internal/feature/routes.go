package feature

import (
	"context"
	"net/http"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// Register mounts GET /api/features, public: every flag by name with the plan ids it's on for (the pricing page); and
// GET /api/workspaces/{slug}/features, for a member: every flag for the workspace's plan. It takes the auth handler's
// Authenticate and the workspace handler's Member.
func (f *Flags) Register(mux *http.ServeMux, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) {
	mux.HandleFunc("GET /api/features", func(w http.ResponseWriter, r *http.Request) {
		out := make(map[string][]string, len(names))
		for _, n := range names {
			out[n] = f.Plans(r.Context(), n)
		}
		web.JSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /api/workspaces/{slug}/features", web.Wrap(authenticate, 0, func(w http.ResponseWriter, r *http.Request, userID string) error {
		ws, err := member(r.Context(), userID, r.PathValue("slug"))
		if err != nil {
			return err
		}
		if ws == nil {
			web.Err(w, http.StatusNotFound, "Workspace not found.")
			return nil
		}
		p, err := plan.ForWorkspace(r.Context(), f.db, ws.ID)
		if err != nil {
			return err
		}
		out := make(map[string]bool, len(names))
		for _, n := range names {
			out[n] = f.Enabled(r.Context(), n, p.ID)
		}
		web.JSON(w, http.StatusOK, out)
		return nil
	}))
}
