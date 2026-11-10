package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/taskwell/backend/shared/totp"
	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/notify"
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
	// MFAKey seals two-factor secrets (totp.Key).
	MFAKey []byte
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
	wsName := strings.TrimSpace(r.FormValue("workspace_name")) // optional: an invited sign-up leaves it out
	terms := ""
	if r.FormValue("accept_terms") != "true" {
		terms = "Confirm you're 18 or older and accept the terms."
	}
	if errs := fieldErrors("name", checkName(name), "email", web.CheckEmail(email), "password", checkPassword(pw),
		"workspace_name", checkWorkspaceName(wsName), "accept_terms", terms); errs != nil {
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
	html, err := mail.Render(mail.Email{Preview: "Your verification code is " + code, Heading: "Confirm your email",
		Intro: "Enter this code on the sign-up page to finish creating your Taskwell account.", Code: code,
		Note: fmt.Sprintf("It expires in %d minutes. If you didn't sign up for Taskwell, you can ignore this email.", minutes)})
	if err != nil {
		return err
	}
	err = h.model.startSignup(ctx, strings.TrimSpace(name), email, string(ph), wsName, secret.Hash(code+"."+nonce), minutes)
	if errors.Is(err, errEmailTaken) {
		// Answer exactly as for a new address, so sign-up can't tell who has an account; tell the owner instead.
		if subject, body, html, err = h.signupTakenMail(ctx); err != nil {
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
	if err := h.mail.EnqueueHTML(ctx, h.model.db, email, subject, body, html); err != nil {
		return err
	}
	web.JSON(w, http.StatusAccepted, map[string]string{"email": email})
	return nil
}

// signupTakenMail is what a sign-up with an existing account's email sends that account instead of a code.
func (h *Handler) signupTakenMail(ctx context.Context) (subject, body, html string, err error) {
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return "", "", "", err
	}
	base = strings.TrimRight(base, "/")
	html, err = mail.Render(mail.Email{Preview: "You already have a Taskwell account.", Heading: "You already have an account",
		Intro:  "Someone tried to create a Taskwell account with this email, but you already have one. Sign in instead.",
		Button: "Sign in", URL: base + "/signin",
		Note: "Forgot your password? Reset it at " + base + "/forgot-password. If this wasn't you, you can ignore this email. Your account hasn't changed."})
	return "Someone tried to sign up with your email",
		fmt.Sprintf("Someone tried to create a Taskwell account with this email, but you already have one.\n\n"+
			"Sign in instead:\n\n%s/signin\n\nForgot your password? Reset it here:\n\n%s/forgot-password\n\n"+
			"If this wasn't you, you can ignore this email. Your account hasn't changed.\n", base, base), html, err
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
	return h.afterFirstStep(w, r, *u, true, "code")
}

// afterFirstStep finishes a first-step sign-in (password, email code, sign-up): it answers the user, or
// {"mfa_required": true} when two-factor is on and POST /api/auth/mfa comes next.
func (h *Handler) afterFirstStep(w http.ResponseWriter, r *http.Request, u User, remember bool, method string) error {
	mfa, err := h.begin(w, r, u.ID, remember, method)
	if errors.Is(err, errSuspended) {
		web.Err(w, http.StatusForbidden, errSuspendedMsg)
		return nil
	}
	if err != nil {
		return err
	}
	if mfa {
		web.JSON(w, http.StatusOK, map[string]bool{"mfa_required": true})
		return nil
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
		if u.ID != "" {
			h.record(r, u.ID, "sign_in_failed", "password")
		}
		web.Err(w, http.StatusUnauthorized, errBadLogin)
		return nil
	}
	return h.afterFirstStep(w, r, u, remember(r), "password")
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
	return h.sendSecret(w, r, web.NormEmail(email), purposeCode, "otp_ttl_minutes", code, func(minutes int) (string, string, string, error) {
		html, err := mail.Render(mail.Email{Preview: "Your sign-in code is " + code, Heading: "Your sign-in code",
			Intro: "Enter this code to sign in to Taskwell.", Code: code,
			Note: fmt.Sprintf("It expires in %d minutes. If you didn't ask for it, you can ignore this email. Never share this code.", minutes)})
		return "Your Taskwell sign-in code: " + code,
			fmt.Sprintf("Your Taskwell sign-in code is %s\n\nIt expires in %d minutes. If you didn't ask for it, ignore this email.\n", code, minutes), html, err
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
	return h.sendSecret(w, r, web.NormEmail(email), purposeReset, "reset_ttl_minutes", token, func(minutes int) (string, string, string, error) {
		base, err := h.cfg.Get(r.Context(), "app_base_url")
		if err != nil {
			return "", "", "", err
		}
		link := strings.TrimRight(base, "/") + "/reset-password?token=" + url.QueryEscape(token)
		html, err := mail.Render(mail.Email{Preview: "Reset your Taskwell password.", Heading: "Reset your password",
			Intro: "Choose a new password for your Taskwell account.", Button: "Reset password", URL: link,
			Note: fmt.Sprintf("The link expires in %d minutes. If you didn't ask for it, you can ignore this email.", minutes)})
		return "Reset your Taskwell password",
			fmt.Sprintf("Reset your Taskwell password by opening this link:\n\n%s\n\nIt expires in %d minutes. If you didn't ask for it, ignore this email.\n", link, minutes), html, err
	})
}

// sendSecret stores a new single-use secret for the account (if it exists), voids older unused
// ones of the same purpose, and emails it. Always answers 204 so callers can't probe for accounts.
func (h *Handler) sendSecret(w http.ResponseWriter, r *http.Request, email, purpose, ttlKey, code string,
	compose func(minutes int) (subject, body, html string, err error)) error {
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
	subject, body, html, err := compose(minutes)
	if err != nil {
		return err
	}
	if err := h.mail.EnqueueHTML(ctx, h.model.db, email, subject, body, html); err != nil {
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
		if id, err := h.model.userIDByEmail(r.Context(), email); err != nil {
			return err
		} else if id != "" {
			h.record(r, id, "sign_in_failed", "code")
		}
		web.Err(w, http.StatusUnauthorized, errBadCode)
		return nil
	}
	return h.afterFirstStep(w, r, *u, remember(r), "code")
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
	email, err := h.model.resetPassword(r.Context(), secret.Hash(token), string(ph), reqMeta(r))
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
	return h.sendSecret(w, r, p.Email, purposePasswordSet, "otp_ttl_minutes", code, func(minutes int) (string, string, string, error) {
		html, err := mail.Render(mail.Email{Preview: "Your code to add a password is " + code, Heading: "Add a password",
			Intro: "Enter this code to add a password to your Taskwell account.", Code: code,
			Note: fmt.Sprintf("It expires in %d minutes. If you didn't ask for it, someone may be signed in to your account: "+
				"sign out of every device by resetting your password.", minutes)})
		return "Your Taskwell code to add a password: " + code,
			fmt.Sprintf("Your code to add a password to your Taskwell account is %s\n\nIt expires in %d minutes. "+
				"If you didn't ask for it, someone may be signed in to your account: sign out of every device by "+
				"resetting your password.\n", code, minutes), html, err
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
	action := "password_changed"
	if old == nil {
		action = "password_added"
	}
	if err := h.model.setPassword(ctx, userID, string(ph), keep, action, reqMeta(r)); err != nil {
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
	oldHTML, err := mail.Render(mail.Email{Preview: "Your code to allow the email change is " + oldCode, Heading: "Allow the email change",
		Intro: "Someone asked to change your Taskwell account's email to " + email + ". Enter this code to allow it.", Code: oldCode,
		Note: fmt.Sprintf("It expires in %d minutes. If this wasn't you, don't share the code: someone may be signed in to your "+
			"account, so reset your password to sign them out.", minutes)})
	if err != nil {
		return err
	}
	newHTML, err := mail.Render(mail.Email{Preview: "Your code to confirm this email is " + newCodeV, Heading: "Confirm your new email",
		Intro: "Enter this code to make this your Taskwell account's email.", Code: newCodeV,
		Note: fmt.Sprintf("It expires in %d minutes. If you didn't ask for it, you can ignore this email.", minutes)})
	if err != nil {
		return err
	}
	if err := h.mail.EnqueueHTML(ctx, h.model.db, p.Email, "Your Taskwell code to change your email: "+oldCode,
		fmt.Sprintf("Someone asked to change your Taskwell account's email to %s. Your code to allow it is %s\n\n"+
			"It expires in %d minutes. If this wasn't you, don't share the code: someone may be signed in to your account, "+
			"so reset your password to sign them out.\n", email, oldCode, minutes), oldHTML); err != nil {
		return err
	}
	if err := h.mail.EnqueueHTML(ctx, h.model.db, email, "Your Taskwell code to confirm this email: "+newCodeV,
		fmt.Sprintf("Your code to make this your Taskwell account's email is %s\n\nIt expires in %d minutes. "+
			"If you didn't ask for it, you can ignore this email.\n", newCodeV, minutes), newHTML); err != nil {
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
	from, to, bad, err := h.model.changeEmail(ctx, p.ID, secret.Hash(oldCode), secret.Hash(newCodeV), keep, reqMeta(r))
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
		if err := h.model.revokeFamily(r.Context(), secret.Hash(c.Value), reqMeta(r)); err != nil {
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
	mfa, err := h.begin(w, r, u.ID, parts[2] == "1", "google")
	if errors.Is(err, errSuspended) {
		http.Redirect(w, r, base+"/signin?error=suspended", http.StatusFound)
		return nil
	} else if err != nil {
		return err
	}
	b, _ := base64.RawURLEncoding.DecodeString(parts[3])
	next := safeRedirect(string(b))
	if mfa {
		to := base + "/signin/2fa"
		if next != "/signin" {
			to += "?redirect=" + url.QueryEscape(next)
		}
		http.Redirect(w, r, to, http.StatusFound)
		return nil
	}
	http.Redirect(w, r, base+next, http.StatusFound)
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
	t, err := h.model.insertAPIToken(r.Context(), userID, strings.TrimSpace(name), secret.Hash(tok), reqMeta(r))
	if errors.Is(err, ErrTooManyTokens) {
		web.Err(w, http.StatusConflict, fmt.Sprintf("You have %d tokens. Revoke one first.", maxAPITokens))
		return nil
	}
	if err != nil {
		return err
	}
	if err := h.tokenMail(r.Context(), userID, t.Name); err != nil {
		slog.ErrorContext(r.Context(), "token created email failed", "user_id", userID, "err", err) // the secret still goes back
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
		if ok, err = h.model.deleteAPIToken(r.Context(), userID, id, reqMeta(r)); err != nil {
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

// --- two-factor ---

const (
	errMFACode     = "That code didn't match. Try the newest one."
	errMFARecovery = "That recovery code didn't match."
	errMFATimedOut = "Your sign-in timed out. Start again."
	errMFAIsOn     = "Two-factor is already on."
	errMFAIsOff    = "Two-factor is off."
	errMFANoSetup  = "Start setting up two-factor again."
	mfaIssuer      = "Taskwell"
)

// mfaVerify finishes a sign-in waiting for its second step (the taskwell_mfa cookie) with a code from the
// authenticator app or a recovery code, and starts the session. mfaMaxFails wrong codes end the pending sign-in; the
// user's wrong codes across sign-ins count toward the usual lockout too.
func (h *Handler) mfaVerify(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var c *mfaClaims
	if ck, err := r.Cookie(mfaCookie); err == nil {
		key, err := h.jwtSecret(ctx)
		if err != nil {
			return err
		}
		c, _ = parseMFA(key, ck.Value) // expired or forged: timed out
	}
	timedOut := func() error {
		if err := h.setMFACookie(ctx, w, "", -1); err != nil {
			return err
		}
		web.Err(w, http.StatusUnauthorized, errMFATimedOut)
		return nil
	}
	if c == nil {
		return timedOut()
	}
	code, recovery, field, msg, ok := mfaInput(r)
	if !ok {
		web.Fields(w, http.StatusBadRequest, map[string]string{field: msg})
		return nil
	}
	if n, err := h.model.failures(ctx, "mfa:"+c.ID); err != nil {
		return err
	} else if n >= mfaMaxFails {
		return timedOut()
	}
	if locked, err := h.locked(w, r, "mfa-user:"+c.Subject); err != nil || locked {
		return err
	}
	res, err := h.model.mfa(ctx, h.MFAKey, c.Subject, code, recovery, nil)
	if err != nil && !errors.Is(err, errMFAOff) {
		return err
	}
	if !res.ok {
		for _, k := range []string{"mfa:" + c.ID, "mfa-user:" + c.Subject} {
			if err := h.model.fail(ctx, k); err != nil {
				return err
			}
		}
		h.record(r, c.Subject, "sign_in_failed", "two-factor")
		if n, err := h.model.failures(ctx, "mfa:"+c.ID); err != nil {
			return err
		} else if n >= mfaMaxFails {
			return timedOut()
		}
		web.Fields(w, http.StatusBadRequest, map[string]string{field: msg})
		return nil
	}
	if err := h.setMFACookie(ctx, w, "", -1); err != nil {
		return err
	}
	p, err := h.model.profile(ctx, c.Subject)
	if err != nil {
		return err
	}
	if p == nil {
		return timedOut()
	}
	if err := h.startSession(w, r, p.ID, c.Remember, "two-factor"); errors.Is(err, errSuspended) {
		web.Err(w, http.StatusForbidden, errSuspendedMsg)
		return nil
	} else if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, p.User)
	return nil
}

// mfaInput reads code (from the app) or else recovery_code. When neither is usable ok is false and field and msg say
// what to fix; otherwise they are the message a wrong one gets.
func mfaInput(r *http.Request) (code, recovery, field, msg string, ok bool) {
	code, recovery = strings.TrimSpace(r.FormValue("code")), strings.TrimSpace(r.FormValue("recovery_code"))
	switch {
	case code != "":
		if m := checkCode(code); m != "" {
			return "", "", "code", m, false
		}
		return code, "", "code", errMFACode, true
	case recovery != "":
		if len(recovery) > 64 {
			return "", "", "recovery_code", errMFARecovery, false
		}
		return "", recovery, "recovery_code", errMFARecovery, true
	}
	return "", "", "code", checkCode(""), false
}

// mfaStatus answers whether two-factor is on and how many recovery codes are left.
func (h *Handler) mfaStatus(w http.ResponseWriter, r *http.Request) error {
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	on, left, err := h.model.mfaStatus(r.Context(), p.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"enabled": on, "recovery_left": left})
	return nil
}

// mfaSetup starts (or restarts) turning two-factor on and answers the new secret. It takes the password, or for an
// account without one an email_code from passwordCode, so a stolen session alone can't tie the account to a new app.
func (h *Handler) mfaSetup(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	if on, _, err := h.model.mfaStatus(ctx, p.ID); err != nil {
		return err
	} else if on {
		web.Err(w, http.StatusConflict, errMFAIsOn)
		return nil
	}
	if p.HasPassword {
		pw := r.FormValue("password")
		if pw == "" {
			web.Fields(w, http.StatusBadRequest, map[string]string{"password": "Password is required."})
			return nil
		}
		if locked, err := h.locked(w, r, "pw:"+p.Email); err != nil || locked {
			return err
		}
		old, err := h.model.passwordHash(ctx, p.ID)
		if err != nil {
			return err
		}
		if old == nil || bcrypt.CompareHashAndPassword([]byte(*old), []byte(pw)) != nil {
			if err := h.model.fail(ctx, "pw:"+p.Email); err != nil {
				return err
			}
			web.Fields(w, http.StatusBadRequest, map[string]string{"password": "Wrong password."})
			return nil
		}
	} else {
		code := strings.TrimSpace(r.FormValue("email_code"))
		if msg := checkCode(code); msg != "" {
			web.Fields(w, http.StatusBadRequest, map[string]string{"email_code": msg})
			return nil
		}
		if locked, err := h.locked(w, r, "code:"+p.Email); err != nil || locked {
			return err
		}
		if ok, err := h.model.useUserCode(ctx, p.ID, purposePasswordSet, secret.Hash(code)); err != nil {
			return err
		} else if !ok {
			if err := h.model.fail(ctx, "code:"+p.Email); err != nil {
				return err
			}
			web.Fields(w, http.StatusBadRequest, map[string]string{"email_code": errBadCode})
			return nil
		}
	}
	s := totp.NewSecret()
	sealed, err := totp.Seal(h.MFAKey, p.ID, s)
	if err != nil {
		return err
	}
	if err := h.model.startMFA(ctx, p.ID, sealed); errors.Is(err, errMFAOn) {
		web.Err(w, http.StatusConflict, errMFAIsOn)
		return nil
	} else if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]string{"secret": s, "otpauth_url": totp.URL(mfaIssuer, p.Email, s)})
	return nil
}

// mfaEnable turns two-factor on with a code from the app just set up, and answers the recovery codes (shown once).
func (h *Handler) mfaEnable(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if msg := checkCode(code); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"code": msg})
		return nil
	}
	if locked, err := h.locked(w, r, "mfa-user:"+p.ID); err != nil || locked {
		return err
	}
	codes, hashes := totp.NewRecoveryCodes()
	ok, err := h.model.enableMFA(ctx, h.MFAKey, p.ID, code, hashes, time.Now(), reqMeta(r))
	switch {
	case errors.Is(err, errMFAOn):
		web.Err(w, http.StatusConflict, errMFAIsOn)
		return nil
	case errors.Is(err, errMFAOff):
		web.Err(w, http.StatusConflict, errMFANoSetup)
		return nil
	case err != nil:
		return err
	case !ok:
		if err := h.model.fail(ctx, "mfa-user:"+p.ID); err != nil {
			return err
		}
		web.Fields(w, http.StatusBadRequest, map[string]string{"code": errMFACode})
		return nil
	}
	if err := h.mfaMail(ctx, p.Email, true); err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string][]string{"recovery_codes": codes})
	return nil
}

// mfaDisable turns two-factor off with a code from the app or a recovery code, and signs out the other sessions.
func (h *Handler) mfaDisable(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	code, recovery, field, msg, ok := mfaInput(r)
	if !ok {
		web.Fields(w, http.StatusBadRequest, map[string]string{field: msg})
		return nil
	}
	if locked, err := h.locked(w, r, "mfa-user:"+p.ID); err != nil || locked {
		return err
	}
	keep := "" // this sign-in: the refresh cookie doesn't come to /api/me
	if c, err := h.access(r); err != nil {
		return err
	} else if c != nil {
		keep = c.SID
	}
	ok, err = h.model.disableMFA(ctx, h.MFAKey, p.ID, code, recovery, keep, reqMeta(r))
	if errors.Is(err, errMFAOff) {
		web.Err(w, http.StatusConflict, errMFAIsOff)
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		if err := h.model.fail(ctx, "mfa-user:"+p.ID); err != nil {
			return err
		}
		web.Fields(w, http.StatusBadRequest, map[string]string{field: msg})
		return nil
	}
	if err := h.mfaMail(ctx, p.Email, false); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// mfaRecovery replaces the recovery codes with a new set, given a code from the app.
func (h *Handler) mfaRecovery(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if msg := checkCode(code); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"code": msg})
		return nil
	}
	if locked, err := h.locked(w, r, "mfa-user:"+p.ID); err != nil || locked {
		return err
	}
	codes, hashes := totp.NewRecoveryCodes()
	ok, err := h.model.replaceRecovery(ctx, h.MFAKey, p.ID, code, hashes)
	if errors.Is(err, errMFAOff) {
		web.Err(w, http.StatusConflict, errMFAIsOff)
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		if err := h.model.fail(ctx, "mfa-user:"+p.ID); err != nil {
			return err
		}
		web.Fields(w, http.StatusBadRequest, map[string]string{"code": errMFACode})
		return nil
	}
	web.JSON(w, http.StatusOK, map[string][]string{"recovery_codes": codes})
	return nil
}

// mfaMail tells the owner two-factor was turned on or off, in case it wasn't them.
func (h *Handler) mfaMail(ctx context.Context, email string, on bool) error {
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return err
	}
	subject, what := "Two-factor sign-in turned off", "Two-factor sign-in was just turned off for your Taskwell account, and other devices were signed out."
	if on {
		subject, what = "Two-factor sign-in turned on", "Two-factor sign-in was just turned on for your Taskwell account. Signing in now also asks for a code from your authenticator app."
	}
	return h.mail.Enqueue(ctx, h.model.db, email, subject, fmt.Sprintf("%s\n\nIf this was you, there's nothing to do.\n\n"+
		"If it wasn't, reset your password now to sign everyone else out:\n\n%s/forgot-password\n",
		what, strings.TrimRight(base, "/")))
}

// --- account events, export and deletion ---

// reqMeta is the request's client IP (when it is one) and browser, for an account event.
func reqMeta(r *http.Request) meta {
	ip := web.ClientIP(r)
	if _, err := netip.ParseAddr(ip); err != nil {
		ip = ""
	}
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = strings.ToValidUTF8(ua[:512], "")
	}
	return meta{ip: ip, ua: ua}
}

// record adds an account event outside any transaction, best effort: a failure is logged, not answered.
func (h *Handler) record(r *http.Request, userID, action, detail string) {
	if err := addEvent(r.Context(), h.model.db, userID, action, detail, reqMeta(r)); err != nil {
		slog.ErrorContext(r.Context(), "account event failed", "user_id", userID, "action", action, "err", err)
	}
}

// tokenMail tells the user a personal API token was created, in case it wasn't them.
func (h *Handler) tokenMail(ctx context.Context, userID, name string) error {
	p, err := h.model.profile(ctx, userID)
	if err != nil || p == nil {
		return err
	}
	return h.mail.Enqueue(ctx, h.model.db, p.Email, "A new API token was created",
		fmt.Sprintf("A new API token named \"%s\" was just created for your Taskwell account.\n\n"+
			"If this wasn't you, delete it in Settings → API tokens and reset your password.\n", name))
}

// accountEvents lists a page of the signed-in user's security activity, newest first (?before= the last id shown).
func (h *Handler) accountEvents(w http.ResponseWriter, r *http.Request) error {
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	before := r.URL.Query().Get("before")
	if before != "" && !web.IsUUID(before) {
		web.Err(w, http.StatusBadRequest, "Invalid cursor.")
		return nil
	}
	es, more, err := h.model.accountEvents(r.Context(), p.ID, before)
	if err != nil {
		return err
	}
	if es == nil {
		es = []AccountEvent{}
	}
	web.JSON(w, http.StatusOK, map[string]any{"events": es, "has_more": more})
	return nil
}

// exportMe downloads the signed-in user's personal data (not their workspaces' content: its admins export that).
func (h *Handler) exportMe(w http.ResponseWriter, r *http.Request) error {
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	if stop, err := web.Limited(w, r, h.lim.export, p.ID); err != nil || stop {
		return err
	}
	data, err := h.model.exportData(r.Context(), p.ID)
	if err != nil {
		return err
	}
	s, err := notify.UserSettings(r.Context(), h.model.db, p.ID)
	if err != nil {
		return err
	}
	if data["notification_settings"], err = json.Marshal(s); err != nil {
		return fmt.Errorf("export personal data: %w", err)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="taskwell-my-data.json"`)
	web.JSON(w, http.StatusOK, data)
	return nil
}

// deleteCode emails the signed-in user the code deleteMe needs, so a stolen session alone can't delete the account.
func (h *Handler) deleteCode(w http.ResponseWriter, r *http.Request) error {
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	if stop, err := web.Limited(w, r, h.lim.send, p.Email); err != nil || stop {
		return err
	}
	code, err := newCode()
	if err != nil {
		return err
	}
	return h.sendSecret(w, r, p.Email, purposeAccountDelete, "otp_ttl_minutes", code, func(minutes int) (string, string, string, error) {
		note := fmt.Sprintf("Deleting your account can't be undone. It expires in %d minutes. If you didn't ask for it, "+
			"don't share it: someone may be signed in to your account, so reset your password to sign them out.", minutes)
		html, err := mail.Render(mail.Email{Preview: "Your code to delete your account is " + code, Heading: "Confirm account deletion",
			Intro: "Enter this code to delete your Taskwell account.", Code: code, Note: note})
		return "Your Taskwell code to delete your account: " + code,
			fmt.Sprintf("Your code to delete your Taskwell account is %s\n\n%s\n", code, note), html, err
	})
}

// deleteMe deletes the signed-in user's account with the code from deleteCode, and signs them out. It refuses (409,
// naming the workspaces) while they're the only admin of a workspace with other members, or their plan is still
// billed.
func (h *Handler) deleteMe(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, err := h.signedIn(w, r)
	if p == nil || err != nil {
		return err
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if msg := checkCode(code); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"code": msg})
		return nil
	}
	if locked, err := h.locked(w, r, "code:"+p.Email); err != nil || locked {
		return err
	}
	email, refs, err := h.model.deleteAccount(ctx, p.ID, secret.Hash(code))
	refused := func(msg string) error {
		web.JSON(w, http.StatusConflict, map[string]any{"error": msg, "workspaces": refs})
		return nil
	}
	switch {
	case errors.Is(err, errSharedWorkspaces):
		return refused("Make someone else an admin of these workspaces, or remove the other members, first.")
	case errors.Is(err, errPaidPlan):
		if refs == nil {
			refs = []WorkspaceRef{}
		}
		return refused("Cancel the plan for these workspaces first.")
	case errors.Is(err, errDeleteCode):
		if err := h.model.fail(ctx, "code:"+p.Email); err != nil {
			return err
		}
		web.Fields(w, http.StatusBadRequest, map[string]string{"code": "That code is wrong or has expired."})
		return nil
	case err != nil:
		return err
	}
	if err := h.mail.Enqueue(ctx, h.model.db, email, "Your Taskwell account was deleted",
		"Your Taskwell account and the workspaces where you were the only member were deleted, as you asked.\n\n"+
			"Your issues and comments in shared workspaces stay, shown as \"Deleted user\".\n"); err != nil {
		slog.ErrorContext(ctx, "account deleted email failed", "err", err) // the account is gone either way
	}
	if err := h.clearCookies(ctx, w); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
