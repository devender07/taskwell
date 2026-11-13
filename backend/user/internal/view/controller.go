package view

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/shared/doc"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNotFound     = "Workspace not found."
	errViewNotFound = "View not found."
	errForbidden    = "You don't have permission to do that."
	errBadPath      = "This page can't be saved as a view."
)

// pathRe is the issue pages a view can be: My issues, a project's, or a cycle's, each with any query.
var pathRe = regexp.MustCompile(`^/(my-issues|projects/[^/?#]+(/cycles/[0-9]+)?)(\?[^#]*)?$`)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return err
	}
	vs, err := h.model.list(r.Context(), ws.ID, userID)
	if err != nil {
		return err
	}
	for i := range vs {
		allow(&vs[i], userID, ws.Permissions)
	}
	web.JSON(w, http.StatusOK, vs)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return err
	}
	name, path, shared, ok := read(w, r, true)
	if !ok {
		return nil
	}
	v, err := h.model.create(r.Context(), ws, userID, *name, *path, shared != nil && *shared)
	if errors.Is(err, errTooMany) {
		web.Err(w, http.StatusBadRequest, "You have 50 views. Delete one first.")
		return nil
	}
	if err != nil {
		return err
	}
	allow(v, userID, ws.Permissions)
	web.JSON(w, http.StatusCreated, v)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, v, ok, err := h.editable(w, r, userID)
	if !ok || err != nil {
		return err
	}
	name, path, shared, ok := read(w, r, false)
	if !ok {
		return nil
	}
	if v, err = h.model.update(r.Context(), ws, userID, v, name, path, shared); err != nil {
		return err
	}
	allow(v, userID, ws.Permissions)
	web.JSON(w, http.StatusOK, v)
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, v, ok, err := h.editable(w, r, userID)
	if !ok || err != nil {
		return err
	}
	if err := h.model.delete(r.Context(), ws, userID, v); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// editable returns the path's workspace and view when the caller may change it, or writes a 404 or 403 and returns
// ok=false.
func (h *Handler) editable(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, *View, bool, error) {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return nil, nil, false, err
	}
	v, err := h.model.get(r.Context(), ws.ID, r.PathValue("id"), userID)
	switch {
	case err != nil:
		return nil, nil, false, err
	case v == nil:
		web.Err(w, http.StatusNotFound, errViewNotFound)
		return nil, nil, false, nil
	}
	if allow(v, userID, ws.Permissions); !v.CanEdit {
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, nil, false, nil
	}
	return ws, v, true, nil
}

// allow sets the caller's can_edit: they own the view, or it's shared and they may edit the workspace.
func allow(v *View, userID string, perms []string) {
	v.CanEdit = v.Owner.ID == userID || v.Shared && workspace.Has(perms, workspace.PermWorkspaceEdit)
}

// read returns the form's name (trimmed), path and shared, nil when absent (all are read on create), or writes a 400
// with field errors and returns ok=false.
func read(w http.ResponseWriter, r *http.Request, create bool) (name, path *string, shared *bool, ok bool) {
	_ = r.ParseMultipartForm(maxBody)
	form := r.PostForm
	errs := map[string]string{}
	if _, has := form["name"]; has || create {
		n := strings.TrimSpace(form.Get("name"))
		name, errs["name"] = &n, checkName(n)
	}
	if _, has := form["path"]; has || create {
		p := form.Get("path")
		path, errs["path"] = &p, checkPath(p)
	}
	if _, has := form["shared"]; has {
		s := form.Get("shared") == "1"
		shared = &s
	}
	return name, path, shared, web.Check(w, errs)
}

func checkName(s string) string {
	switch {
	case s == "":
		return "Enter a name."
	case doc.Len(s) > 60:
		return "Use 60 characters or fewer."
	}
	return ""
}

// checkPath takes a path relative to the workspace, like /projects/WEB?view=board.
func checkPath(s string) string {
	if doc.Len(s) > 2000 || !pathRe.MatchString(s) {
		return errBadPath
	}
	return ""
}

// access returns the path's workspace, or writes a 404 when the caller isn't a member and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	}
	return ws, true, nil
}
