package auth

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devender07/tether/backend/user/internal/dbtest"
)

func TestLimiter(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := t.Context()
	l := limiter{pool, dbtest.Name("test"), 3, time.Minute}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from rate_limits where key like $1`, l.name+":%")
	})
	take := func(key string) time.Duration {
		t.Helper()
		wait, err := l.take(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		return wait
	}
	// age moves key's bucket d into the past, as if d had gone by.
	age := func(key string, d time.Duration) {
		t.Helper()
		if _, err := pool.Exec(ctx, `update rate_limits set at = at - make_interval(secs => $2) where key = $1`, l.name+":"+key, d.Seconds()); err != nil {
			t.Fatal(err)
		}
	}
	near := func(got, want time.Duration) bool { return got > want-2*time.Second && got <= want }
	for i := range 3 {
		if wait := take("a"); wait != 0 {
			t.Fatalf("take %d: refused, wait %v", i, wait)
		}
	}
	if wait := take("a"); !near(wait, time.Minute) {
		t.Fatalf("empty bucket: wait %v, want 1m", wait)
	}
	if wait := take("b"); wait != 0 {
		t.Fatal("keys share a bucket")
	}
	age("a", 30*time.Second)
	if wait := take("a"); !near(wait, 30*time.Second) {
		t.Fatalf("half refilled: wait %v, want 30s", wait)
	}
	age("a", 30*time.Second)
	if wait := take("a"); wait != 0 {
		t.Fatalf("refilled one token: wait %v", wait)
	}
	if wait := take("a"); wait == 0 {
		t.Fatal("refill gave more than one token")
	}
	age("a", time.Hour)
	for i := range 3 {
		if wait := take("a"); wait != 0 {
			t.Fatalf("after an hour, take %d: refused", i)
		}
	}
	if wait := take("a"); wait == 0 {
		t.Fatal("refill went past the burst")
	}
}

// TestLimiterConcurrent checks the upsert is atomic: of many simultaneous takes, exactly burst succeed.
func TestLimiterConcurrent(t *testing.T) {
	pool := dbtest.Pool(t)
	l := limiter{pool, dbtest.Name("test"), 5, time.Hour}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from rate_limits where key like $1`, l.name+":%")
	})
	var ok atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			wait, err := l.take(t.Context(), "k")
			if err != nil {
				t.Error(err)
			}
			if wait == 0 {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 5 {
		t.Fatalf("%d takes succeeded, want 5", ok.Load())
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
