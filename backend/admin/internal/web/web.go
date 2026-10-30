// Package web holds the JSON response helpers and handler wrappers every admin API package uses.
package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const maxBody = 64 << 10

// Authenticator returns the signed-in admin's user ID, or "" when there is none.
type Authenticator func(*http.Request) (string, error)

// Public caps the body size and turns a returned error into a logged 500.
func Public(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := fn(w, r); err != nil {
			fail(w, r, err)
		}
	}
}

// Private is Public that first answers 401 unless an admin is signed in, and passes their user ID on.
func Private(auth Authenticator, fn func(w http.ResponseWriter, r *http.Request, userID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		userID, err := auth(r)
		if err == nil && userID == "" {
			WriteErr(w, http.StatusUnauthorized, "Not signed in.")
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

func fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	WriteErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteErr(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

func WriteFields(w http.ResponseWriter, status int, errs map[string]string) {
	WriteJSON(w, status, map[string]map[string]string{"errors": errs})
}
