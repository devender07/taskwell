package project

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/web"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	errNotFound        = "Workspace not found."
	errProjectNotFound = "Project not found."
	errForbidden       = "You don't have permission to do that."
	errKeyTaken        = "That key is taken."
	errPickedOutsider  = "Someone you picked isn't in this workspace."
	errPublicMembers   = "Only private projects have members."
)

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
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	ps, err := h.model.list(r.Context(), ws.ID, userID, r.URL.Query().Get("archived") == "true")
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, ps)
	return nil
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	ss, err := h.model.stats(r.Context(), ws.ID, userID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, ss)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	name, key, desc, ok := readForm(w, r)
	if !ok {
		return nil
	}
	p, err := h.model.create(r.Context(), ws.ID, userID, name, key, desc, r.PostForm.Get("private") == "true", ids(r, "member_ids"),
		event(ws, userID))
	if errors.Is(err, errTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"key": errKeyTaken})
		return nil
	}
	if errors.Is(err, errNotMember) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"member_ids": errPickedOutsider})
		return nil
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, p)
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	p, err := h.model.get(r.Context(), ws.ID, normKey(r.PathValue("key")), userID)
	return respond(w, p, err)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	name, key, desc, ok := readForm(w, r)
	if !ok {
		return nil
	}
	var private *bool
	if v, ok := r.PostForm["private"]; ok {
		b := v[0] == "true"
		private = &b
	}
	p, err := h.model.update(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, name, key, desc, private, event(ws, userID))
	if errors.Is(err, errTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"key": errKeyTaken})
		return nil
	}
	return respond(w, p, err)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.setArchived(w, r, userID, true)
}

func (h *Handler) unarchive(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.setArchived(w, r, userID, false)
}

func (h *Handler) setArchived(w http.ResponseWriter, r *http.Request, userID string, archived bool) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	p, err := h.model.setArchived(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, archived, event(ws, userID))
	return respond(w, p, err)
}

// people lists everyone who can see the project, for assignee and mention pickers.
func (h *Handler) people(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	ms, err := h.model.people(r.Context(), ws.ID, normKey(r.PathValue("key")), userID)
	if err != nil {
		return err
	}
	if ms == nil {
		web.Err(w, http.StatusNotFound, errProjectNotFound)
		return nil
	}
	web.JSON(w, http.StatusOK, ms)
	return nil
}

func (h *Handler) addMembers(w http.ResponseWriter, r *http.Request, userID string) error {
	_ = r.ParseMultipartForm(maxBody)
	return h.setMembers(w, r, userID, ids(r, "user_ids"), true)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.setMembers(w, r, userID, []string{r.PathValue("userId")}, false)
}

func (h *Handler) setMembers(w http.ResponseWriter, r *http.Request, userID string, ids []string, add bool) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	p, err := h.model.setMembers(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, ids, add, event(ws, userID))
	switch {
	case errors.Is(err, errNotMember):
		web.Fields(w, http.StatusBadRequest, map[string]string{"user_ids": errPickedOutsider})
		return nil
	case errors.Is(err, errPublic):
		web.Err(w, http.StatusConflict, errPublicMembers)
		return nil
	}
	return respond(w, p, err)
}

// ids returns the form's non-empty values for the repeated field, once each.
func ids(r *http.Request, field string) []string {
	return slices.Compact(slices.Sorted(slices.Values(slices.DeleteFunc(slices.Clone(r.PostForm[field]), func(s string) bool { return s == "" }))))
}

// access returns the path's workspace. It writes a 404 when the caller isn't a member, or a 403 when manage is set
// and they lack projects.manage, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string, manage bool) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	case manage && !workspace.Has(ws.Permissions, workspace.PermProjectsManage):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// event is a change by the user in the workspace; the model fills in its kind and paths.
func event(ws *workspace.Workspace, userID string) realtime.Event {
	return realtime.Event{Workspace: ws.Slug, Actor: userID, Audience: realtime.ToWorkspace(ws.ID)}
}

// readForm returns the normalized name, key and description, or writes a 400 with field errors and returns ok=false.
func readForm(w http.ResponseWriter, r *http.Request) (name, key, desc string, ok bool) {
	name, key, desc = strings.TrimSpace(r.FormValue("name")), normKey(r.FormValue("key")), r.FormValue("description")
	errs := map[string]string{}
	for field, msg := range map[string]string{"name": checkName(name), "key": checkKey(key), "description": checkDescription(desc)} {
		if msg != "" {
			errs[field] = msg
		}
	}
	if len(errs) > 0 {
		web.Fields(w, http.StatusBadRequest, errs)
		return "", "", "", false
	}
	return name, key, desc, true
}

// respond writes the project, or a 404 when it's nil.
func respond(w http.ResponseWriter, p *Project, err error) error {
	if err != nil {
		return err
	}
	if p == nil {
		web.Err(w, http.StatusNotFound, errProjectNotFound)
		return nil
	}
	web.JSON(w, http.StatusOK, p)
	return nil
}

// --- responses ---
