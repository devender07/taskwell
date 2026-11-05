package note

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/collab"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errWSNotFound   = "Workspace not found."
	errNoteNotFound = "Note not found."
	errGuest        = "Notes aren't available to guests."
	errForbidden    = "You don't have permission to do that."
	errChoose       = "Choose someone in this workspace."
	errViewOnly     = "You can only view this note."
	errRole         = "Choose view or edit."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	collab       *collab.Hub
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	m := &model{db: db}
	return &Handler{model: m, authenticate: authenticate, member: member, collab: collab.New(db, docs, m.saveFrame)}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return err
	}
	ns, err := h.model.list(r.Context(), userID, ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, ns)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return err
	}
	f, ok := read(w, r, true)
	if !ok {
		return nil
	}
	n, err := h.model.create(r.Context(), ws, userID, f)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, n)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, err := h.note(w, r, userID)
	if n == nil || err != nil {
		return err
	}
	f, ok := read(w, r, false)
	if !ok {
		return nil
	}
	if f.shared() && !n.CanEdit {
		web.Err(w, http.StatusForbidden, errViewOnly)
		return nil
	}
	if err := h.model.update(r.Context(), ws, userID, n.ID, f); err != nil {
		return err
	}
	return h.respond(w, r, ws, userID, n.ID)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, err := h.note(w, r, userID)
	if n == nil || err != nil {
		return err
	}
	if err := h.model.delete(r.Context(), ws, userID, n); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, err := h.note(w, r, userID)
	if n == nil || err != nil {
		return err
	}
	if !n.CanEdit {
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil
	}
	other := strings.TrimSpace(r.FormValue("user_id"))
	role, roleOK := readRole(r, "edit")
	errs := map[string]string{}
	if !web.IsUUID(other) {
		errs["user_id"] = errChoose
	}
	if !roleOK {
		errs["role"] = errRole
	}
	if !web.Check(w, errs) {
		return nil
	}
	switch err := h.model.addMember(r.Context(), ws, userID, n.ID, other, role); {
	case errors.Is(err, errNotOnWS):
		web.Fields(w, http.StatusBadRequest, map[string]string{"user_id": errChoose})
		return nil
	case errors.Is(err, errTooMany):
		web.Fields(w, http.StatusBadRequest, map[string]string{"user_id": "A note can be shared with up to 50 people."})
		return nil
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errNoteNotFound)
		return nil
	case err != nil:
		return err
	}
	return h.respond(w, r, ws, userID, n.ID)
}

// removeMember takes someone off the note: the owner may remove anyone but themselves, others only themselves.
func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, err := h.note(w, r, userID)
	if n == nil || err != nil {
		return err
	}
	other := r.PathValue("userId")
	switch {
	case other == n.Owner.ID:
		web.Err(w, http.StatusBadRequest, "The owner can't be removed. Delete the note instead.")
		return nil
	case other != userID && !n.CanDelete:
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil
	case !web.IsUUID(other):
		web.Err(w, http.StatusBadRequest, errChoose)
		return nil
	}
	if err := h.model.removeMember(r.Context(), ws, userID, n.ID, other); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setRole changes a member's role; only the owner may, and the owner always edits.
func (h *Handler) setRole(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, n, err := h.note(w, r, userID)
	if n == nil || err != nil {
		return err
	}
	other := r.PathValue("userId")
	switch {
	case n.Owner.ID != userID:
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil
	case other == n.Owner.ID:
		web.Err(w, http.StatusBadRequest, "The owner can always edit.")
		return nil
	case !web.IsUUID(other):
		web.Err(w, http.StatusNotFound, errNoteNotFound)
		return nil
	}
	role, ok := readRole(r, "")
	if !ok {
		web.Fields(w, http.StatusBadRequest, map[string]string{"role": errRole})
		return nil
	}
	switch err := h.model.setRole(r.Context(), ws, userID, n.ID, other, role); {
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errNoteNotFound)
		return nil
	case err != nil:
		return err
	}
	return h.respond(w, r, ws, userID, n.ID)
}

// readRole returns the form's role, "edit" or "view", or def when it's absent; ok is false for anything else.
func readRole(r *http.Request, def string) (string, bool) {
	role := strings.TrimSpace(r.FormValue("role"))
	if role == "" {
		role = def
	}
	return role, role == "edit" || role == "view"
}

// respond writes the note as the caller now sees it, or a 404 when it went meanwhile.
func (h *Handler) respond(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, userID, noteID string) error {
	n, err := h.model.get(r.Context(), userID, ws.ID, noteID)
	switch {
	case err != nil:
		return err
	case n == nil:
		web.Err(w, http.StatusNotFound, errNoteNotFound)
		return nil
	}
	web.JSON(w, http.StatusOK, n)
	return nil
}

// note returns the path's workspace and note, or writes a 404 or 403 and returns a nil note.
func (h *Handler) note(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, *Note, error) {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return nil, nil, err
	}
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errNoteNotFound)
		return nil, nil, nil
	}
	n, err := h.model.get(r.Context(), userID, ws.ID, id)
	switch {
	case err != nil:
		return nil, nil, err
	case n == nil:
		web.Err(w, http.StatusNotFound, errNoteNotFound)
	}
	return ws, n, nil
}

// access returns the path's workspace, or writes a 404 when the caller isn't a member (403 for a guest) and returns
// ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errWSNotFound)
		return nil, false, nil
	case ws.Guest:
		web.Err(w, http.StatusForbidden, errGuest)
		return nil, false, nil
	}
	return ws, true, nil
}
