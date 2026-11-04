package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	errBadCode   = "That code is invalid or expired."
	errBadReset  = "This reset link is invalid or expired."
	errBadLogin  = "Wrong email or password."
	errNoMail    = "Email is not configured. Contact your administrator."
	errSignedOut = "Not signed in."

	errCurrentRequired = "Current password is required."
	errCurrentWrong    = "Current password is incorrect."
	errSameEmail       = "That's already your email."
	errEmailInUse      = "That email is already in use."
)

// dummyHash keeps sign-in timing the same whether or not the email exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)

type Handler struct {
	model  *model
	cfg    *sysconfig.Store
	mail   *mail.Mailer
	secret []byte // JWT_SECRET; nil falls back to system_config's jwt_secret (dev)
	lim    limits
	alive  *aliveCache

	// APIURL is the API's public URL, where Google sends the browser back; "" means app_base_url's host serves it.
	APIURL string
}

// New takes the access token key (nil in dev, see jwtSecret) and onUserCreated, run inside the transaction that
// first verifies an account's email (it creates the default workspace).
func New(db *pgxpool.Pool, cfg *sysconfig.Store, m *mail.Mailer, secret []byte,
	onUserCreated func(ctx context.Context, tx pgx.Tx, userID, name, workspaceName string) error) *Handler {
	return &Handler{model: &model{db: db, onCreated: onUserCreated}, cfg: cfg, mail: m, secret: secret, lim: newLimits(db),
		alive: newAliveCache()}
}

// signup starts an account: it emails a code that signupVerify takes, from this browser only (the code's hash
// includes a nonce kept in a cookie). Nothing is usable until then, so signing up with someone else's email gets
// nowhere, and a later sign-up by the owner or their Google or email-code sign-in takes the account over.
func (h *Handler) signup(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	name, email, pw := r.FormValue("name"), r.FormValue("email"), r.FormValue("password")
	wsName := strings.TrimSpace(r.FormValue("workspace_name")) // optional: an invited sign-up joins the inviter's
	if errs := fieldErrors("name", checkName(name), "email", web.CheckEmail(email), "password", checkPassword(pw),
		"workspace_name", checkWorkspaceName(wsName)); errs != nil {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	email = web.NormEmail(email)
	if stop, err := web.Limited(w, r, h.lim.send, email); err != nil || stop {
		return err
	}
	if ok, err := h.mail.Configured(ctx); err != nil {
		return err
	} else if !ok {
		web.Err(w, http.StatusServiceUnavailable, errNoMail)
		return nil
	}
	minutes, err := h.cfg.GetInt(ctx, "otp_ttl_minutes")
	if err != nil {
		return err
	}
	ph, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	nonce := secret.NewToken()
	subject := "Your Taskwell verification code: " + code
	body := fmt.Sprintf("Your Taskwell verification code is %s\n\nEnter it on the sign-up page to finish creating your account. "+
		"It expires in %d minutes. If you didn't sign up for Taskwell, ignore this email.\n", code, minutes)
	err = h.model.startSignup(ctx, strings.TrimSpace(name), email, string(ph), wsName, secret.Hash(code+"."+nonce), minutes)
	if errors.Is(err, errEmailTaken) {
		// Answer exactly as for a new address, so sign-up can't tell who has an account; tell the owner instead.
		if subject, body, err = h.signupTakenMail(ctx); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: signupCookie, Value: nonce, Path: "/api/auth/signup", MaxAge: minutes * 60,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	if err := h.mail.Enqueue(ctx, h.model.db, email, subject, body); err != nil {
		return err
	}
	web.JSON(w, http.StatusAccepted, map[string]string{"email": email})
	return nil
}

// signupTakenMail is what a sign-up with an existing account's email sends that account instead of a code.
func (h *Handler) signupTakenMail(ctx context.Context) (subject, body string, err error) {
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return "", "", err
	}
	base = strings.TrimRight(base, "/")
	return "Someone tried to sign up with your email",
		fmt.Sprintf("Someone tried to create a Taskwell account with this email, but you already have one.\n\n"+
			"Sign in instead:\n\n%s/signin\n\nForgot your password? Reset it here:\n\n%s/forgot-password\n\n"+
			"If this wasn't you, you can ignore this email. Your account hasn't changed.\n", base, base), nil
}

// signupVerify finishes a sign-up with the emailed code and signs the new account in.
func (h *Handler) signupVerify(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	email, code := r.FormValue("email"), strings.TrimSpace(r.FormValue("code"))
	if errs := fieldErrors("email", web.CheckEmail(email), "code", checkCode(code)); errs != nil {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	email = web.NormEmail(email)
	if locked, err := h.locked(w, r, "code:"+email); err != nil || locked {
		return err
	}
	c, err := r.Cookie(signupCookie)
	if err != nil {
		web.Err(w, http.StatusUnauthorized, "Enter the code in the browser you signed up in, or sign up again.")
		return nil
	}
	u, err := h.model.verifySignup(ctx, email, secret.Hash(code+"."+c.Value))
	if err != nil {
		return err
	}
	if u == nil {
		if err := h.model.fail(ctx, "code:"+email); err != nil {
			return err
		}
		web.Err(w, http.StatusUnauthorized, errBadCode)
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: signupCookie, Path: "/api/auth/signup", MaxAge: -1, HttpOnly: true})
	if err := h.startSession(ctx, w, u.ID, true); errors.Is(err, errSuspended) {
		web.Err(w, http.StatusForbidden, errSuspendedMsg)
		return nil
	} else if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, u)
	return nil
}

// locked answers 429 when key has had maxFails failed attempts in the last failWindow.
func (h *Handler) locked(w http.ResponseWriter, r *http.Request, key string) (bool, error) {
	n, err := h.model.failures(r.Context(), key)
	if err != nil || n < maxFails {
		return false, err
	}
	return web.TooMany(w, failWindow), nil
}

func (h *Handler) signin(w http.ResponseWriter, r *http.Request) error {
	email, pw := r.FormValue("email"), r.FormValue("password")
	pwErr := ""
	if pw == "" {
		pwErr = "Password is required."
	}
	if errs := fieldErrors("email", web.CheckEmail(email), "password", pwErr); errs != nil {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	email = web.NormEmail(email)
	if locked, err := h.locked(w, r, "pw:"+email); err != nil || locked {
		return err
	}
	u, ph, err := h.model.userWithPassword(r.Context(), email)
	if err != nil {
		return err
	}
	stored := dummyHash
	if ph != nil {
		stored = []byte(*ph)
	}
	if bcrypt.CompareHashAndPassword(stored, []byte(pw)) != nil || ph == nil {
		if err := h.model.fail(r.Context(), "pw:"+email); err != nil {
			return err
		}
		web.Err(w, http.StatusUnauthorized, errBadLogin)
		return nil
	}
	if err := h.startSession(r.Context(), w, u.ID, remember(r)); errors.Is(err, errSuspended) {
		web.Err(w, http.StatusForbidden, errSuspendedMsg)
		return nil
	} else if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) otpSend(w http.ResponseWriter, r *http.Request) error {
	email := r.FormValue("email")
	if msg := web.CheckEmail(email); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"email": msg})
		return nil
	}
	if stop, err := web.Limited(w, r, h.lim.send, web.NormEmail(email)); err != nil || stop {
		return err
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	return h.sendSecret(w, r, web.NormEmail(email), purposeCode, "otp_ttl_minutes", code, func(minutes int) (string, string, error) {
		return "Your Taskwell sign-in code: " + code,
			fmt.Sprintf("Your Taskwell sign-in code is %s\n\nIt expires in %d minutes. If you didn't ask for it, ignore this email.\n", code, minutes), nil
	})
}

func (h *Handler) forgot(w http.ResponseWriter, r *http.Request) error {
	email := r.FormValue("email")
	if msg := web.CheckEmail(email); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"email": msg})
		return nil
	}
	if stop, err := web.Limited(w, r, h.lim.send, web.NormEmail(email)); err != nil || stop {
		return err
	}
	token := secret.NewToken()
	return h.sendSecret(w, r, web.NormEmail(email), purposeReset, "reset_ttl_minutes", token, func(minutes int) (string, string, error) {
		base, err := h.cfg.Get(r.Context(), "app_base_url")
		if err != nil {
			return "", "", err
		}
		link := strings.TrimRight(base, "/") + "/reset-password?token=" + url.QueryEscape(token)
		return "Reset your Taskwell password",
			fmt.Sprintf("Reset your Taskwell password by opening this link:\n\n%s\n\nIt expires in %d minutes. If you didn't ask for it, ignore this email.\n", link, minutes), nil
	})
}

// sendSecret stores a new single-use secret for the account (if it exists), voids older unused
// ones of the same purpose, and emails it. Always answers 204 so callers can't probe for accounts.
func (h *Handler) sendSecret(w http.ResponseWriter, r *http.Request, email, purpose, ttlKey, code string,
	compose func(minutes int) (subject, body string, err error)) error {
	ctx := r.Context()
	if ok, err := h.mail.Configured(ctx); err != nil {
		return err
	} else if !ok {
		web.Err(w, http.StatusServiceUnavailable, errNoMail)
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
	if err := h.model.replaceToken(ctx, userID, purpose, secret.Hash(code), minutes); err != nil {
		return err
	}
	subject, body, err := compose(minutes)
	if err != nil {
		return err
	}
	if err := h.mail.Enqueue(ctx, h.model.db, email, subject, body); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) otpVerify(w http.ResponseWriter, r *http.Request) error {
	email, code := r.FormValue("email"), strings.TrimSpace(r.FormValue("code"))
	if errs := fieldErrors("email", web.CheckEmail(email), "code", checkCode(code)); errs != nil {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	email = web.NormEmail(email)
	if locked, err := h.locked(w, r, "code:"+email); err != nil || locked {
		return err
	}
	u, err := h.model.useCode(r.Context(), email, secret.Hash(code))
	if err != nil {
		return err
	}
	if u == nil {
		if err := h.model.fail(r.Context(), "code:"+email); err != nil {
			return err
		}
		web.Err(w, http.StatusUnauthorized, errBadCode)
		return nil
	}
	if err := h.startSession(r.Context(), w, u.ID, remember(r)); errors.Is(err, errSuspended) {
		web.Err(w, http.StatusForbidden, errSuspendedMsg)
		return nil
	} else if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, u)
	return nil
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) error {
	token, pw := r.FormValue("token"), r.FormValue("password")
	if msg := checkPassword(pw); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"password": msg})
		return nil
	}
	ph, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	email, err := h.model.resetPassword(r.Context(), secret.Hash(token), string(ph))
	if err != nil {
		return err
	}
	if email == "" {
		web.Err(w, http.StatusUnauthorized, errBadReset)
		return nil
	}
	if err := h.passwordChangedMail(r.Context(), email, false); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// passwordCode emails the signed-in user a code that changePassword needs to add a first password, so a stolen
// session alone can't set one.
func (h *Handler) passwordCode(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID, err := h.Authenticate(r)
	if err != nil {
		return err
	}
	var p *Profile
	if userID != "" {
		if p, err = h.model.profile(ctx, userID); err != nil {
			return err
		}
	}
	if p == nil {
		web.Err(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	if stop, err := web.Limited(w, r, h.lim.send, p.Email); err != nil || stop {
		return err
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	return h.sendSecret(w, r, p.Email, purposePasswordSet, "otp_ttl_minutes", code, func(minutes int) (string, string, error) {
		return "Your Taskwell code to add a password: " + code,
			fmt.Sprintf("Your code to add a password to your Taskwell account is %s\n\nIt expires in %d minutes. "+
				"If you didn't ask for it, someone may be signed in to your account: sign out of every device by "+
				"resetting your password.\n", code, minutes), nil
	})
}

// changePassword sets a new password, checking the current one when the account has one, or else an emailed code
// from passwordCode (Google and email-code accounts may have no password). Other sessions are signed out; this one
// stays. The owner is emailed either way.
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID, err := h.Authenticate(r)
	if err != nil {
		return err
	}
	var p *Profile
	if userID != "" {
		if p, err = h.model.profile(ctx, userID); err != nil {
			return err
		}
	}
	if p == nil {
		web.Err(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	current, pw, code := r.FormValue("current_password"), r.FormValue("password"), strings.TrimSpace(r.FormValue("code"))
	old, err := h.model.passwordHash(ctx, userID)
	if err != nil {
		return err
	}
	errs := map[string]string{}
	if msg := checkPassword(pw); msg != "" {
		errs["password"] = msg
	}
	switch {
	case old != nil && current == "":
		errs["current_password"] = errCurrentRequired
	case old != nil && bcrypt.CompareHashAndPassword([]byte(*old), []byte(current)) != nil:
		errs["current_password"] = errCurrentWrong
	case old == nil && checkCode(code) != "":
		errs["code"] = checkCode(code)
	}
	if len(errs) > 0 {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	if old == nil {
		if locked, err := h.locked(w, r, "code:"+p.Email); err != nil || locked {
			return err
		}
		if ok, err := h.model.useUserCode(ctx, userID, purposePasswordSet, secret.Hash(code)); err != nil {
			return err
		} else if !ok {
			if err := h.model.fail(ctx, "code:"+p.Email); err != nil {
				return err
			}
			web.Fields(w, http.StatusBadRequest, map[string]string{"code": errBadCode})
			return nil
		}
	}
	ph, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	var keep []byte
	if c, err := r.Cookie(refreshCookie); err == nil {
		keep = secret.Hash(c.Value)
	}
	if err := h.model.setPassword(ctx, userID, string(ph), keep); err != nil {
		return err
	}
	if err := h.passwordChangedMail(ctx, p.Email, old == nil); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// emailCode starts changing the signed-in user's email: it sends one code to their current address and one to the new
// address (form field email). changeEmail needs both, so neither a stolen session nor a typo'd address can move the
// account.
func (h *Handler) emailCode(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	email := r.FormValue("email")
	if msg := web.CheckEmail(email); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"email": msg})
		return nil
	}
	email = web.NormEmail(email)
	if email == p.Email {
		web.Fields(w, http.StatusBadRequest, map[string]string{"email": errSameEmail})
		return nil
	}
	if taken, err := h.model.userIDByEmail(ctx, email); err != nil {
		return err
	} else if taken != "" {
		web.Fields(w, http.StatusConflict, map[string]string{"email": errEmailInUse})
		return nil
	}
	if stop, err := web.Limited(w, r, h.lim.send, p.Email); err != nil || stop {
		return err
	}
	if ok, err := h.mail.Configured(ctx); err != nil {
		return err
	} else if !ok {
		web.Err(w, http.StatusServiceUnavailable, errNoMail)
		return nil
	}
	minutes, err := h.cfg.GetInt(ctx, "otp_ttl_minutes")
	if err != nil {
		return err
	}
	oldCode, err := newCode()
	if err != nil {
		return err
	}
	newCodeV, err := newCode()
	if err != nil {
		return err
	}
	if err := h.model.startEmailChange(ctx, p.ID, email, secret.Hash(oldCode), secret.Hash(newCodeV), minutes); err != nil {
		return err
	}
	if err := h.mail.Enqueue(ctx, h.model.db, p.Email, "Your Taskwell code to change your email: "+oldCode,
		fmt.Sprintf("Someone asked to change your Taskwell account's email to %s. Your code to allow it is %s\n\n"+
			"It expires in %d minutes. If this wasn't you, don't share the code: someone may be signed in to your account, "+
			"so reset your password to sign them out.\n", email, oldCode, minutes)); err != nil {
		return err
	}
	if err := h.mail.Enqueue(ctx, h.model.db, email, "Your Taskwell code to confirm this email: "+newCodeV,
		fmt.Sprintf("Your code to make this your Taskwell account's email is %s\n\nIt expires in %d minutes. "+
			"If you didn't ask for it, you can ignore this email.\n", newCodeV, minutes)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// changeEmail moves the signed-in user to the address emailCode was given, with both codes (old_code from the current
// address, new_code from the new one). Other sessions are signed out; this one stays. The old address is told.
func (h *Handler) changeEmail(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	oldCode, newCodeV := strings.TrimSpace(r.FormValue("old_code")), strings.TrimSpace(r.FormValue("new_code"))
	if errs := fieldErrors("old_code", checkCode(oldCode), "new_code", checkCode(newCodeV)); errs != nil {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	if locked, err := h.locked(w, r, "code:"+p.Email); err != nil || locked {
		return err
	}
	var keep []byte
	if c, err := r.Cookie(refreshCookie); err == nil {
		keep = secret.Hash(c.Value)
	}
	from, to, bad, err := h.model.changeEmail(ctx, p.ID, secret.Hash(oldCode), secret.Hash(newCodeV), keep)
	if errors.Is(err, errEmailTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"email": errEmailInUse})
		return nil
	}
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		if err := h.model.fail(ctx, "code:"+p.Email); err != nil {
			return err
		}
		errs := map[string]string{}
		for _, f := range bad {
			errs[f] = errBadCode
		}
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return err
	}
	if err := h.mail.Enqueue(ctx, h.model.db, from, "Your Taskwell email was changed",
		fmt.Sprintf("Your Taskwell account's email was just changed from %s to %s, and other devices were signed out.\n\n"+
			"If this wasn't you, contact your workspace admin right away.\n\n%s\n", from, to, strings.TrimRight(base, "/"))); err != nil {
		return err
	}
	p.Email = to
	web.JSON(w, http.StatusOK, p)
	return nil
}

// signedIn returns the signed-in user's profile, or writes a 401 and returns nil.
func (h *Handler) signedIn(w http.ResponseWriter, r *http.Request) (*Profile, error) {
	userID, err := h.Authenticate(r)
	if err != nil {
		return nil, err
	}
	var p *Profile
	if userID != "" {
		if p, err = h.model.profile(r.Context(), userID); err != nil {
			return nil, err
		}
	}
	if p == nil {
		web.Err(w, http.StatusUnauthorized, errSignedOut)
	}
	return p, nil
}

// passwordChangedMail tells the owner a password was added (added) or changed, in case it wasn't them.
func (h *Handler) passwordChangedMail(ctx context.Context, email string, added bool) error {
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return err
	}
	subject, what := "Your Taskwell password was changed", "The password for your Taskwell account was just changed."
	if added {
		subject, what = "A password was added to your Taskwell account", "A password was just added to your Taskwell account."
	}
	return h.mail.Enqueue(ctx, h.model.db, email, subject, fmt.Sprintf("%s\n\nIf this was you, there's nothing to do.\n\n"+
		"If it wasn't, reset your password now to sign everyone else out:\n\n%s/forgot-password\n",
		what, strings.TrimRight(base, "/")))
}

// refresh trades a valid refresh token for a new access + refresh pair.
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		return h.signedOut(w, r)
	}
	userID, f, ok, err := h.model.rotateRefresh(r.Context(), secret.Hash(c.Value))
	if err != nil {
		return err
	}
	if !ok {
		return h.signedOut(w, r)
	}
	if err := h.setTokens(r.Context(), w, userID, f); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) signout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(refreshCookie); err == nil {
		if err := h.model.revokeFamily(r.Context(), secret.Hash(c.Value)); err != nil {
			return err
		}
	}
	if err := h.clearCookies(r.Context(), w); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Profile returns the user as GET /api/me answers it; nil when there is no such user.
func (h *Handler) Profile(ctx context.Context, userID string) (*Profile, error) {
	return h.model.profile(ctx, userID)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	userID, err := h.Authenticate(r)
	if err != nil {
		return err
	}
	var u *Profile
	if userID != "" {
		if u, err = h.model.profile(r.Context(), userID); err != nil {
			return err
		}
	}
	if u == nil {
		web.Err(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	web.JSON(w, http.StatusOK, u)
	return nil
}

// googleStart sends the browser to Google. Without keys in system_config it bounces back to /signin with an error.
func (h *Handler) googleStart(w http.ResponseWriter, r *http.Request) error {
	cfg, base, err := h.googleConfig(r.Context())
	if err != nil {
		return err
	}
	if cfg.clientID == "" || cfg.clientSecret == "" {
		http.Redirect(w, r, base+"/signin?error=google_unavailable", http.StatusFound)
		return nil
	}
	state, verifier := secret.NewToken(), secret.NewToken()
	rememberFlag := "0"
	if remember(r) {
		rememberFlag = "1"
	}
	secure, err := h.secure(r.Context())
	if err != nil {
		return err
	}
	next := base64.RawURLEncoding.EncodeToString([]byte(safeRedirect(r.URL.Query().Get("redirect")))) // no dots
	http.SetCookie(w, &http.Cookie{Name: googleCookie, Value: state + "." + verifier + "." + rememberFlag + "." + next,
		Path: "/api/auth/google", MaxAge: 600, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, googleConsentURL(cfg, state, verifier), http.StatusFound)
	return nil
}

// googleCallback finishes the Google round trip: checks state, exchanges the code, signs the user in and
// redirects to the app. Every failure lands on /signin?error=google.
func (h *Handler) googleCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	cfg, base, err := h.googleConfig(ctx)
	if err != nil {
		return err
	}
	fail := func(reason string, err error) error {
		slog.Warn("google sign-in failed", "reason", reason, "err", err)
		http.Redirect(w, r, base+"/signin?error=google", http.StatusFound)
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: googleCookie, Path: "/api/auth/google", MaxAge: -1, HttpOnly: true})
	if r.URL.Query().Get("error") == "access_denied" {
		http.Redirect(w, r, base+"/signin", http.StatusFound) // the user cancelled on Google's screen
		return nil
	}
	c, err := r.Cookie(googleCookie)
	if err != nil {
		return fail("missing state cookie", err)
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 4 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(r.URL.Query().Get("state"))) != 1 {
		return fail("state mismatch", nil)
	}
	id, err := exchangeGoogleCode(ctx, cfg, r.URL.Query().Get("code"), parts[1])
	if err != nil {
		return fail("code exchange", err)
	}
	if !id.EmailVerified {
		return fail("email not verified", nil)
	}
	name := strings.TrimSpace(id.Name)
	if name == "" {
		name, _, _ = strings.Cut(id.Email, "@")
	}
	u, err := h.model.userForGoogle(ctx, id.Sub, web.NormEmail(id.Email), name)
	if errors.Is(err, errEmailTaken) {
		return fail("email linked to another Google account", err)
	}
	if err != nil {
		return err
	}
	if err := h.startSession(ctx, w, u.ID, parts[2] == "1"); errors.Is(err, errSuspended) {
		http.Redirect(w, r, base+"/signin?error=suspended", http.StatusFound)
		return nil
	} else if err != nil {
		return err
	}
	next, _ := base64.RawURLEncoding.DecodeString(parts[3])
	http.Redirect(w, r, base+safeRedirect(string(next)), http.StatusFound)
	return nil
}

// safeRedirect returns v when it is a path on this site ("/invite?token=…"), else "/signin", which sends a signed-in
// user on to their workspace. It refuses "//host" and "/\host" (browsers treat both as another host) and control
// characters. Mirrors the frontend's safeRedirect.
func safeRedirect(v string) string {
	if len(v) > 2048 || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") ||
		strings.ContainsFunc(v, func(c rune) bool { return c == '\\' || c < 0x20 || c == 0x7f }) {
		return "/signin"
	}
	return v
}

// googleConfig reads the OAuth client from system_config; the redirect URI is on APIURL (or app_base_url).
func (h *Handler) googleConfig(ctx context.Context) (googleConfig, string, error) {
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return googleConfig{}, "", err
	}
	base = strings.TrimRight(base, "/")
	id, err := h.cfg.Get(ctx, "google_client_id")
	if err != nil {
		return googleConfig{}, "", err
	}
	clientSecret, err := h.cfg.Get(ctx, "google_client_secret")
	if err != nil {
		return googleConfig{}, "", err
	}
	api := base
	if h.APIURL != "" {
		api = strings.TrimRight(h.APIURL, "/")
	}
	return googleConfig{clientID: id, clientSecret: clientSecret, redirectURI: api + "/api/auth/google/callback"}, base, nil
}

// updateMe changes the signed-in user's name; the email changes through emailCode and changeEmail.
func (h *Handler) updateMe(w http.ResponseWriter, r *http.Request) error {
	userID, err := h.Authenticate(r)
	if err != nil {
		return err
	}
	if userID == "" {
		web.Err(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	name := r.FormValue("name")
	if msg := checkName(name); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"name": msg})
		return nil
	}
	u, err := h.model.updateName(r.Context(), userID, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if u == nil {
		web.Err(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	web.JSON(w, http.StatusOK, u)
	return nil
}

// listTokens returns the signed-in user's personal API tokens, newest first.
func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) error {
	userID, err := h.Authenticate(r)
	if err != nil || userID == "" {
		return h.unauthorized(w, err)
	}
	tokens, err := h.model.apiTokens(r.Context(), userID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, tokens)
	return nil
}

// createToken makes a personal API token and returns its secret, the only time it is shown.
func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) error {
	userID, err := h.Authenticate(r)
	if err != nil || userID == "" {
		return h.unauthorized(w, err)
	}
	name := r.FormValue("name")
	if msg := checkTokenName(name); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"name": msg})
		return nil
	}
	tok := apiTokenPrefix + secret.NewToken()
	t, err := h.model.insertAPIToken(r.Context(), userID, strings.TrimSpace(name), secret.Hash(tok))
	if errors.Is(err, ErrTooManyTokens) {
		web.Err(w, http.StatusConflict, fmt.Sprintf("You have %d tokens. Revoke one first.", maxAPITokens))
		return nil
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, map[string]any{"token": t, "secret": tok})
	return nil
}

// deleteToken revokes one of the signed-in user's tokens; another user's id is not found.
func (h *Handler) deleteToken(w http.ResponseWriter, r *http.Request) error {
	userID, err := h.Authenticate(r)
	if err != nil || userID == "" {
		return h.unauthorized(w, err)
	}
	id := r.PathValue("id")
	ok := web.IsUUID(id)
	if ok {
		if ok, err = h.model.deleteAPIToken(r.Context(), userID, id); err != nil {
			return err
		}
	}
	if !ok {
		web.Err(w, http.StatusNotFound, "Token not found.")
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// unauthorized answers 401, or returns err when authenticating failed.
func (h *Handler) unauthorized(w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	web.Err(w, http.StatusUnauthorized, errSignedOut)
	return nil
}

func (h *Handler) signedOut(w http.ResponseWriter, r *http.Request) error {
	if err := h.clearCookies(r.Context(), w); err != nil {
		return err
	}
	web.Err(w, http.StatusUnauthorized, errSignedOut)
	return nil
}

// --- responses ---
