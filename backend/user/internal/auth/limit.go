package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	failWindow = time.Hour
	maxFails   = 10 // wrong passwords, or wrong codes, per email per failWindow (counted in Postgres, every instance)

	errTooMany = "Too many attempts. Try again later."
)

// limiter is a set of token buckets by key: each holds up to burst tokens and regains one every per.
// ponytail: in-process, so each API instance has its own buckets and N instances allow N times the rate; fine for
// abuse and flood control. Brute force is capped globally by auth_failures instead.
type limiter struct {
	mu      sync.Mutex
	burst   float64
	per     time.Duration
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

const maxBuckets = 100_000

func newLimiter(burst int, per time.Duration) *limiter {
	return &limiter{burst: float64(burst), per: per, buckets: map[string]*bucket{}}
}

// take spends one of key's tokens and returns 0, or returns how long until one is available, spending nothing.
func (l *limiter) take(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxBuckets {
			l.prune(now)
		}
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+float64(now.Sub(b.at))/float64(l.per))
	b.at = now
	if b.tokens < 1 {
		return time.Duration((1 - b.tokens) * float64(l.per))
	}
	b.tokens--
	return 0
}

// prune drops the buckets that have refilled; they behave the same as missing ones. Called with mu held.
func (l *limiter) prune(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+float64(now.Sub(b.at))/float64(l.per) >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// limits are the auth endpoints' in-process rate limits.
type limits struct {
	ip   *limiter // every rate-limited auth request, per client IP
	send *limiter // emails sent (codes, reset links, verification codes), per address
}

func newLimits() limits {
	return limits{ip: newLimiter(20, 3*time.Second), send: newLimiter(3, 5*time.Minute)}
}

// tooMany answers 429 when wait is set, and reports whether it did.
func tooMany(w http.ResponseWriter, wait time.Duration) bool {
	if wait <= 0 {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	writeErr(w, http.StatusTooManyRequests, errTooMany)
	return true
}

// limitIP rate-limits fn per client IP.
func (h *Handler) limitIP(fn func(http.ResponseWriter, *http.Request) error) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		if tooMany(w, h.lim.ip.take(clientIP(r), time.Now())) {
			return nil
		}
		return fn(w, r)
	}
}

// clientIP is the request's peer address, or, when the peer is a proxy on this host or a private network, the
// address that proxy appended last to X-Forwarded-For (Caddy and nginx's $proxy_add_x_forwarded_for do).
// ponytail: any private-network peer is trusted as a proxy; pin a TRUSTED_PROXIES list if clients can reach the API
// from inside that network.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !(peer.IsLoopback() || peer.IsPrivate()) {
		return host
	}
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return host
	}
	parts := strings.Split(xff[len(xff)-1], ",")
	if last, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
		return last.String()
	}
	return host
}
