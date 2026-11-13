package auth

import (
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/taskwell/backend/admin/internal/web"
)

func (h *Handler) listAdmins(w http.ResponseWriter, r *http.Request, _ string) error {
	as, err := h.model.accounts(r.Context())
	if err != nil {
		return err
	}
	web.WriteJSON(w, http.StatusOK, map[string]any{"admins": as})
	return nil
}

// checkNewAdmin returns the field errors of an add-admin form (email already trimmed and lowercased).
func checkNewAdmin(name, email, pw, current string) map[string]string {
	errs := map[string]string{}
	if name == "" {
		errs["name"] = "Name is required."
	} else if len([]rune(name)) > 100 {
		errs["name"] = "Name must be 100 characters or fewer."
	}
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		errs["email"] = "Enter a valid email address."
	}
	if len(pw) < 8 {
		errs["password"] = "Password must be at least 8 characters."
	} else if len(pw) > 72 {
		errs["password"] = "Password must be 72 bytes or fewer."
	}
	if current == "" {
		errs["current_password"] = "Your password is required."
	}
	return errs
}

// confirm checks the signed-in admin's own password before a change to admin accounts, so a stolen session alone
// can't add a lasting account or lock the others out. Wrong ones count toward their sign-in lockout. It reports
// whether to go on, having answered otherwise.
func (h *Handler) confirm(w http.ResponseWriter, r *http.Request, adminID, current string) (bool, error) {
	ctx := r.Context()
	me, err := h.model.admin(ctx, adminID)
	if err != nil || me == nil {
		return false, err
	}
	if locked, err := h.locked(w, r, "admin:"+me.Email); err != nil || locked {
		return false, err
	}
	stored, err := h.model.passwordHash(ctx, adminID)
	if err != nil {
		return false, err
	}
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte(current)) != nil {
		if err := h.model.fail(ctx, "admin:"+me.Email); err != nil {
			return false, err
		}
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"current_password": "Your password is incorrect."})
		return false, nil
	}
	return true, nil
}

// createAdmin adds an admin account (confirmed with the admin's own password) and answers it with its new TOTP
// factor, shown once.
func (h *Handler) createAdmin(w http.ResponseWriter, r *http.Request, adminID string) error {
	ctx := r.Context()
	name, email := strings.TrimSpace(r.FormValue("name")), strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	pw, current := r.FormValue("password"), r.FormValue("current_password")
	if errs := checkNewAdmin(name, email, pw, current); len(errs) > 0 {
		web.WriteFields(w, http.StatusBadRequest, errs)
		return nil
	}
	if ok, err := h.confirm(w, r, adminID, current); err != nil || !ok {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a, e, err := h.model.createAccount(ctx, h.mfaKey, adminID, web.ClientIP(r), name, email, string(hash))
	if err != nil {
		return err
	}
	if a == nil {
		web.WriteFields(w, http.StatusConflict, map[string]string{"email": "An admin with this email already exists."})
		return nil
	}
	web.WriteJSON(w, http.StatusCreated, map[string]any{"admin": a, "enrolment": e})
	return nil
}

// removeAdmin deletes another admin (confirmed with the admin's own password); their sessions end with it.
func (h *Handler) removeAdmin(w http.ResponseWriter, r *http.Request, adminID string) error {
	id := r.PathValue("id")
	if id == adminID {
		web.WriteErr(w, http.StatusBadRequest, "You can't remove yourself.")
		return nil
	}
	current := r.FormValue("current_password")
	if current == "" {
		web.WriteFields(w, http.StatusBadRequest, map[string]string{"current_password": "Your password is required."})
		return nil
	}
	if ok, err := h.confirm(w, r, adminID, current); err != nil || !ok {
		return err
	}
	var u pgtype.UUID
	found := false
	if u.Scan(id) == nil {
		var err error
		if found, err = h.model.removeAccount(r.Context(), adminID, web.ClientIP(r), id); err != nil {
			return err
		}
	}
	if !found {
		web.WriteErr(w, http.StatusNotFound, "Admin not found.")
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
