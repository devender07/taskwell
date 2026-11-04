package project

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errNotFound          = "Workspace not found."
	errProjectNotFound   = "Project not found."
	errForbidden         = "You don't have permission to do that."
	errKeyTaken          = "That key is taken."
	errPickedOutsider    = "Someone you picked isn't in this workspace."
	errPublicMembers     = "Public projects take guests only."
	errMilestoneNotFound = "Milestone not found."
	errArchivedProject   = "This project is archived."
	errNotArchived       = "Archive the project before deleting it."
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
	if !ok || err != nil || workspace.DenyGuest(w, ws) {
		return err
	}
	in, ok := readForm(w, r)
	if !ok {
		return nil
	}
	p, err := h.model.create(r.Context(), ws.ID, userID, in, r.PostForm.Get("private") == "true", ids(r, "member_ids"),
		event(ws, userID))
	if errors.Is(err, errDates) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"target_date": errDateOrder})
		return nil
	}
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
	in, ok := readForm(w, r)
	if !ok {
		return nil
	}
	var private *bool
	if v, ok := r.PostForm["private"]; ok {
		b := v[0] == "true"
		private = &b
	}
	p, err := h.model.update(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, in, private, event(ws, userID))
	if errors.Is(err, errDates) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"target_date": errDateOrder})
		return nil
	}
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

// remove deletes an archived project and everything in it.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	found, err := h.model.remove(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, event(ws, userID))
	switch {
	case errors.Is(err, errActive):
		web.Err(w, http.StatusConflict, errNotArchived)
	case err != nil:
		return err
	case !found:
		web.Err(w, http.StatusNotFound, errProjectNotFound)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return nil
}

// people lists everyone who can see the project, for assignee and mention pickers: those matching ?q= (name or
// email), at most ?limit= (1-100; all without it).
func (h *Handler) people(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	query := r.URL.Query()
	limit := 0
	if query.Has("limit") {
		if limit, err = strconv.Atoi(query.Get("limit")); err != nil || limit < 1 || limit > 100 {
			web.Err(w, http.StatusBadRequest, "limit must be from 1 to 100.")
			return nil
		}
	}
	ms, err := h.model.people(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, strings.TrimSpace(query.Get("q")), limit)
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

// readForm returns the normalized name, key, description and dates (nil when the field isn't sent), or writes a 400
// with field errors and returns ok=false.
func readForm(w http.ResponseWriter, r *http.Request) (input, bool) {
	in := input{name: strings.TrimSpace(r.FormValue("name")), key: normKey(r.FormValue("key")), description: r.FormValue("description"),
		start: date(r, "start_date"), target: date(r, "target_date")}
	errs := map[string]string{"name": checkName(in.name), "key": checkKey(in.key), "description": checkDescription(in.description)}
	if in.start != nil && *in.start != "" {
		errs["start_date"] = checkDate(*in.start)
	}
	if in.target != nil && *in.target != "" {
		errs["target_date"] = checkDate(*in.target)
	}
	return in, web.Check(w, errs)
}

// date returns the trimmed form field, or nil when it isn't sent.
func date(r *http.Request, field string) *string {
	v, ok := r.PostForm[field]
	if !ok {
		return nil
	}
	s := strings.TrimSpace(v[0])
	return &s
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

func (h *Handler) roadmap(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	ps, err := h.model.roadmap(r.Context(), ws.ID, userID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, ps)
	return nil
}

// --- milestones ---

func (h *Handler) milestones(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, false)
	if !ok || err != nil {
		return err
	}
	ms, err := h.model.milestones(r.Context(), ws.ID, normKey(r.PathValue("key")), userID)
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

func (h *Handler) createMilestone(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return err
	}
	name, date, ok := readMilestone(w, r)
	if !ok {
		return nil
	}
	ms, err := h.model.createMilestone(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, name, date, event(ws, userID))
	return answerMilestone(w, http.StatusCreated, ms, err)
}

func (h *Handler) updateMilestone(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.milestone(w, r, userID)
	if !ok || err != nil {
		return err
	}
	name, date, ok := readMilestone(w, r)
	if !ok {
		return nil
	}
	ms, err := h.model.updateMilestone(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, id, name, date, event(ws, userID))
	return answerMilestone(w, http.StatusOK, ms, err)
}

func (h *Handler) deleteMilestone(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.milestone(w, r, userID)
	if !ok || err != nil {
		return err
	}
	err = h.model.deleteMilestone(r.Context(), ws.ID, normKey(r.PathValue("key")), userID, id, event(ws, userID))
	return answerMilestone(w, http.StatusNoContent, nil, err)
}

// milestone is access for a write to the path's milestone; it writes a 404 when its id isn't a UUID.
func (h *Handler) milestone(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, string, bool, error) {
	ws, ok, err := h.access(w, r, userID, true)
	if !ok || err != nil {
		return nil, "", false, err
	}
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errMilestoneNotFound)
		return nil, "", false, nil
	}
	return ws, id, true, nil
}

// readMilestone returns the trimmed name and the date, or writes a 400 with field errors and returns ok=false.
func readMilestone(w http.ResponseWriter, r *http.Request) (name, date string, ok bool) {
	name, date = strings.TrimSpace(r.FormValue("name")), strings.TrimSpace(r.FormValue("date"))
	return name, date, web.Check(w, map[string]string{"name": checkMilestoneName(name), "date": checkDate(date)})
}

// answerMilestone writes the milestone with the status (nothing for a 204), or the error a write returned.
func answerMilestone(w http.ResponseWriter, status int, ms *Milestone, err error) error {
	switch {
	case errors.Is(err, errNoProject):
		web.Err(w, http.StatusNotFound, errProjectNotFound)
	case errors.Is(err, errNoMilestone):
		web.Err(w, http.StatusNotFound, errMilestoneNotFound)
	case errors.Is(err, errArchived):
		web.Err(w, http.StatusConflict, errArchivedProject)
	case err != nil:
		return err
	case status == http.StatusNoContent:
		w.WriteHeader(status)
	default:
		web.JSON(w, status, ms)
	}
	return nil
}

// --- responses ---
