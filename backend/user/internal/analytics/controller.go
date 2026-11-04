package analytics

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNoWorkspace     = "Workspace not found."
	errNoMember        = "Member not found."
	errForbiddenMember = "You don't have permission to see this member's analytics."
	errForbiddenTeam   = "You don't have permission to see the team's analytics."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	members      func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, members: member}
}

// member answers a member's analytics: anyone's own, anyone else's with analytics.view.
func (h *Handler) member(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.workspace(w, r, userID)
	if ws == nil || err != nil {
		return err
	}
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errNoMember)
		return nil
	}
	if id != userID && !workspace.Has(ws.Permissions, workspace.PermAnalyticsView) {
		web.Err(w, http.StatusForbidden, errForbiddenMember)
		return nil
	}
	a, err := h.model.member(r.Context(), ws.ID, userID, id)
	if err != nil {
		return err
	}
	if a == nil {
		web.Err(w, http.StatusNotFound, errNoMember)
		return nil
	}
	web.JSON(w, http.StatusOK, a)
	return nil
}

func (h *Handler) team(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.workspace(w, r, userID)
	if ws == nil || err != nil {
		return err
	}
	if !workspace.Has(ws.Permissions, workspace.PermAnalyticsView) {
		web.Err(w, http.StatusForbidden, errForbiddenTeam)
		return nil
	}
	ms, err := h.model.team(r.Context(), ws.ID, userID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"members": ms})
	return nil
}

// workspace returns the path's workspace, or writes a 404 and returns nil when the caller isn't a member.
func (h *Handler) workspace(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, error) {
	ws, err := h.members(r.Context(), userID, r.PathValue("slug"))
	if err == nil && ws == nil {
		web.Err(w, http.StatusNotFound, errNoWorkspace)
	}
	return ws, err
}
