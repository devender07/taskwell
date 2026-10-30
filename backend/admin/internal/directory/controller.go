package directory

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/web"
)

const pageSize = 50

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

// search reads ?q= and the 1-based ?page= (anything invalid is page 1) as a query and an offset.
func search(r *http.Request) (string, int) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	return strings.TrimSpace(r.URL.Query().Get("q")), (page - 1) * pageSize
}

func (h *Handler) workspaces(w http.ResponseWriter, r *http.Request, _ string) error {
	q, offset := search(r)
	ws, total, err := h.model.workspaces(r.Context(), q, offset)
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"workspaces": ws, "total": total})
	return nil
}

func (h *Handler) users(w http.ResponseWriter, r *http.Request, _ string) error {
	q, offset := search(r)
	us, total, err := h.model.users(r.Context(), q, offset)
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"users": us, "total": total})
	return nil
}

func (h *Handler) suspend(w http.ResponseWriter, r *http.Request, adminID string) error {
	if r.PathValue("id") == adminID {
		web.WriteErr(w, http.StatusBadRequest, "You can't suspend yourself.")
		return nil
	}
	return h.setSuspended(w, r, true)
}

func (h *Handler) unsuspend(w http.ResponseWriter, r *http.Request, _ string) error {
	return h.setSuspended(w, r, false)
}

func (h *Handler) setSuspended(w http.ResponseWriter, r *http.Request, suspended bool) error {
	var id pgtype.UUID
	var u *User
	if id.Scan(r.PathValue("id")) == nil {
		var err error
		if u, err = h.model.setSuspended(r.Context(), r.PathValue("id"), suspended); err != nil {
			return err
		}
	}
	if u == nil {
		web.WriteErr(w, http.StatusNotFound, "User not found.")
		return nil
	}
	web.WriteJSON(w, http.StatusOK, u)
	return nil
}
