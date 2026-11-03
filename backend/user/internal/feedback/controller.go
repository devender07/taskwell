package feedback

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/issue"
	"github.com/devender07/tether/backend/user/internal/web"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	errNoWorkspace = "Workspace not found."
	errForbidden   = "You don't have permission to do that."
	errNoFeedback  = "Feedback not found."
	errNoIssue     = "Issue not found."
	errNoForm      = "This feedback form isn't available."
)

type Handler struct {
	model        *model
	lim          web.Limiter
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, lim: web.Limiter{DB: db, Name: "feedback", Burst: 10, Per: 6 * time.Minute},
		authenticate: authenticate, member: member}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	q := r.URL.Query()
	before, ok := cursor(w, r)
	if !ok {
		return nil
	}
	items, more, err := h.model.list(r.Context(), ws.ID, userID, q.Get("state"), strings.TrimSpace(q.Get("q")), before)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"items": items, "has_more": more})
	return nil
}

func (h *Handler) count(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	n, err := h.model.countNew(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int{"new": n})
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	in, errs := read(r.PostFormValue, false)
	if !web.Check(w, errs) {
		return nil
	}
	id, err := h.model.create(r.Context(), scope{ws.ID, ws.Slug, userID}, in, "member")
	if err != nil {
		return err
	}
	return h.respond(w, r, ws.ID, userID, id, http.StatusCreated)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	id, ok := feedbackID(w, r)
	if !ok {
		return nil
	}
	_ = r.ParseMultipartForm(maxBody)
	var archived *bool
	if vs, set := r.PostForm["archived"]; set {
		if !workspace.Has(ws.Permissions, workspace.PermFeedbackManage) {
			web.Err(w, http.StatusForbidden, errForbidden)
			return nil
		}
		v := vs[0] == "true"
		archived = &v
	}
	var link issueRef
	vs, setIssue := r.PostForm["issue"]
	if setIssue && strings.TrimSpace(vs[0]) != "" {
		ref, err := h.issue(r.Context(), ws.ID, userID, strings.TrimSpace(vs[0]))
		if err != nil {
			return err
		}
		if ref == nil {
			web.Fields(w, http.StatusBadRequest, map[string]string{"issue": errNoIssue})
			return nil
		}
		link = *ref
	}
	found, err := h.model.update(r.Context(), scope{ws.ID, ws.Slug, userID}, id, setIssue, link, archived)
	if err != nil {
		return err
	}
	if !found {
		web.Err(w, http.StatusNotFound, errNoFeedback)
		return nil
	}
	return h.respond(w, r, ws.ID, userID, id, http.StatusOK)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermFeedbackManage)
	if !ok || err != nil {
		return err
	}
	id, ok := feedbackID(w, r)
	if !ok {
		return nil
	}
	found, err := h.model.delete(r.Context(), scope{ws.ID, ws.Slug, userID}, id)
	if err != nil {
		return err
	}
	if !found {
		web.Err(w, http.StatusNotFound, errNoFeedback)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) issueFeedback(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	ref, err := h.issue(r.Context(), ws.ID, userID, r.PathValue("identifier"))
	if err != nil {
		return err
	}
	if ref == nil {
		web.Err(w, http.StatusNotFound, errNoIssue)
		return nil
	}
	before, ok := cursor(w, r)
	if !ok {
		return nil
	}
	fs, more, total, err := h.model.forIssue(r.Context(), ws.ID, userID, ref.id, before)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"items": fs, "has_more": more, "total": total})
	return nil
}

// cursor reads ?before= (a feedback id), writing a 400 and returning ok=false when it isn't one.
func cursor(w http.ResponseWriter, r *http.Request) (*string, bool) {
	b := r.URL.Query().Get("before")
	if b == "" {
		return nil, true
	}
	if !web.IsUUID(b) {
		web.Err(w, http.StatusBadRequest, "Invalid cursor.")
		return nil, false
	}
	return &b, true
}

func (h *Handler) form(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermFeedbackManage)
	if !ok || err != nil {
		return err
	}
	token, err := h.model.formToken(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"enabled": token != nil, "token": token})
	return nil
}

func (h *Handler) setForm(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermFeedbackManage)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	var enabled *bool
	if vs, set := r.PostForm["enabled"]; set {
		v := vs[0] == "true"
		enabled = &v
	}
	token, err := h.model.setForm(r.Context(), ws.ID, enabled, r.PostForm.Get("rotate") == "true", web.RandomToken())
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"enabled": token != nil, "token": token})
	return nil
}

func (h *Handler) publicForm(w http.ResponseWriter, r *http.Request) error {
	_, name, ok, err := h.model.formWorkspace(r.Context(), r.PathValue("token"))
	if err != nil {
		return err
	}
	if !ok {
		web.Err(w, http.StatusNotFound, errNoForm)
		return nil
	}
	web.JSON(w, http.StatusOK, map[string]string{"workspace": name})
	return nil
}

// submit takes feedback from the public form. A filled-in honeypot (website) is answered as a success and dropped.
func (h *Handler) submit(w http.ResponseWriter, r *http.Request) error {
	if stop, err := web.Limited(w, r, h.lim, web.ClientIP(r)); err != nil || stop {
		return err
	}
	sc, _, ok, err := h.model.formWorkspace(r.Context(), r.PathValue("token"))
	if err != nil {
		return err
	}
	if !ok {
		web.Err(w, http.StatusNotFound, errNoForm)
		return nil
	}
	if r.PostFormValue("website") != "" {
		web.JSON(w, http.StatusCreated, struct{}{})
		return nil
	}
	in, errs := read(r.PostFormValue, true)
	if !web.Check(w, errs) {
		return nil
	}
	if _, err := h.model.create(r.Context(), sc, in, "form"); err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, struct{}{})
	return nil
}

// respond answers the feedback as the user sees it.
func (h *Handler) respond(w http.ResponseWriter, r *http.Request, wsID, userID, id string, status int) error {
	f, err := h.model.get(r.Context(), wsID, userID, id)
	if err != nil {
		return err
	}
	if f == nil {
		web.Err(w, http.StatusNotFound, errNoFeedback)
		return nil
	}
	web.JSON(w, status, f)
	return nil
}

// issue resolves an identifier like ENG-12 to an issue the user can see; nil when it isn't one.
func (h *Handler) issue(ctx context.Context, wsID, userID, identifier string) (*issueRef, error) {
	key, number, ok := issue.ParseIdentifier(identifier)
	if !ok {
		return nil, nil
	}
	return h.model.issue(ctx, wsID, userID, key, number)
}

// access returns the path's workspace. It writes a 404 when the caller isn't a member or a 403 without perm (when
// set), and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID, perm string) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNoWorkspace)
		return nil, false, nil
	case perm != "" && !workspace.Has(ws.Permissions, perm):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

func feedbackID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errNoFeedback)
		return "", false
	}
	return id, true
}
