package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/tether/backend/admin/internal/audit"
	"github.com/devender07/tether/backend/admin/internal/web"
)

const errBadLogin = "Email or password is incorrect."

// dummyHash keeps sign-in timing the same whether or not the email belongs to an admin.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)

type Handler struct {
	model  *model
	secret []byte // ADMIN_JWT_SECRET; nil falls back to system_config's jwt_secret (dev)
}

func New(db *pgxpool.Pool, secret []byte) *Handler {
	return &Handler{model: &model{db: db}, secret: secret}
}

// signin checks the password and starts a sign-in that can only finish the second factor (POST /api/auth/mfa).
// It answers {"mfa": "verify"}, or {"mfa": "enrol", "secret", "otpauth_url"} when the admin has no factor yet.
func (h *Handler) signin(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
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
	if tooMany(w, ipLimit.take(web.ClientIP(r), time.Now())) {
		return nil
	}
	if locked, err := h.locked(w, r, "admin:"+email); err != nil || locked {
		return err
	}
	u, ph, err := h.model.adminWithPassword(ctx, email)
	if err != nil {
		return err
	}
	stored := dummyHash
	if ph != nil {
		stored = []byte(*ph)
	}
	if bcrypt.CompareHashAndPassword(stored, []byte(pw)) != nil || ph == nil {
		if err := h.model.fail(ctx, "admin:"+email); err != nil {
			return err
		}
		if err := audit.Record(ctx, h.model.db, "", audit.SignInFailed, email, web.ClientIP(r)); err != nil {
			return err
		}
		web.WriteErr(w, http.StatusUnauthorized, errBadLogin)
		return nil
	}
	sid, err := h.model.startSession(ctx, u.ID, false, mfaTTL)
	if err != nil {
		return err
	}
	if err := h.setCookie(ctx, w, u.ID, sid, mfaTTL); err != nil {
		return err
	}
	secret, err := h.model.enrolment(ctx, u.ID, newTOTPSecret())
	if err != nil {
		return err
	}
	if secret == "" {
		web.WriteJSON(w, http.StatusOK, map[string]string{"mfa": "verify"})
		return nil
	}
	web.WriteJSON(w, http.StatusOK, map[string]string{"mfa": "enrol", "secret": secret, "otpauth_url": otpauthURL(u.Email, secret)})
	return nil
}

// mfa finishes a sign-in with a code from the authenticator app (which also confirms enrolment) or a recovery code.
// It answers the admin, plus recovery_codes, shown once, when this confirmed enrolment.
func (h *Handler) mfa(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID, sid, err := h.token(r)
	if err != nil {
		return err
	}
	if sid != "" {
		if ok, err := h.model.pending(ctx, sid, userID); err != nil {
			return err
		} else if !ok {
			sid = ""
		}
	}
	if sid == "" {
		web.WriteErr(w, http.StatusUnauthorized, "Your sign-in timed out. Sign in again.")
		return nil
	}
	code, recovery := strings.TrimSpace(r.FormValue("code")), strings.TrimSpace(r.FormValue("recovery_code"))
	if code == "" && recovery == "" {
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"code": "Enter the 6-digit code."})
		return nil
	}
	if tooMany(w, ipLimit.take(web.ClientIP(r), time.Now())) {
		return nil
	}
	key := "admin-mfa:" + userID
	if locked, err := h.locked(w, r, key); err != nil || locked {
		return err
	}
	res, err := h.model.checkMFA(ctx, userID, code, recovery, time.Now())
	if err != nil {
		return err
	}
	ip := web.ClientIP(r)
	if !res.ok {
		if err := h.model.fail(ctx, key); err != nil {
			return err
		}
		if err := audit.Record(ctx, h.model.db, userID, audit.MFAFailed, "", ip); err != nil {
			return err
		}
		field := "code"
		if code == "" {
			field = "recovery_code"
		}
		web.WriteFields(w, http.StatusUnauthorized, map[string]string{field: "That code is incorrect."})
		return nil
	}
	full, err := h.model.finishSignin(ctx, sid, userID, ttl)
	if err != nil {
		return err
	}
	if full == "" {
		web.WriteErr(w, http.StatusUnauthorized, "Your sign-in timed out. Sign in again.")
		return nil
	}
	if err := h.setCookie(ctx, w, userID, full, ttl); err != nil {
		return err
	}
	for _, a := range []struct {
		on     bool
		action string
	}{{res.enrolled, audit.MFAEnrolled}, {res.usedRecovery, audit.RecoveryUsed}, {true, audit.SignIn}} {
		if a.on {
			if err := audit.Record(ctx, h.model.db, userID, a.action, "", ip); err != nil {
				return err
			}
		}
	}
	u, err := h.model.admin(ctx, userID)
	if err != nil {
		return err
	}
	if u == nil {
		web.WriteErr(w, http.StatusUnauthorized, "Not signed in.")
		return nil
	}
	web.WriteJSON(w, http.StatusOK, struct {
		*User
		RecoveryCodes []string `json:"recovery_codes,omitempty"`
	}{u, res.recovery})
	return nil
}

// locked answers 429 when key has had maxFails failures in the last failWindow.
func (h *Handler) locked(w http.ResponseWriter, r *http.Request, key string) (bool, error) {
	n, err := h.model.failures(r.Context(), key)
	if err != nil || n < maxFails {
		return false, err
	}
	return tooMany(w, failWindow), nil
}

// signout ends the sign-in server-side, so a copied cookie stops working too.
func (h *Handler) signout(w http.ResponseWriter, r *http.Request) error {
	_, sid, err := h.token(r)
	if err != nil {
		return err
	}
	if sid != "" {
		if err := h.model.endSession(r.Context(), sid); err != nil {
			return err
		}
	}
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
