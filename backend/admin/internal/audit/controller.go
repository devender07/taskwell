package audit

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/web"
)

const pageSize = 100

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

// list answers a page of entries, newest first; ?before=<id> continues after the last one shown.
func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ string) error {
	before := r.URL.Query().Get("before")
	var id pgtype.UUID
	if before != "" && id.Scan(before) != nil {
		web.WriteErr(w, http.StatusBadRequest, "Invalid cursor.")
		return nil
	}
	es, err := h.model.list(r.Context(), before, pageSize)
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"entries": es, "more": len(es) == pageSize})
	return nil
}
