package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	failWindow = time.Hour
	maxFails   = 10 // wrong passwords, or wrong codes, per email per failWindow (counted in Postgres, every instance)

	errTooMany = "Too many attempts. Try again later."
)

// limiter is a token bucket per key, kept in Postgres (rate_limits) so every API instance shares it: each holds up to
// burst tokens and regains one every per. name prefixes its keys.
type limiter struct {
	db    *pgxpool.Pool
	name  string
	burst int
	per   time.Duration
}

// takeSQL spends one of $1's tokens (burst $2, one regained every $3 seconds) when it has one, in one atomic upsert,
// and returns whether it did and the tokens left. The database's clock is the only one used, so instances' clocks
// don't matter.
const takeSQL = `
	insert into rate_limits as r (key, tokens, at, ok) values ($1, $2::float8 - 1, statement_timestamp(), true)
	on conflict (key) do update set
	  tokens = case when ` + refill + ` >= 1 then ` + refill + ` - 1 else ` + refill + ` end,
	  at = greatest(r.at, statement_timestamp()),
	  ok = ` + refill + ` >= 1
	returning ok, tokens`

const refill = `least($2::float8, r.tokens + greatest(0, extract(epoch from statement_timestamp() - r.at)) / $3::float8)`

// take spends one of key's tokens and returns 0, or returns how long until one is available, spending nothing.
func (l limiter) take(ctx context.Context, key string) (time.Duration, error) {
	var ok bool
	var tokens float64
	if err := l.db.QueryRow(ctx, takeSQL, l.name+":"+key, l.burst, l.per.Seconds()).Scan(&ok, &tokens); err != nil {
		return 0, fmt.Errorf("rate limit %s: %w", l.name, err)
	}
	if ok {
		return 0, nil
	}
	return time.Duration((1 - tokens) * float64(l.per)), nil
}

// limits are the auth endpoints' rate limits.
type limits struct {
	ip   limiter // every rate-limited auth request, per client IP
	send limiter // emails sent (codes, reset links, verification codes), per address
}

func newLimits(db *pgxpool.Pool) limits {
	return limits{ip: limiter{db, "ip", 20, 3 * time.Second}, send: limiter{db, "send", 3, 5 * time.Minute}}
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

// limited spends one of l's tokens for key and answers 429 when there was none, reporting whether it did.
func limited(w http.ResponseWriter, r *http.Request, l limiter, key string) (bool, error) {
	wait, err := l.take(r.Context(), key)
	if err != nil {
		return false, err
	}
	return tooMany(w, wait), nil
}

// limitIP rate-limits fn per client IP.
func (h *Handler) limitIP(fn func(http.ResponseWriter, *http.Request) error) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		if stop, err := limited(w, r, h.lim.ip, clientIP(r)); err != nil || stop {
			return err
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
