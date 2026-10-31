package auth

// A copy of the user service's auth/limit.go (modules can't share internal packages); keep the two in step.
// clientIP lives in web (web.ClientIP), since the audit log records it too.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/web"
)

const (
	failWindow = time.Hour
	maxFails   = 10 // wrong passwords per email per failWindow (counted in Postgres, every instance)

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

// limited spends one of l's tokens for key and answers 429 when there was none, reporting whether it did.
func limited(w http.ResponseWriter, r *http.Request, l limiter, key string) (bool, error) {
	wait, err := l.take(r.Context(), key)
	if err != nil {
		return false, err
	}
	return tooMany(w, wait), nil
}

// tooMany answers 429 when wait is set, and reports whether it did.
func tooMany(w http.ResponseWriter, wait time.Duration) bool {
	if wait <= 0 {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	web.WriteErr(w, http.StatusTooManyRequests, errTooMany)
	return true
}
