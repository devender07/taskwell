package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := newLimiter(3, time.Minute)
	now := time.Unix(1_000, 0)
	for i := range 3 {
		if wait := l.take("a", now); wait != 0 {
			t.Fatalf("take %d: refused, wait %v", i, wait)
		}
	}
	if wait := l.take("a", now); wait != time.Minute {
		t.Fatalf("empty bucket: wait %v, want 1m", wait)
	}
	if wait := l.take("b", now); wait != 0 {
		t.Fatal("keys share a bucket")
	}
	if wait := l.take("a", now.Add(30*time.Second)); wait != 30*time.Second {
		t.Fatalf("half refilled: wait %v, want 30s", wait)
	}
	if wait := l.take("a", now.Add(time.Minute)); wait != 0 {
		t.Fatalf("refilled one token: wait %v", wait)
	}
	if wait := l.take("a", now.Add(time.Minute)); wait == 0 {
		t.Fatal("refill gave more than one token")
	}
	l.prune(now.Add(time.Hour))
	if len(l.buckets) != 0 {
		t.Fatalf("prune kept %d full buckets", len(l.buckets))
	}
}

func TestClientIP(t *testing.T) {
	for _, tc := range []struct{ remote, xff, want string }{
		{"203.0.113.9:4000", "", "203.0.113.9"},
		{"203.0.113.9:4000", "198.51.100.1", "203.0.113.9"}, // a public peer can't claim another address
		{"127.0.0.1:4000", "198.51.100.1", "198.51.100.1"},
		{"10.0.0.5:4000", "1.1.1.1, 198.51.100.1", "198.51.100.1"}, // the proxy's own entry is the last one
		{"10.0.0.5:4000", "junk", "10.0.0.5"},
		{"[::1]:4000", "", "::1"},
	} {
		r := httptest.NewRequest("POST", "/api/auth/signin", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := clientIP(r); got != tc.want {
			t.Errorf("%s xff %q: got %s, want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	const base = "https://app.tether.so/"
	for _, tc := range []struct {
		name, site, origin string
		want               bool
	}{
		{"same origin", "same-origin", "https://app.tether.so", true},
		{"typed in", "none", "", true},
		{"cross site", "cross-site", "https://evil.example", false},
		{"sibling subdomain", "same-site", "https://evil.tether.so", false},
		{"API on its own subdomain", "same-site", "https://app.tether.so", true},
		{"old browser, our origin", "", "https://app.tether.so", true},
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
