package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	mail         *mail.Mailer
	cfg          *sysconfig.Store
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error),
	mailer *mail.Mailer, cfg *sysconfig.Store) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member, mail: mailer, cfg: cfg}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.workspace(w, r, userID)
	if ws == nil || err != nil {
		return err
	}
	before := r.URL.Query().Get("before")
	if before != "" && !uuidRe.MatchString(before) {
		writeErr(w, http.StatusBadRequest, "That page doesn't exist.")
		return nil
	}
	ns, more, err := h.model.list(r.Context(), userID, ws.ID, before)
	if err != nil {
		return err
	}
	if ns == nil {
		ns = []Notification{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": ns, "has_more": more})
	return nil
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.workspace(w, r, userID)
	if ws == nil || err != nil {
		return err
	}
	n, err := h.model.unreadCount(r.Context(), userID, ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
	return nil
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.mark(w, r, userID, true)
}

func (h *Handler) markUnread(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.mark(w, r, userID, false)
}

// mark sets the form's notifications (ids, or all=true when marking read) read or unread.
func (h *Handler) mark(w http.ResponseWriter, r *http.Request, userID string, read bool) error {
	ws, err := h.workspace(w, r, userID)
	if ws == nil || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	ids := slices.DeleteFunc(slices.Clone(r.PostForm["ids"]), func(s string) bool { return s == "" })
	all := read && r.PostForm.Get("all") == "true"
	if !all && len(ids) == 0 {
		writeErr(w, http.StatusBadRequest, "Choose notifications.")
		return nil
	}
	if err := h.model.setRead(r.Context(), userID, ws.ID, ws.Slug, ids, all, read); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request, userID string) error {
	s, err := h.model.settings(r.Context(), userID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s)
	return nil
}

// updateSettings changes the settings present in the form.
func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request, userID string) error {
	_ = r.ParseMultipartForm(maxBody)
	form := r.PostForm
	errs := map[string]string{}
	var email *string
	if v, ok := form["email"]; ok {
		email = &v[0]
		if !slices.Contains([]string{"instant", "digest", "off"}, *email) {
			errs["email"] = "Choose how to get emails."
		}
	}
	flag := func(k string) *bool {
		v, ok := form[k]
		if !ok {
			return nil
		}
		on := v[0] == "true"
		if !on && v[0] != "false" {
			errs[k] = "Choose on or off."
		}
		return &on
	}
	mentions, assignments, status, comments := flag("mentions"), flag("assignments"), flag("status"), flag("comments")
	if len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]map[string]string{"errors": errs})
		return nil
	}
	s, err := h.model.update(r.Context(), userID, email, mentions, assignments, status, comments)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, s)
	return nil
}

// workspace returns the path's workspace, or writes a 404 and returns nil when the caller isn't a member.
func (h *Handler) workspace(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err == nil && ws == nil {
		writeErr(w, http.StatusNotFound, "Workspace not found.")
	}
	return ws, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
