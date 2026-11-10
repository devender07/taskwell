package template

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNotFound         = "Workspace not found."
	errTemplateNotFound = "Template not found."
	errForbidden        = "You don't have permission to do that."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	Plans        func(ctx context.Context) []string // when set, the plans the templates feature is on for: others' recurring templates wait
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	ts, err := h.model.list(r.Context(), ws.ID, userID, workspace.Has(ws.Permissions, workspace.PermWorkflowManage))
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, ts)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.save(w, r, userID, "")
}

// update changes the fields sent.
func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errTemplateNotFound)
		return nil
	}
	return h.save(w, r, userID, id)
}

func (h *Handler) save(w http.ResponseWriter, r *http.Request, userID, id string) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	t, err := h.model.save(r.Context(), ws.ID, id, userID, r.PostForm, event(ws, userID))
	var errs fieldErrors
	switch {
	case errors.As(err, &errs):
		web.Fields(w, http.StatusBadRequest, errs)
	case err != nil:
		return err
	case t == nil:
		web.Err(w, http.StatusNotFound, errTemplateNotFound)
	case id == "":
		web.JSON(w, http.StatusCreated, t)
	default:
		web.JSON(w, http.StatusOK, t)
	}
	return nil
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	found := false
	if web.IsUUID(id) {
		if found, err = h.model.remove(r.Context(), ws.ID, id, userID, event(ws, userID)); err != nil {
			return err
		}
	}
	if !found {
		web.Err(w, http.StatusNotFound, errTemplateNotFound)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// access returns the path's workspace. It writes a 404 when the caller isn't a member, or a 403 when manage is set
// and they lack workflow.manage, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string, manage bool) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	case manage && !workspace.Has(ws.Permissions, workspace.PermWorkflowManage):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

func event(ws *workspace.Workspace, userID string) realtime.Event {
	return realtime.Event{Workspace: ws.Slug, Actor: userID, Audience: realtime.ToWorkspace(ws.ID)}
}
