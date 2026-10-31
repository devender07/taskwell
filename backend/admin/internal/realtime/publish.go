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

// envelope is the NOTIFY payload: the event plus its audience.
type envelope struct {
	Aud   string          `json:"aud"`
	Event json.RawMessage `json:"event,omitempty"`
}

// Publish sends an admin event of kind about paths to every admin tab when tx commits, stamped with the request's
// origin (see WithOrigin).
func Publish(ctx context.Context, tx pgx.Tx, kind, actorID string, paths ...string) error {
	ev, err := json.Marshal(Event{Type: "event", Kind: kind, Actor: actorID, Origin: origin(ctx), Paths: paths,
		At: time.Now().UTC().Truncate(time.Second)})
	if err != nil {
		return fmt.Errorf("publish %s: %w", kind, err)
	}
	p, err := json.Marshal(envelope{Aud: audience, Event: ev})
	if err != nil {
		return fmt.Errorf("publish %s: %w", kind, err)
	}
	if _, err := tx.Exec(ctx, `select pg_notify('realtime', $1)`, string(p)); err != nil {
		return fmt.Errorf("publish %s: %w", kind, err)
	}
	return nil
}

type originKey struct{}

var originRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// WithOrigin puts the request's X-Tether-Client header, the id of the tab's realtime connection, into its context so
// the events it publishes carry it as origin. Anything that isn't a short token is ignored.
func WithOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Tether-Client"); originRe.MatchString(id) {
			r = r.WithContext(context.WithValue(r.Context(), originKey{}, id))
		}
		next.ServeHTTP(w, r)
	})
}

func origin(ctx context.Context) string {
	id, _ := ctx.Value(originKey{}).(string)
	return id
}
