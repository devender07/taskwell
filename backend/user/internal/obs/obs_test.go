package obs

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareRequestID(t *testing.T) {
	var logs bytes.Buffer
	slog.SetDefault(slog.New(requestIDHandler{slog.NewJSONHandler(&logs, nil)}))
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetUser(r.Context(), "u1")
		slog.ErrorContext(r.Context(), "request failed")
		w.WriteHeader(http.StatusTeapot)
	}))
	tests := []struct {
		name, header string
		kept         bool
	}{
		{"sane id kept", "abc-123", true},
		{"spaces replaced", "a b", false},
		{"missing generated", "", false},
		{"too long replaced", strings.Repeat("a", 65), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs.Reset()
			r := httptest.NewRequest("GET", "/x", nil)
			r.Header.Set("X-Request-Id", tt.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			id := w.Header().Get("X-Request-Id")
			if tt.kept != (id == tt.header) || id == "" {
				t.Fatalf("X-Request-Id = %q for %q", id, tt.header)
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if len(lines) != 2 {
				t.Fatalf("want an error and an access line, got %q", logs.String())
			}
			for _, l := range lines {
				if !strings.Contains(l, `"request_id":"`+id+`"`) {
					t.Errorf("log line without the id: %s", l)
				}
			}
			if !strings.Contains(lines[1], `"status":418`) || !strings.Contains(lines[1], `"user_id":"u1"`) {
				t.Errorf("access line: %s", lines[1])
			}
		})
	}
	if requests[4].Load() != int64(len(tests)) {
		t.Errorf("4xx count = %d", requests[4].Load())
	}
	SetUser(context.Background(), "nobody") // no request: a no-op
}
