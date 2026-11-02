package realtime

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
)

// TestPublishPrivate: an event about a private project goes to each person who can see it (its member and the
// admin), never the workspace; one about a public project goes to the workspace.
func TestPublishPrivate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := t.Context()
	ann, mia, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto")
	var wsID, private, public string
	if err := pool.QueryRow(ctx, `
		with w as (insert into workspaces (name, slug, created_by) values ('Rt', $4, $1) returning id),
		     r as (insert into workspace_roles (workspace_id, name, builtin) select id, n, b from w,
		           (values ('Admin', 'admin'), ('Member', 'member')) v(n, b) returning id, builtin),
		     m as (insert into workspace_members (workspace_id, user_id, role_id)
		           select (select id from w), u, (select id from r where builtin = b)
		           from (values ($1::uuid, 'admin'), ($2::uuid, 'member'), ($3::uuid, 'member')) v(u, b)),
		     p as (insert into projects (workspace_id, name, key, private)
		           select id, k, k, k = 'SEC' from w, (values ('SEC'), ('PUB')) v(k) returning id, key),
		     pm as (insert into project_members (project_id, user_id) select id, $2 from p where key = 'SEC')
		select (select id::text from w), (select id::text from p where key = 'SEC'), (select id::text from p where key = 'PUB')`,
		ann, mia, otto, dbtest.Name("rt-")).Scan(&wsID, &private, &public); err != nil {
		t.Fatal(err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `listen realtime`); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(context.Background(), `unlisten realtime`) //nolint:errcheck // the conn goes back to the pool
	for _, p := range []string{private, public} {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return Publish(ctx, tx, Event{Kind: "test.private", Workspace: "rt", Audience: ToWorkspace(wsID), Project: p,
				Paths: []string{"/x/" + p}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string][]string{} // path: audiences
	for n := 0; n < 3; {
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		note, err := conn.Conn().WaitForNotification(wctx)
		cancel()
		if err != nil {
			t.Fatalf("waiting for events, got %v: %v", got, err)
		}
		var env struct {
			Aud   string
			Event Event
		}
		if json.Unmarshal([]byte(note.Payload), &env) != nil || env.Event.Kind != "test.private" {
			continue // another test's
		}
		p := env.Event.Paths[0]
		got[p] = append(got[p], env.Aud)
		n++
	}
	slices.Sort(got["/x/"+private])
	want := []string{ToUser(ann), ToUser(mia)}
	slices.Sort(want)
	if !slices.Equal(got["/x/"+private], want) || !slices.Equal(got["/x/"+public], []string{ToWorkspace(wsID)}) {
		t.Errorf("audiences %v, want private to %v", got, want)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from workspaces where id = $1`, wsID) })
}
