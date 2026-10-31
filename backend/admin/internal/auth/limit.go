package auth

// A copy of the user service's auth/limit.go (modules can't share internal packages); keep the two in step.
// clientIP lives in web (web.ClientIP), since the audit log records it too.

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/devender07/tether/backend/admin/internal/web"
)

const (
	failWindow = time.Hour
	maxFails   = 10 // wrong passwords per email per failWindow (counted in Postgres, every instance)

	errTooMany = "Too many attempts. Try again later."
)

// limiter is a set of token buckets by key: each holds up to burst tokens and regains one every per.
// ponytail: in-process, so each API instance has its own buckets and N instances allow N times the rate; fine for
// abuse and flood control. Password guessing is capped globally by auth_failures.
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

// ipLimit caps sign-in attempts per client IP.
var ipLimit = newLimiter(10, 6*time.Second)

// tooMany answers 429 when wait is set, and reports whether it did.
func tooMany(w http.ResponseWriter, wait time.Duration) bool {
	if wait <= 0 {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	web.WriteErr(w, http.StatusTooManyRequests, errTooMany)
	return true
}
