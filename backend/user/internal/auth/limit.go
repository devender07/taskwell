package auth

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/web"
)

const (
	failWindow = time.Hour
	maxFails   = 10 // wrong passwords, or wrong codes, per email per failWindow (counted in Postgres, every instance)
)

// limits are the auth endpoints' rate limits.
type limits struct {
	ip   web.Limiter // every rate-limited auth request, per client IP
	send web.Limiter // emails sent (codes, reset links, verification codes), per address
}

func newLimits(db *pgxpool.Pool) limits {
	return limits{ip: web.Limiter{DB: db, Name: "ip", Burst: 20, Per: 3 * time.Second},
		send: web.Limiter{DB: db, Name: "send", Burst: 3, Per: 5 * time.Minute}}
}

// limitIP rate-limits fn per client IP.
func (h *Handler) limitIP(fn func(http.ResponseWriter, *http.Request) error) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		if stop, err := web.Limited(w, r, h.lim.ip, web.ClientIP(r)); err != nil || stop {
			return err
		}
		return fn(w, r)
	}
}
