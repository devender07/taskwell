package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

const (
	errBadCode   = "That code is invalid or expired."
	errBadReset  = "This reset link is invalid or expired."
	errBadLogin  = "Wrong email or password."
	errSignedOut = "Not signed in."
)

// dummyHash keeps sign-in timing the same whether or not the email exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)

type Handler struct {
	model *model
	cfg   *sysconfig.Store
	mail  *mail.Mailer
}

func New(db *pgxpool.Pool, cfg *sysconfig.Store, m *mail.Mailer) *Handler {
	return &Handler{model: &model{db: db}, cfg: cfg, mail: m}
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) error {
	name, email, pw := r.FormValue("name"), r.FormValue("email"), r.FormValue("password")
	if errs := fieldErrors("name", checkName(name), "email", checkEmail(email), "password", checkPassword(pw)); errs != nil {
		writeFields(w, http.StatusBadRequest, errs)
		return nil
	}
	ph, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	u, err := h.model.createUser(r.Context(), strings.TrimSpace(name), normEmail(email), string(ph))
	if errors.Is(err, errEmailTaken) {
		writeFields(w, http.StatusConflict, map[string]string{"email": "An account with this email already exists."})
		return nil
	}
	if err != nil {
		return err
	}
	if err := h.startSession(r.Context(), w, u.ID, true); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, u)
	return nil
}

func (h *Handler) signin(w http.ResponseWriter, r *http.Request) error {
	email, pw := r.FormValue("email"), r.FormValue("password")
	pwErr := ""
	if pw == "" {
		pwErr = "Password is required."
	}
	if errs := fieldErrors("email", checkEmail(email), "password", pwErr); errs != nil {
		writeFields(w, http.StatusBadRequest, errs)
		return nil
	}
	u, ph, err := h.model.userWithPassword(r.Context(), normEmail(email))
	if err != nil {
		return err
	}
	stored := dummyHash
	if ph != nil {
		stored = []byte(*ph)
	}
	if bcrypt.CompareHashAndPassword(stored, []byte(pw)) != nil || ph == nil {
		writeErr(w, http.StatusUnauthorized, errBadLogin)
		return nil
	}
	if err := h.startSession(r.Context(), w, u.ID, remember(r)); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) otpSend(w http.ResponseWriter, r *http.Request) error {
	email := r.FormValue("email")
	if msg := checkEmail(email); msg != "" {
		writeFields(w, http.StatusBadRequest, map[string]string{"email": msg})
		return nil
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	return h.sendSecret(w, r, normEmail(email), purposeCode, "otp_ttl_minutes", code, func(minutes int) (string, string, error) {
		return "Your Tether sign-in code: " + code,
			fmt.Sprintf("Your Tether sign-in code is %s\n\nIt expires in %d minutes. If you didn't ask for it, ignore this email.\n", code, minutes), nil
	})
}

func (h *Handler) forgot(w http.ResponseWriter, r *http.Request) error {
	email := r.FormValue("email")
	if msg := checkEmail(email); msg != "" {
		writeFields(w, http.StatusBadRequest, map[string]string{"email": msg})
		return nil
	}
	token := newToken()
	return h.sendSecret(w, r, normEmail(email), purposeReset, "reset_ttl_minutes", token, func(minutes int) (string, string, error) {
		base, err := h.cfg.Get(r.Context(), "app_base_url")
		if err != nil {
			return "", "", err
		}
		link := strings.TrimRight(base, "/") + "/reset-password?token=" + url.QueryEscape(token)
		return "Reset your Tether password",
			fmt.Sprintf("Reset your Tether password by opening this link:\n\n%s\n\nIt expires in %d minutes. If you didn't ask for it, ignore this email.\n", link, minutes), nil
	})
}

// sendSecret stores a new single-use secret for the account (if it exists), voids older unused
// ones of the same purpose, and emails it. Always answers 204 so callers can't probe for accounts.
func (h *Handler) sendSecret(w http.ResponseWriter, r *http.Request, email, purpose, ttlKey, secret string,
	compose func(minutes int) (subject, body string, err error)) error {
	ctx := r.Context()
	if ok, err := h.mail.Configured(ctx); err != nil {
		return err
	} else if !ok {
		writeErr(w, http.StatusServiceUnavailable, "Email is not configured. Contact your administrator.")
		return nil
	}
	minutes, err := h.cfg.GetInt(ctx, ttlKey)
	if err != nil {
		return err
	}
	userID, err := h.model.userIDByEmail(ctx, email)
	if err != nil {
		return err
	}
	if userID == "" {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if err := h.model.replaceToken(ctx, userID, purpose, hash(secret), minutes); err != nil {
		return err
	}
	subject, body, err := compose(minutes)
	if err != nil {
		return err
	}
	// ponytail: a send failure surfaces as 500 only for existing accounts, a weak enumeration signal; send via a queue to close it.
	if err := h.mail.Send(ctx, email, subject, body); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) otpVerify(w http.ResponseWriter, r *http.Request) error {
	email, code := r.FormValue("email"), strings.TrimSpace(r.FormValue("code"))
	if errs := fieldErrors("email", checkEmail(email), "code", checkCode(code)); errs != nil {
		writeFields(w, http.StatusBadRequest, errs)
		return nil
	}
	u, err := h.model.useCode(r.Context(), normEmail(email), hash(code))
	if err != nil {
		return err
	}
	if u == nil {
		writeErr(w, http.StatusUnauthorized, errBadCode)
		return nil
	}
	if err := h.startSession(r.Context(), w, u.ID, remember(r)); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) error {
	token, pw := r.FormValue("token"), r.FormValue("password")
	if msg := checkPassword(pw); msg != "" {
		writeFields(w, http.StatusBadRequest, map[string]string{"password": msg})
		return nil
	}
	ph, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	ok, err := h.model.resetPassword(r.Context(), hash(token), string(ph))
	if err != nil {
		return err
	}
	if !ok {
		writeErr(w, http.StatusUnauthorized, errBadReset)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// refresh trades a valid refresh token for a new access + refresh pair.
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		return h.signedOut(w, r)
	}
	userID, f, result, err := h.model.rotateRefresh(r.Context(), hash(c.Value))
	if err != nil {
		return err
	}
	switch result {
	case refreshInvalid:
		return h.signedOut(w, r)
	case refreshRaced:
		// Another request just rotated it and set fresh cookies; don't clear them.
		writeErr(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	if err := h.setTokens(r.Context(), w, userID, f); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) signout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(refreshCookie); err == nil {
		if err := h.model.revokeFamily(r.Context(), hash(c.Value)); err != nil {
			return err
		}
	}
	if err := h.clearCookies(r.Context(), w); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	userID, err := h.authenticate(r)
	if err != nil {
		return err
	}
	var u *User
	if userID != "" {
		if u, err = h.model.userByID(r.Context(), userID); err != nil {
			return err
		}
	}
	if u == nil {
		writeErr(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	writeJSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) signedOut(w http.ResponseWriter, r *http.Request) error {
	if err := h.clearCookies(r.Context(), w); err != nil {
		return err
	}
	writeErr(w, http.StatusUnauthorized, errSignedOut)
	return nil
}

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

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
