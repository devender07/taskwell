// Package web is the HTTP plumbing every feature package shares: JSON responses and the handler wrappers.
package web

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"strings"

	"github.com/devender07/tether/backend/user/internal/doc"
)

// Handler is a signed-in request's handler; a returned error is logged and answered with a generic 500.
type Handler func(w http.ResponseWriter, r *http.Request, userID string) error

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Err(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// Fields answers {"errors": {field: message}}.
func Fields(w http.ResponseWriter, status int, errs map[string]string) {
	JSON(w, status, map[string]map[string]string{"errors": errs})
}

// Wrap signs the request in with authenticate (401 when nobody is) and caps its body at maxBody bytes (0: the
// handler caps it itself).
func Wrap(authenticate func(*http.Request) (string, error), maxBody int64, fn Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if maxBody > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		userID, err := authenticate(r)
		if err == nil && userID == "" {
			Err(w, http.StatusUnauthorized, "Not signed in.")
			return
		}
		if err == nil {
			err = fn(w, r, userID)
		}
		if err != nil {
			fail(w, r, err)
		}
	}
}

// Public is Wrap for a request that needs no session.
func Public(maxBody int64, fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := fn(w, r); err != nil {
			fail(w, r, err)
		}
	}
}

func fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	Err(w, http.StatusInternalServerError, "Something went wrong. Try again.")
}

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// NormEmail is how emails are stored and compared: trimmed and lowercase.
func NormEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// CheckEmail is an email field's message, "" when it's valid.
func CheckEmail(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return "Email is required."
	}
	if doc.Len(s) > 254 || !emailRe.MatchString(s) {
		return "Enter a valid email address."
	}
	return ""
}

// Check answers 400 with the field messages that aren't "", and reports whether there were none.
func Check(w http.ResponseWriter, msgs map[string]string) bool {
	maps.DeleteFunc(msgs, func(_, msg string) bool { return msg == "" })
	if len(msgs) > 0 {
		Fields(w, http.StatusBadRequest, msgs)
		return false
	}
	return true
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsUUID reports whether s is a lowercase UUID, as Postgres prints them.
func IsUUID(s string) bool { return uuidRe.MatchString(s) }

// RandomToken is 32 random bytes, base64url: an unguessable token for a URL.
func RandomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error (Go 1.24+)
	return base64.RawURLEncoding.EncodeToString(b)
}
