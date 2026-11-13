package audit

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

const pageSize = 50 // the admin app's PAGE_SIZE

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

// list answers the 1-based ?page= of entries (anything invalid is page 1), newest first, and how many there are.
func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ string) error {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	es, total, err := h.model.list(r.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"entries": es, "total": total})
	return nil
}
