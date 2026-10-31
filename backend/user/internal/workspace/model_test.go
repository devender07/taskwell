package workspace

import (
	"context"
	"os"
	"testing"

	"github.com/devender07/tether/backend/user/internal/db"
)

// TestTakeInviteSend runs against a scratch database, e.g.
// TEST_DATABASE_URL=postgres://tether:tether@localhost:5433/tether_test?sslmode=disable.
func TestTakeInviteSend(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close) // after the row cleanup below, which runs first
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var ann, bob, ws1, ws2 string
	if err := pool.QueryRow(ctx, `
		with a as (insert into users (name, email) values ('Ann', 'ann@invite-limit.test') returning id),
		     b as (insert into users (name, email) values ('Bob', 'bob@invite-limit.test') returning id),
		     w1 as (insert into workspaces (name, slug) values ('One', 'invite-limit-1') returning id),
		     w2 as (insert into workspaces (name, slug) values ('Two', 'invite-limit-2') returning id)
		select a.id::text, b.id::text, w1.id::text, w2.id::text from a, b, w1, w2`).Scan(&ann, &bob, &ws1, &ws2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `delete from workspaces where slug like 'invite-limit-%'`); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `delete from users where email like '%@invite-limit.test'`); err != nil {
			t.Error(err)
		}
	})
	m := &model{db: pool}
	take := func(ws, user string) bool {
		t.Helper()
		ok, err := m.takeInviteSend(ctx, ws, user, 2, 3)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	steps := []struct {
		name     string
		ws, user string
		want     bool
	}{
		{"ann's first", ws1, ann, true},
		{"ann's second", ws1, ann, true},
		{"ann over her daily cap", ws1, ann, false},
		{"ann's cap spans workspaces", ws2, ann, false},
		{"bob in the same workspace", ws1, bob, true},
		{"workspace over its daily cap", ws1, bob, false},
		{"bob elsewhere", ws2, bob, true},
	}
	for _, s := range steps {
		if got := take(s.ws, s.user); got != s.want {
			t.Errorf("%s: got %v, want %v", s.name, got, s.want)
		}
	}
	// A day later the sends no longer count.
	if _, err := pool.Exec(ctx, `update invite_sends set at = now() - interval '25 hours' where workspace_id = any($1::uuid[])`,
		[]string{ws1, ws2}); err != nil {
		t.Fatal(err)
	}
	if !take(ws1, ann) {
		t.Error("yesterday's sends still count")
	}
}
