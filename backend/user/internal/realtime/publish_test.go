package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithOrigin(t *testing.T) {
	tests := []struct{ name, header, want string }{
		{"client id", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
		{"none", "", ""},
		{"too long", strings.Repeat("A", 65), ""},
		{"not a token", "abc def", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := WithOrigin(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = origin(r.Context()) }))
			r := httptest.NewRequest(http.MethodPost, "/api/workspaces/acme/issues/ENG-1", nil)
			if tt.header != "" {
				r.Header.Set("X-Tether-Client", tt.header)
			}
			h.ServeHTTP(httptest.NewRecorder(), r)
			if got != tt.want {
				t.Errorf("origin = %q, want %q", got, tt.want)
			}
		})
	}
}
