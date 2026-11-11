package onboarding

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNoWorkspace = "Workspace not found."
	errForbidden   = "You don't have permission to do that."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	integrations func(ctx context.Context, planID string) bool
}

// New takes the auth handler's Authenticate, the workspace handler's Member and whether the integrations feature is
// on for a plan.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error),
	integrations func(ctx context.Context, planID string) bool) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member, integrations: integrations}
}

type step struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
}

// get answers the checklist: invite is left out when the plan allows one member, github when integrations are off.
func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.workspace(w, r, userID)
	if ws == nil || err != nil {
		return err
	}
	ctx := r.Context()
	s, err := h.model.state(ctx, ws.ID)
	if err != nil {
		return err
	}
	p, err := plan.ForWorkspace(ctx, h.model.db, ws.ID)
	if err != nil {
		return err
	}
	steps := []step{{"project", s.project}}
	if p.Members != 1 {
		steps = append(steps, step{"invite", s.invite})
	}
	steps = append(steps, step{"issue", s.issue})
	if h.integrations(ctx, p.ID) {
		steps = append(steps, step{"github", s.github})
	}
	web.JSON(w, http.StatusOK, map[string]any{"dismissed": s.dismissed, "steps": steps})
	return nil
}

// dismiss hides the checklist for everyone in the workspace; it needs the permission to edit the workspace.
func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.workspace(w, r, userID)
	if ws == nil || err != nil {
		return err
	}
	if !workspace.Has(ws.Permissions, workspace.PermWorkspaceEdit) {
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil
	}
	if err := h.model.dismiss(r.Context(), ws, userID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// workspace returns the path's workspace, or writes a 404 (not a member) or 403 (a guest) and returns nil.
func (h *Handler) workspace(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err == nil && ws == nil {
		web.Err(w, http.StatusNotFound, errNoWorkspace)
	}
	if ws != nil && workspace.DenyGuest(w, ws) {
		return nil, nil
	}
	return ws, err
}
