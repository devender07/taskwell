package workspace

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/dbtest"
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

// TestCreateFirstJoinsInvites: a new user someone invited joins that workspace and gets none of their own; anyone
// else gets their default workspace.
func TestCreateFirstJoinsInvites(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var ann, cara, dev string
	if err := pool.QueryRow(ctx, `
		with a as (insert into users (name, email) values ('Ann', 'ann@join-invited.test') returning id),
		     c as (insert into users (name, email) values ('Cara', 'cara@join-invited.test') returning id),
		     d as (insert into users (name, email) values ('Dev', 'dev@join-invited.test') returning id)
		select a.id::text, c.id::text, d.id::text from a, c, d`).Scan(&ann, &cara, &dev); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `delete from workspaces where id in (select workspace_id from workspace_members m
			join users u on u.id = m.user_id where u.email like '%@join-invited.test')`); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(ctx, `delete from users where email like '%@join-invited.test'`); err != nil {
			t.Error(err)
		}
	})
	tx := func(f func(pgx.Tx) error) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, pool, f); err != nil {
			t.Fatal(err)
		}
	}
	var annWS string
	tx(func(tx pgx.Tx) error {
		ws, err := create(ctx, tx, ann, "Ann's team", "join-invited-ann")
		annWS = ws.ID
		return err
	})
	if _, err := pool.Exec(ctx, `insert into workspace_invites (workspace_id, email, token_hash, invited_by, expires_at)
		values ($1, 'cara@join-invited.test', 'join-invited', $2, now() + interval '1 day')`, annWS, ann); err != nil {
		t.Fatal(err)
	}
	for _, u := range []struct{ id, name string }{{cara, "Cara"}, {dev, "Dev"}} {
		tx(func(tx pgx.Tx) error { return CreateFirst(ctx, tx, u.id, u.name, "") })
	}
	workspaces := func(user string) []string {
		t.Helper()
		rows, _ := pool.Query(ctx, `select w.id::text from workspace_members m join workspaces w on w.id = m.workspace_id
			where m.user_id = $1`, user)
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if got := workspaces(cara); len(got) != 1 || got[0] != annWS {
		t.Errorf("invited Cara is in %v, want only Ann's workspace %s", got, annWS)
	}
	if invs, err := (&model{db: pool}).invites(ctx, annWS); err != nil || len(invs) != 0 {
		t.Errorf("Ann's pending invites after Cara joined: %v %v, want none", invs, err)
	}
	if got := workspaces(dev); len(got) != 1 || got[0] == annWS {
		t.Errorf("Dev is in %v, want just a default workspace of his own", got)
	}
}

func TestWorkspaceCreation(t *testing.T) {
	p := dbtest.Pool(t)
	ctx := context.Background()
	alice, bob := dbtest.User(t, p, "Alice"), dbtest.User(t, p, "Bob")
	ws, err := create(ctx, p, alice, "Alice's", dbtest.Name("a"))
	if err != nil {
		t.Fatalf("first workspace: %v", err)
	}
	dbtest.Join(t, p, ws.ID, bob, "member")
	if _, err := create(ctx, p, bob, "Bob's", dbtest.Name("b")); !errors.Is(err, errNoCreate) {
		t.Fatalf("member without the permission: got %v, want errNoCreate", err)
	}
	if _, err := p.Exec(ctx, `update workspace_roles set permissions = permissions || $2::text
		where workspace_id = $1 and builtin = 'member'`, ws.ID, permWorkspacesCreate); err != nil {
		t.Fatal(err)
	}
	if _, err := create(ctx, p, bob, "Bob's", dbtest.Name("b")); err != nil {
		t.Fatalf("member with the permission: %v", err)
	}
	if _, err := create(ctx, p, alice, "Alice's second", dbtest.Name("a")); err != nil {
		t.Fatalf("admin: %v", err)
	}
}
