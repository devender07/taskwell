package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllow(t *testing.T) {
	h := Allow(func(*http.Request) string { return "http://localhost:5173" },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	for _, tc := range []struct {
		method, origin string
		status         int
		allowed        string
	}{
		{"OPTIONS", "http://localhost:5173", http.StatusNoContent, "http://localhost:5173"},
		{"POST", "http://localhost:5173", http.StatusTeapot, "http://localhost:5173"},
		{"POST", "https://evil.test", http.StatusTeapot, ""},
		{"GET", "", http.StatusTeapot, ""},
	} {
		r := httptest.NewRequest(tc.method, "/api/me", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.method == "OPTIONS" {
			r.Header.Set("Access-Control-Request-Method", "POST")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Origin") != tc.allowed {
			t.Errorf("%s from %q: %d %q, want %d %q", tc.method, tc.origin, w.Code, w.Header().Get("Access-Control-Allow-Origin"), tc.status, tc.allowed)
		}
	}
	if got := Origin("https://app.example.com/path/"); got != "https://app.example.com" {
		t.Errorf("Origin = %q", got)
	}
}
