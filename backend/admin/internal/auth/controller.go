package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/tether/backend/admin/internal/web"
)

const errBadLogin = "Email or password is incorrect."

// dummyHash keeps sign-in timing the same whether or not the email belongs to an admin.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)

type Handler struct {
	model  *model
	secret []byte // ADMIN_JWT_SECRET; nil falls back to system_config's jwt_secret (dev)
}

func New(db *pgxpool.Pool, secret []byte) *Handler { return &Handler{model: &model{db: db}, secret: secret} }

func (h *Handler) signin(w http.ResponseWriter, r *http.Request) error {
	email, pw := strings.ToLower(strings.TrimSpace(r.FormValue("email"))), r.FormValue("password")
	errs := map[string]string{}
	if email == "" {
		errs["email"] = "Email is required."
	}
	if pw == "" {
		errs["password"] = "Password is required."
	}
	if len(errs) > 0 {
		web.WriteFields(w, http.StatusBadRequest, errs)
		return nil
	}
	if tooMany(w, ipLimit.take(clientIP(r), time.Now())) {
		return nil
	}
	key := "admin:" + email
	if n, err := h.model.failures(r.Context(), key); err != nil {
		return err
	} else if n >= maxFails {
		tooMany(w, failWindow)
		return nil
	}
	u, ph, err := h.model.adminWithPassword(r.Context(), email)
	if err != nil {
		return err
	}
	stored := dummyHash
	if ph != nil {
		stored = []byte(*ph)
	}
	if bcrypt.CompareHashAndPassword(stored, []byte(pw)) != nil || ph == nil {
		if err := h.model.fail(r.Context(), key); err != nil {
			return err
		}
		web.WriteErr(w, http.StatusUnauthorized, errBadLogin)
		return nil
	}
	if err := h.setCookie(r.Context(), w, u.ID); err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) signout(w http.ResponseWriter, r *http.Request) error {
	if err := h.clearCookie(r.Context(), w); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request, userID string) error {
	u, err := h.model.admin(r.Context(), userID)
	if err != nil {
		return err
	}
	if u == nil {
		web.WriteErr(w, http.StatusUnauthorized, "Not signed in.")
		return nil
	}
	web.WriteJSON(w, http.StatusOK, u)
	return nil
}
