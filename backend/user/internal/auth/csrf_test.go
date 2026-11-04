package auth

import (
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	const base = "https://app.taskwell.so/"
	for _, tc := range []struct {
		name, site, origin string
		want               bool
	}{
		{"same origin", "same-origin", "https://app.taskwell.so", true},
		{"typed in", "none", "", true},
		{"cross site", "cross-site", "https://evil.example", false},
		{"sibling subdomain", "same-site", "https://evil.taskwell.so", false},
		{"API on its own subdomain", "same-site", "https://app.taskwell.so", true},
		{"old browser, our origin", "", "https://app.taskwell.so", true},
		{"old browser, other origin", "", "https://evil.example", false},
		{"opaque origin", "cross-site", "null", false},
		{"not a browser", "", "", true},
	} {
		r := httptest.NewRequest("POST", "/api/auth/signin", nil)
		if tc.site != "" {
			r.Header.Set("Sec-Fetch-Site", tc.site)
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if got := sameOrigin(r, base); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
