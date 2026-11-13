package devreset

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIsLocal(t *testing.T) {
	for base, want := range map[string]bool{
		"": true, "http://localhost:5173": true, "http://127.0.0.1:5173/": true, "http://[::1]:5173": true,
		"https://taskwell.dev": false, "https://localhost.taskwell.dev": false, "::bad": false,
	} {
		if got := isLocal(base); got != want {
			t.Errorf("isLocal(%q) = %v, want %v", base, got, want)
		}
	}
}

// The confirm word is checked before anything touches the database.
func TestResetNeedsConfirm(t *testing.T) {
	h := &Handler{model: &model{}}
	for _, confirm := range []string{"", "yes", "deletee"} {
		r := httptest.NewRequest("POST", "/api/dev/reset", strings.NewReader(url.Values{"confirm": {confirm}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		if err := h.reset(w, r, ""); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), errConfirm) {
			t.Errorf("confirm %q: %d %s", confirm, w.Code, w.Body.String())
		}
	}
}
