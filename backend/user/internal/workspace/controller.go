package workspace

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

const (
	errNotFound   = "Workspace not found."
	errForbidden  = "You don't have permission to do that."
	errEscalation = "You can't grant permissions you don't have."
	errNoAdmin    = "A workspace needs at least one admin."
	errSlugTaken  = "That URL is taken."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	mail         *mail.Mailer
	cfg          *sysconfig.Store
}

// New takes the auth handler's Authenticate, which returns the signed-in user ID or "".
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error), mailer *mail.Mailer, cfg *sysconfig.Store) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, mail: mailer, cfg: cfg}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.model.list(r.Context(), userID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ws)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	name, slug, ok := readForm(w, r)
	if !ok {
		return nil
	}
	ws, err := h.model.create(r.Context(), userID, name, slug)
	if errors.Is(err, errTaken) {
		writeFields(w, http.StatusConflict, map[string]string{"slug": errSlugTaken})
		return nil
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, ws)
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.model.open(r.Context(), userID, normSlug(r.PathValue("slug")))
	if err != nil {
		return err
	}
	if ws == nil {
		writeErr(w, http.StatusNotFound, errNotFound)
		return nil
	}
	writeJSON(w, http.StatusOK, ws)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permWorkspaceEdit)
	if !ok || err != nil {
		return err
	}
	name, slug, ok := readForm(w, r)
	if !ok {
		return nil
	}
	err = h.model.update(r.Context(), ws.ID, name, slug)
	if errors.Is(err, errTaken) {
		writeFields(w, http.StatusConflict, map[string]string{"slug": errSlugTaken})
		return nil
	}
	if err != nil {
		return err
	}
	ws.Name, ws.Slug = name, slug
	writeJSON(w, http.StatusOK, ws)
	return nil
}

// access returns the path's workspace with the caller's role and permissions. It writes a 404 when the caller
// isn't a member, or a 403 when perm isn't "" and they lack it, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID, perm string) (*Workspace, bool, error) {
	ws, err := h.model.membership(r.Context(), userID, normSlug(r.PathValue("slug")))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		writeErr(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	case perm != "" && !has(ws.Permissions, perm):
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// readForm returns the normalized name and slug, or writes a 400 with field errors and returns ok=false.
func readForm(w http.ResponseWriter, r *http.Request) (name, slug string, ok bool) {
	name, slug = strings.TrimSpace(r.FormValue("name")), normSlug(r.FormValue("slug"))
	errs := map[string]string{}
	if msg := checkName(name); msg != "" {
		errs["name"] = msg
	}
	if msg := checkSlug(slug); msg != "" {
		errs["slug"] = msg
	}
	if len(errs) > 0 {
		writeFields(w, http.StatusBadRequest, errs)
		return "", "", false
	}
	return name, slug, true
}

func normSlug(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

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

// remove deletes the workspace with its members, roles and invites.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permWorkspaceDelete)
	if !ok || err != nil {
		return err
	}
	if err := h.model.delete(r.Context(), ws.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) permissions(w http.ResponseWriter, _ *http.Request, _ string) error {
	writeJSON(w, http.StatusOK, catalog)
	return nil
}
