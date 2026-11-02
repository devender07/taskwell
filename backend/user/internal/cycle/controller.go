package cycle

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/issue"
	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	errNotFound        = "Workspace not found."
	errProjectNotFound = "Project not found."
	errCycleNotFound   = "Cycle not found."
	errForbidden       = "You don't have permission to do that."
	errArchivedProject = "This project is archived."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	issues       func(ctx context.Context, cycleID, userID string, perms []string) ([]issue.Issue, error)
}

// New takes the auth handler's Authenticate, the workspace handler's Member and the issue handler's CycleIssues.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error),
	issues func(ctx context.Context, cycleID, userID string, perms []string) ([]issue.Issue, error)) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member, issues: issues}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	_, p, ok, err := h.project(w, r, userID)
	if !ok || err != nil {
		return err
	}
	cs, err := h.model.list(r.Context(), p.id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, cs)
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, p, ok, err := h.project(w, r, userID)
	if !ok || err != nil {
		return err
	}
	var c *Cycle
	if n, ok := number(r); ok {
		if c, err = h.model.get(r.Context(), p.id, n); err != nil {
			return err
		}
	}
	if c == nil {
		writeErr(w, http.StatusNotFound, errCycleNotFound)
		return nil
	}
	is, err := h.issues(r.Context(), c.ID, userID, ws.Permissions)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"cycle": c, "issues": is})
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	in, ok := readForm(w, r, true)
	if !ok {
		return nil
	}
	c, err := h.model.create(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, in, event(ws, userID))
	if err != nil {
		return answer(w, err)
	}
	writeJSON(w, http.StatusCreated, c)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, ok, err := h.cycle(w, r, userID)
	if !ok || err != nil {
		return err
	}
	in, ok := readForm(w, r, false)
	if !ok {
		return nil
	}
	c, err := h.model.update(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, n, in, event(ws, userID))
	if err != nil {
		return answer(w, err)
	}
	writeJSON(w, http.StatusOK, c)
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, ok, err := h.cycle(w, r, userID)
	if !ok || err != nil {
		return err
	}
	err = h.model.delete(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, n, event(ws, userID))
	if err != nil {
		return answer(w, err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) rollover(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, ok, err := h.cycle(w, r, userID)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	to, toErr := 0, ""
	if v := strings.TrimSpace(r.PostForm.Get("to")); v == "" {
		toErr = "Choose a cycle."
	} else if to, err = strconv.Atoi(v); err != nil || to < 1 {
		toErr = errCycleNotFound
	}
	if toErr != "" {
		writeFields(w, http.StatusBadRequest, map[string]string{"to": toErr})
		return nil
	}
	moved, c, err := h.model.rollover(r.Context(), ws.ID, normKey(r.PathValue("key")), n, to, userID, event(ws, userID))
	if err != nil {
		return answer(w, err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": moved, "cycle": c})
	return nil
}

// answer writes the response for a model error the API names (missing project or cycle, archived project, field
// errors) and returns nil; any other err is returned for wrap.
func answer(w http.ResponseWriter, err error) error {
	var fe fieldErrors
	switch {
	case errors.Is(err, errNoProject):
		writeErr(w, http.StatusNotFound, errProjectNotFound)
	case errors.Is(err, errNoCycle):
		writeErr(w, http.StatusNotFound, errCycleNotFound)
	case errors.Is(err, errArchived):
		writeErr(w, http.StatusConflict, errArchivedProject)
	case errors.As(err, &fe):
		writeFields(w, http.StatusBadRequest, fe)
	default:
		return err
	}
	return nil
}

// cycle is access with cycles.manage plus the path's cycle number, or writes a 404 for one that can't exist.
func (h *Handler) cycle(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, int, bool, error) {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return nil, 0, false, err
	}
	n, ok := number(r)
	if !ok {
		writeErr(w, http.StatusNotFound, errCycleNotFound)
		return nil, 0, false, nil
	}
	return ws, n, true, nil
}

// project returns the path's workspace and project for a read, or writes a 404 and returns ok=false.
func (h *Handler) project(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, *project, bool, error) {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return nil, nil, false, err
	}
	p, err := h.model.project(r.Context(), ws.ID, normKey(r.PathValue("key")), userID)
	if err != nil {
		return nil, nil, false, err
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, errProjectNotFound)
		return nil, nil, false, nil
	}
	return ws, p, true, nil
}

// access returns the path's workspace. It writes a 404 when the caller isn't a member, or a 403 when manage is set
// and they lack cycles.manage, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string, manage bool) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		writeErr(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	case manage && !workspace.Has(ws.Permissions, workspace.PermCyclesManage):
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// readForm returns the form's name and dates, or writes a 400 with field errors and returns ok=false. On create
// both dates are required; on update a field left out is unchanged.
func readForm(w http.ResponseWriter, r *http.Request, create bool) (input, bool) {
	_ = r.ParseMultipartForm(maxBody)
	form := r.PostForm
	var in input
	errs := map[string]string{}
	if _, ok := form["name"]; ok || create {
		in.setName = true
		if name := strings.TrimSpace(form.Get("name")); name != "" {
			in.name, errs["name"] = &name, checkName(name)
		}
	}
	date := func(k, missing string) *time.Time {
		if _, ok := form[k]; !ok && !create {
			return nil
		}
		t, ok := parseDate(strings.TrimSpace(form.Get(k)))
		if !ok {
			errs[k] = missing
			return nil
		}
		return &t
	}
	in.startsOn, in.endsOn = date("starts_on", "Choose a start date."), date("ends_on", "Choose an end date.")
	maps.DeleteFunc(errs, func(_, msg string) bool { return msg == "" })
	if len(errs) > 0 {
		writeFields(w, http.StatusBadRequest, errs)
		return in, false
	}
	return in, true
}

// number is the path's cycle number; ok=false when it isn't a positive integer.
func number(r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("number"))
	return n, err == nil && n > 0
}

// event is a change by the user in the workspace; the model fills in its kind and paths.
func event(ws *workspace.Workspace, userID string) realtime.Event {
	return realtime.Event{Workspace: ws.Slug, Actor: userID, Audience: realtime.ToWorkspace(ws.ID)}
}

func normKey(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// --- responses ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeFields(w http.ResponseWriter, status int, errs map[string]string) {
	writeJSON(w, status, map[string]map[string]string{"errors": errs})
}
