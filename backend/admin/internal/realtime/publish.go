package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// audience is the admin app's; the user service publishes to it too.
const audience = "admin"

// Event tells admin tabs which admin API paths (without /api) changed, so they refetch them. The user service's
// realtime.Event has the same JSON.
type Event struct {
	Type      string    `json:"type"`
	Kind      string    `json:"kind"`
	Workspace string    `json:"workspace"`
	Actor     string    `json:"actor"`
	Origin    string    `json:"origin"`
	Paths     []string  `json:"paths"`
	At        time.Time `json:"at"`
}

// envelope is the NOTIFY payload: the event plus its audience, as the user service's realtime reads it.
type envelope struct {
	Aud   string          `json:"aud"`
	Close bool            `json:"close,omitempty"`
	Event json.RawMessage `json:"event,omitempty"`
}

// PublishRevoked tells the user service to close the user's sockets when tx commits (their tabs can't reconnect once
// the user is suspended): the user service's realtime.SessionRevoked.
func PublishRevoked(ctx context.Context, tx pgx.Tx, actorID, userID string) error {
	return notify(ctx, tx, envelope{Aud: "user:" + userID, Close: true}, Event{Kind: "session.revoked", Actor: actorID,
		Paths: []string{"/me"}})
}

// Publish sends an admin event of kind about paths to every admin tab when tx commits, stamped with the request's
// origin (see WithOrigin).
func Publish(ctx context.Context, tx pgx.Tx, kind, actorID string, paths ...string) error {
	return notify(ctx, tx, envelope{Aud: audience}, Event{Kind: kind, Actor: actorID, Paths: paths})
}

// notify sends e in env on the realtime channel when tx commits.
func notify(ctx context.Context, tx pgx.Tx, env envelope, e Event) error {
	e.Type, e.Origin, e.At = "event", origin(ctx), time.Now().UTC().Truncate(time.Second)
	ev, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("publish %s: %w", e.Kind, err)
	}
	env.Event = ev
	p, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("publish %s: %w", e.Kind, err)
	}
	if _, err := tx.Exec(ctx, `select pg_notify('realtime', $1)`, string(p)); err != nil {
		return fmt.Errorf("publish %s: %w", e.Kind, err)
	}
	return nil
}

type originKey struct{}

var originRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// WithOrigin puts the request's X-Taskwell-Client header, the id of the tab's realtime connection, into its context so
// the events it publishes carry it as origin. Anything that isn't a short token is ignored.
func WithOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Taskwell-Client"); originRe.MatchString(id) {
			r = r.WithContext(context.WithValue(r.Context(), originKey{}, id))
		}
		next.ServeHTTP(w, r)
	})
}

func origin(ctx context.Context) string {
	id, _ := ctx.Value(originKey{}).(string)
	return id
}
