package devreset

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

const (
	confirmWord = "delete"
	errConfirm  = "Type delete to confirm."
	errNotLocal = "Only a local database can be reset: app_base_url isn't on localhost."
)

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

// available answers 204 while a reset is allowed, so the admin app knows to offer it; 404 when it isn't.
func (h *Handler) available(w http.ResponseWriter, r *http.Request, _ string) error {
	ok, err := h.local(r)
	if err != nil {
		return err
	}
	if !ok {
		web.WriteErr(w, http.StatusNotFound, errNotLocal)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// reset deletes every user, workspace and what's in them (FormData confirm = "delete"), keeping system config,
// admins, the admin audit log and the workspace defaults.
func (h *Handler) reset(w http.ResponseWriter, r *http.Request, adminID string) error {
	if strings.TrimSpace(strings.ToLower(r.FormValue("confirm"))) != confirmWord {
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"confirm": errConfirm})
		return nil
	}
	ok, err := h.local(r)
	if err != nil {
		return err
	}
	if !ok {
		web.WriteErr(w, http.StatusForbidden, errNotLocal)
		return nil
	}
	if err := h.model.reset(r.Context(), adminID, web.ClientIP(r)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// local reports whether app_base_url is on localhost (or unset): a second guard behind not mounting in production.
func (h *Handler) local(r *http.Request) (bool, error) {
	base, err := h.model.baseURL(r.Context())
	if err != nil {
		return false, err
	}
	return isLocal(base), nil
}

// isLocal reports whether the app's base URL is unset or on this machine.
func isLocal(base string) bool {
	if base == "" {
		return true
	}
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
