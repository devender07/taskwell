// Package auth implements the user service's email/password and email-code auth API.
package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

const (
	accessCookie  = "tether_access"
	refreshCookie = "tether_refresh"
	maxAttempts   = 5
	maxBody       = 64 << 10

	// reuseGrace lets two tabs refresh at the same moment without the second one looking like token theft.
	reuseGrace = 10 * time.Second

	purposeCode  = "signin_code"
	purposeReset = "password_reset"
)

const (
	errBadCode   = "That code is invalid or expired."
	errBadReset  = "This reset link is invalid or expired."
	errBadLogin  = "Wrong email or password."
	errSignedOut = "Not signed in."
)

var (
	// dummyHash keeps sign-in timing the same whether or not the email exists.
	dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Handler struct {
	db   *pgxpool.Pool
	cfg  *sysconfig.Store
	mail *mail.Mailer
}

func New(db *pgxpool.Pool, cfg *sysconfig.Store, m *mail.Mailer) *Handler {
	return &Handler{db: db, cfg: cfg, mail: m}
}

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/signup", h.wrap(h.signup))
	mux.HandleFunc("POST /api/auth/signin", h.wrap(h.signin))
	mux.HandleFunc("POST /api/auth/otp/send", h.wrap(h.otpSend))
	mux.HandleFunc("POST /api/auth/otp/verify", h.wrap(h.otpVerify))
	mux.HandleFunc("POST /api/auth/password/forgot", h.wrap(h.forgot))
	mux.HandleFunc("POST /api/auth/password/reset", h.wrap(h.reset))
	mux.HandleFunc("POST /api/auth/refresh", h.wrap(h.refresh))
	mux.HandleFunc("POST /api/auth/signout", h.wrap(h.signout))
	mux.HandleFunc("GET /api/me", h.wrap(h.me))
}

// wrap caps the body size and turns a returned error into a logged 500.
func (h *Handler) wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := fn(w, r); err != nil {
			slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Something went wrong. Try again."})
		}
	}
}

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

// --- handlers ---

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
	var u User
	err = h.db.QueryRow(r.Context(),
		`insert into users (name, email, password_hash) values ($1, $2, $3) returning id::text, name, email`,
		strings.TrimSpace(name), normEmail(email), string(ph)).Scan(&u.ID, &u.Name, &u.Email)
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeFields(w, http.StatusConflict, map[string]string{"email": "An account with this email already exists."})
		return nil
	}
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	if err := h.startSession(r.Context(), w, u.ID, nil); err != nil {
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
	var u User
	var ph *string
	err := h.db.QueryRow(r.Context(),
		`select id::text, name, email, password_hash from users where email = $1`, normEmail(email)).
		Scan(&u.ID, &u.Name, &u.Email, &ph)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("find user: %w", err)
	}
	stored := dummyHash
	if ph != nil {
		stored = []byte(*ph)
	}
	if bcrypt.CompareHashAndPassword(stored, []byte(pw)) != nil || ph == nil {
		writeErr(w, http.StatusUnauthorized, errBadLogin)
		return nil
	}
	if err := h.startSession(r.Context(), w, u.ID, nil); err != nil {
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
	return h.issue(w, r, normEmail(email), purposeCode, "otp_ttl_minutes", code, func(minutes int) (string, string, error) {
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
	return h.issue(w, r, normEmail(email), purposeReset, "reset_ttl_minutes", token, func(minutes int) (string, string, error) {
		base, err := h.cfg.Get(r.Context(), "app_base_url")
		if err != nil {
			return "", "", err
		}
		link := strings.TrimRight(base, "/") + "/reset-password?token=" + url.QueryEscape(token)
		return "Reset your Tether password",
			fmt.Sprintf("Reset your Tether password by opening this link:\n\n%s\n\nIt expires in %d minutes. If you didn't ask for it, ignore this email.\n", link, minutes), nil
	})
}

// issue stores a new single-use secret for the account (if it exists), voids older unused
// ones of the same purpose, and emails it. Always answers 204 so callers can't probe for accounts.
func (h *Handler) issue(w http.ResponseWriter, r *http.Request, email, purpose, ttlKey, secret string,
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
	var userID string
	err = h.db.QueryRow(ctx, `select id::text from users where email = $1`, email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	err = pgx.BeginFunc(ctx, h.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where user_id = $1 and purpose = $2 and used_at is null`, userID, purpose); err != nil {
			return fmt.Errorf("void old tokens: %w", err)
		}
		if _, err := tx.Exec(ctx, `insert into auth_tokens (user_id, purpose, token_hash, expires_at) values ($1, $2, $3, now() + make_interval(mins => $4))`,
			userID, purpose, hash(secret), minutes); err != nil {
			return fmt.Errorf("insert token: %w", err)
		}
		return nil
	})
	if err != nil {
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
	ctx := r.Context()
	var u *User
	err := pgx.BeginFunc(ctx, h.db, func(tx pgx.Tx) error {
		var tokenID string
		var stored []byte
		var cand User
		err := tx.QueryRow(ctx, `
			select t.id::text, t.token_hash, u.id::text, u.name, u.email
			from auth_tokens t join users u on u.id = t.user_id
			where u.email = $1 and t.purpose = $2 and t.used_at is null and t.expires_at > now() and t.attempts < $3
			order by t.created_at desc limit 1
			for update of t`, normEmail(email), purposeCode, maxAttempts).
			Scan(&tokenID, &stored, &cand.ID, &cand.Name, &cand.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find code: %w", err)
		}
		if subtle.ConstantTimeCompare(stored, hash(code)) != 1 {
			if _, err := tx.Exec(ctx, `update auth_tokens set attempts = attempts + 1 where id = $1`, tokenID); err != nil {
				return fmt.Errorf("count attempt: %w", err)
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where id = $1`, tokenID); err != nil {
			return fmt.Errorf("use code: %w", err)
		}
		u = &cand
		return nil
	})
	if err != nil {
		return err
	}
	if u == nil {
		writeErr(w, http.StatusUnauthorized, errBadCode)
		return nil
	}
	if err := h.startSession(ctx, w, u.ID, nil); err != nil {
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
	ctx := r.Context()
	ok := false
	err = pgx.BeginFunc(ctx, h.db, func(tx pgx.Tx) error {
		var tokenID, userID string
		err := tx.QueryRow(ctx, `
			select id::text, user_id::text from auth_tokens
			where token_hash = $1 and purpose = $2 and used_at is null and expires_at > now()
			for update`, hash(token), purposeReset).Scan(&tokenID, &userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find reset token: %w", err)
		}
		if _, err := tx.Exec(ctx, `update auth_tokens set used_at = now() where id = $1`, tokenID); err != nil {
			return fmt.Errorf("use reset token: %w", err)
		}
		if _, err := tx.Exec(ctx, `update users set password_hash = $2 where id = $1`, userID, string(ph)); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		if _, err := tx.Exec(ctx, `delete from refresh_tokens where user_id = $1`, userID); err != nil {
			return fmt.Errorf("sign out sessions: %w", err)
		}
		ok = true
		return nil
	})
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

// refresh trades a valid refresh token for a new access + refresh pair. Presenting an already
// rotated token (outside the grace window) means it leaked, so the whole sign-in family is revoked.
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		return h.signedOut(w, r)
	}
	ctx := r.Context()
	var userID, familyID string
	var usedAt *time.Time
	err = pgx.BeginFunc(ctx, h.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			select user_id::text, family_id::text, used_at from refresh_tokens
			where token_hash = $1 and expires_at > now()
			for update`, hash(c.Value)).Scan(&userID, &familyID, &usedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			userID = ""
			return nil
		}
		if err != nil {
			return fmt.Errorf("find refresh token: %w", err)
		}
		if usedAt != nil {
			if time.Since(*usedAt) > reuseGrace {
				slog.Warn("refresh token reuse, revoking family", "user_id", userID, "family_id", familyID)
				if _, err := tx.Exec(ctx, `delete from refresh_tokens where family_id = $1`, familyID); err != nil {
					return fmt.Errorf("revoke family: %w", err)
				}
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `update refresh_tokens set used_at = now() where token_hash = $1`, hash(c.Value)); err != nil {
			return fmt.Errorf("rotate refresh token: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if userID == "" {
		return h.signedOut(w, r)
	}
	if usedAt != nil {
		// Within the grace window another request already rotated it and set fresh cookies; don't clear them.
		writeErr(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	if err := h.startSession(ctx, w, userID, &familyID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) signout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(refreshCookie); err == nil {
		if _, err := h.db.Exec(r.Context(),
			`delete from refresh_tokens where family_id = (select family_id from refresh_tokens where token_hash = $1)`,
			hash(c.Value)); err != nil {
			return fmt.Errorf("revoke refresh tokens: %w", err)
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
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	var u User
	err = h.db.QueryRow(r.Context(), `select id::text, name, email from users where id = $1`, userID).Scan(&u.ID, &u.Name, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, errSignedOut)
		return nil
	}
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	writeJSON(w, http.StatusOK, u)
	return nil
}

// --- tokens ---

// authenticate returns the user ID from a valid access token cookie, or "" when there is none.
func (h *Handler) authenticate(r *http.Request) (string, error) {
	c, err := r.Cookie(accessCookie)
	if err != nil {
		return "", nil
	}
	secret, err := h.jwtSecret(r.Context())
	if err != nil {
		return "", err
	}
	userID, err := parseAccess(secret, c.Value)
	if err != nil {
		return "", nil // expired or forged: the client refreshes or signs in again
	}
	return userID, nil
}

// startSession issues an access token and a refresh token (in familyID, or a new family) as cookies.
// The refresh cookie is scoped to /api/auth so it only travels to refresh and sign-out.
func (h *Handler) startSession(ctx context.Context, w http.ResponseWriter, userID string, familyID *string) error {
	refreshHours, err := h.cfg.GetInt(ctx, "refresh_ttl_hours")
	if err != nil {
		return err
	}
	accessMinutes, err := h.cfg.GetInt(ctx, "access_ttl_minutes")
	if err != nil {
		return err
	}
	secret, err := h.jwtSecret(ctx)
	if err != nil {
		return err
	}
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	accessTTL, refreshTTL := time.Duration(accessMinutes)*time.Minute, time.Duration(refreshHours)*time.Hour
	access, err := signAccess(secret, userID, accessTTL)
	if err != nil {
		return err
	}
	refresh := newToken()
	if _, err := h.db.Exec(ctx,
		`insert into refresh_tokens (token_hash, user_id, family_id, expires_at) values ($1, $2, coalesce($3::uuid, uuidv7()), $4)`,
		hash(refresh), userID, familyID, time.Now().Add(refreshTTL)); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: access, Path: "/api", MaxAge: int(accessTTL.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: refresh, Path: "/api/auth", MaxAge: int(refreshTTL.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	return nil
}

func (h *Handler) signedOut(w http.ResponseWriter, r *http.Request) error {
	if err := h.clearCookies(r.Context(), w); err != nil {
		return err
	}
	writeErr(w, http.StatusUnauthorized, errSignedOut)
	return nil
}

func (h *Handler) clearCookies(ctx context.Context, w http.ResponseWriter) error {
	secure, err := h.secure(ctx)
	if err != nil {
		return err
	}
	for name, path := range map[string]string{accessCookie: "/api", refreshCookie: "/api/auth"} {
		http.SetCookie(w, &http.Cookie{Name: name, Path: path, MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	}
	return nil
}

func (h *Handler) jwtSecret(ctx context.Context) ([]byte, error) {
	s, err := h.cfg.Get(ctx, "jwt_secret")
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, errors.New("jwt_secret is empty")
	}
	return []byte(s), nil
}

func (h *Handler) secure(ctx context.Context) (bool, error) {
	base, err := h.cfg.Get(ctx, "app_base_url")
	return strings.HasPrefix(base, "https://"), err
}
