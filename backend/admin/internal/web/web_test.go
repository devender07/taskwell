package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowIPs(t *testing.T) {
	allowed, err := ParseAllowlist(" 203.0.113.0/24, 198.51.100.7 ,2001:db8::/32,")
	if err != nil {
		t.Fatal(err)
	}
	h := AllowIPs(allowed, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, tc := range []struct {
		name, remote, xff string
		want              int
	}{
		{"in range", "203.0.113.9:4000", "", http.StatusNoContent},
		{"single address", "198.51.100.7:4000", "", http.StatusNoContent},
		{"next to the single address", "198.51.100.8:4000", "", http.StatusForbidden},
		{"ipv6 range", "[2001:db8::1]:4000", "", http.StatusNoContent},
		{"outside", "192.0.2.1:4000", "", http.StatusForbidden},
		{"through a local proxy", "127.0.0.1:4000", "203.0.113.50", http.StatusNoContent},
		{"local proxy, client outside", "127.0.0.1:4000", "192.0.2.1", http.StatusForbidden},
		{"public peer can't claim an allowed address", "192.0.2.1:4000", "203.0.113.9", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/me", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Errorf("got %d, want %d", w.Code, tc.want)
			}
		})
	}
	if none, err := ParseAllowlist(" , "); err != nil || none != nil {
		t.Errorf("empty list: %v %v, want nil (allow all)", none, err)
	}
	if _, err := ParseAllowlist("10.0.0.0/33"); err == nil {
		t.Error("bad range accepted")
	}
}
