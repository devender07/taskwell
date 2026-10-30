package stats

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/web"
)

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, _ string) error {
	s, err := h.model.stats(r.Context())
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, s)
	return nil
}
