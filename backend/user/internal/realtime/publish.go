package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Publish sends e to its audience on every API instance when tx commits; nothing is sent if it rolls back.
// It stamps the event with the request's origin (see WithOrigin) and the time.
// An event about a private project (e.Project), or any project in a workspace with guests, goes to each person who
// can see it rather than the whole workspace.
func Publish(ctx context.Context, tx pgx.Tx, e Event) error {
	if e.Project != "" && strings.HasPrefix(e.Audience, "ws:") {
		var private bool
		var viewers []string
		err := tx.QueryRow(ctx,
			`with p as (select p.id, p.workspace_id, p.private or exists (select 1 from workspace_roles r
			              join workspace_members m on m.role_id = r.id where r.workspace_id = p.workspace_id and r.builtin = 'guest') private
			            from projects p where p.id = $1)
			 select p.private, array(select m.user_id::text from workspace_members m
			   where m.workspace_id = p.workspace_id and p.private and can_see_project(p.id, m.user_id))
			 from p`, e.Project).Scan(&private, &viewers)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("publish %s: %w", e.Kind, err)
		}
		if private {
			// ponytail: one NOTIFY per viewer; add a project audience to the hub if private projects (or workspaces with
			// guests) get big.
			for _, u := range viewers {
				pe := e
				pe.Project, pe.Audience = "", ToUser(u)
				if err := Publish(ctx, tx, pe); err != nil {
					return err
				}
			}
			return nil
		}
	}
	e.Type, e.Origin, e.At = "event", origin(ctx), time.Now().UTC().Truncate(time.Second)
	p, err := payload(e)
	if err != nil {
		return fmt.Errorf("publish %s: %w", e.Kind, err)
	}
	if _, err := tx.Exec(ctx, `select pg_notify('realtime', $1)`, string(p)); err != nil {
		return fmt.Errorf("publish %s: %w", e.Kind, err)
	}
	return nil
}

// PublishProfile tells the user's own tabs their profile changed, and every workspace they're in that their name or
// avatar did (it shows on members, issues and comments).
func PublishProfile(ctx context.Context, tx pgx.Tx, userID string) error {
	if err := Publish(ctx, tx, Event{Kind: "profile.changed", Actor: userID, Audience: ToUser(userID), Paths: []string{"/me"}}); err != nil {
		return err
	}
	ws, err := memberships(ctx, tx, userID)
	if err != nil {
		return err
	}
	for _, w := range ws {
		if err := Publish(ctx, tx, Event{Kind: "members.changed", Workspace: w.slug, Actor: userID, Audience: ToWorkspace(w.id),
			Paths: []string{Workspace(w.slug) + "/members", All(w.slug)}}); err != nil {
			return err
		}
	}
	return nil
}

type originKey struct{}

// originRe is a client id as the socket hands them out (rand.Text), with room to spare.
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
