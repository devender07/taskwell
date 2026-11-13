package config

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

type Handler struct {
	model *model
	auth  web.Authenticator
}

func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{model: &model{db: db}, auth: auth}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, _ string) error {
	rows, err := h.model.list(r.Context())
	if err != nil {
		return err
	}
	for i := range rows {
		rows[i].mask()
	}
	web.WriteJSON(w, http.StatusOK, rows)
	return nil
}

// update sets one value. An empty value for a secret keeps the stored one (the form can't show it to edit).
func (h *Handler) update(w http.ResponseWriter, r *http.Request, adminID string) error {
	ctx := r.Context()
	key, value := r.PathValue("key"), r.FormValue("value")
	row, err := h.model.get(ctx, key)
	if err != nil {
		return err
	}
	if row == nil {
		web.WriteErr(w, http.StatusNotFound, "Setting not found.")
		return nil
	}
	if key == "jwt_secret" {
		web.WriteErr(w, http.StatusBadRequest, "This setting can't be changed here.")
		return nil
	}
	if !row.Secret {
		value = strings.TrimSpace(value)
	}
	if msg := checkValue(key, value); msg != "" {
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"value": msg})
		return nil
	}
	if !row.Secret || value != "" {
		if row, err = h.model.set(ctx, adminID, web.ClientIP(r), key, value); err != nil {
			return err
		}
	}
	row.mask()
	web.WriteJSON(w, http.StatusOK, row)
	return nil
}
