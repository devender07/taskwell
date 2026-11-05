package workspace

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	errNotFound   = "Workspace not found."
	errForbidden  = "You don't have permission to do that."
	errEscalation = "You can't grant permissions you don't have."
	errNoAdmin    = "A workspace needs at least one admin."
	errSlugTaken  = "That URL is taken."

	// ErrGuest answers a guest asking for something workspace-wide.
	ErrGuest = "Guests can't see this."
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
	web.JSON(w, http.StatusOK, ws)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	name, slug, ok := readForm(w, r)
	if !ok {
		return nil
	}
	ws, err := h.model.create(r.Context(), userID, name, slug)
	if errors.Is(err, errNoCreate) {
		web.Err(w, http.StatusForbidden, "You don't have permission to create workspaces.")
		return nil
	}
	if errors.Is(err, errTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"slug": errSlugTaken})
		return nil
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, ws)
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.model.open(r.Context(), userID, normSlug(r.PathValue("slug")))
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil
	}
	web.JSON(w, http.StatusOK, ws)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, PermWorkspaceEdit)
	if !ok || err != nil {
		return err
	}
	name, slug, ok := readForm(w, r)
	if !ok {
		return nil
	}
	err = h.model.update(r.Context(), ws, userID, name, slug)
	if errors.Is(err, errTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"slug": errSlugTaken})
		return nil
	}
	if err != nil {
		return err
	}
	ws.Name, ws.Slug = name, slug
	web.JSON(w, http.StatusOK, ws)
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
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	case perm != "" && !Has(ws.Permissions, perm):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// DenyGuest writes a 403 and returns true when the caller is a guest of ws, for what's workspace-wide.
func DenyGuest(w http.ResponseWriter, ws *Workspace) bool {
	if ws.Guest {
		web.Err(w, http.StatusForbidden, ErrGuest)
	}
	return ws.Guest
}

// Member returns the workspace with the user's role and permissions; nil when it's missing or the user isn't a member.
func (h *Handler) Member(ctx context.Context, userID, slug string) (*Workspace, error) {
	return h.model.membership(ctx, userID, normSlug(slug))
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
		web.Fields(w, http.StatusBadRequest, errs)
		return "", "", false
	}
	return name, slug, true
}

func normSlug(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// --- responses ---

// remove deletes the workspace with its members, roles and invites.
func (h *Handler) remove(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permWorkspaceDelete)
	if !ok || err != nil {
		return err
	}
	if err := h.model.delete(r.Context(), ws, userID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) permissions(w http.ResponseWriter, _ *http.Request, _ string) error {
	web.JSON(w, http.StatusOK, catalog)
	return nil
}
