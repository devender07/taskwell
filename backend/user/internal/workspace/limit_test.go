package workspace

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/secret"
)

// TestPlanLimits: limits are the owner's plan. On Free (5) members plus pending invites to other emails fill the
// plan, so a new invite is a 402 while re-inviting a pending email isn't counted twice; joining is refused only once
// members alone fill it. Starter and Pro raise the limit and Advanced has none; a workspace without an owner is on Free.
func TestPlanLimits(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	m := &model{db: pool}
	ann, bob, cara := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cara")
	ws, err := m.create(ctx, ann, "Limits", dbtest.Name("limits-"))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		dbtest.Join(t, pool, ws.ID, dbtest.User(t, pool, "Mem"), "member")
	}
	email := func(id string) string {
		var e string
		if err := pool.QueryRow(ctx, `select email from users where id = $1`, id).Scan(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	role, err := m.role(ctx, ws.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	invite := func(email string) string {
		inv, err := m.invite(ctx, &ws, email, ann, role.ID, nil, secret.Hash(secret.NewToken()), 7)
		if err != nil {
			t.Fatal(err)
		}
		return inv.ID
	}
	bobInv := invite(email(bob)) // 4 members + 1 invite: full

	mux := http.NewServeMux()
	New(pool, dbtest.Auth, nil, nil).Register(mux)
	code, body := dbtest.Do(t, mux, ann, "POST", "/api/workspaces/"+ws.Slug+"/invites", url.Values{"email": {"new@dbtest.test"}})
	if code != 402 || !strings.Contains(body, "This workspace's plan allows 5 members. Its owner can upgrade for more.") {
		t.Fatalf("invite over the limit: %d %s", code, body)
	}
	if full, _, err := m.seatsFull(ctx, ws.ID, email(bob)); err != nil || full {
		t.Fatalf("re-inviting Bob counted his own invite: %v %v", full, err)
	}

	if code, body := dbtest.Do(t, mux, bob, "POST", "/api/me/invites/"+bobInv+"/accept", nil); code != 200 {
		t.Fatalf("Bob joins as the 5th: %d %s", code, body)
	}
	caraInv := invite(email(cara))
	code, body = dbtest.Do(t, mux, cara, "POST", "/api/me/invites/"+caraInv+"/accept", nil)
	if code != 402 || !strings.Contains(body, "This workspace is full. Ask its owner to upgrade.") {
		t.Fatalf("Cara joins a full workspace: %d %s", code, body)
	}

	for p, want := range map[string]int{"starter": 10, "pro": 25, "advanced": 0} {
		dbtest.SetPlan(t, pool, ann, p)
		if full, limit, err := m.seatsFull(ctx, ws.ID, "new@dbtest.test"); err != nil || full || limit != want {
			t.Fatalf("%s: full %v limit %d %v", p, full, limit, err)
		}
	}
	if code, body := dbtest.Do(t, mux, cara, "POST", "/api/me/invites/"+caraInv+"/accept", nil); code != 200 {
		t.Fatalf("Cara joins on Advanced: %d %s", code, body)
	}

	if _, err := pool.Exec(ctx, `update workspaces set created_by = null where id = $1`, ws.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from workspaces where id = $1`, ws.ID) })
	if full, limit, err := m.seatsFull(ctx, ws.ID, "new@dbtest.test"); err != nil || !full || limit != 5 {
		t.Fatalf("owner-less: full %v limit %d %v", full, limit, err)
	}
}

// TestWorkspaceLimit: a Free account owns 1 workspace, so creating a 2nd is a 402; Pro allows a 3rd, not a 4th,
// Advanced any, and workspaces joined don't count.
func TestWorkspaceLimit(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	m := &model{db: pool}
	bobs, err := m.create(ctx, bob, "Bob's", dbtest.Name("bobs-"))
	if err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, bobs.ID, ann, "admin")
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, nil, nil).Register(mux)
	create := func(want int, msg string) {
		t.Helper()
		code, body := dbtest.Do(t, mux, ann, "POST", "/api/workspaces", url.Values{"name": {"Ann's"}, "slug": {dbtest.Name("anns-")}})
		if code != want || !strings.Contains(body, msg) {
			t.Fatalf("create: %d %s, want %d %q", code, body, want, msg)
		}
	}
	create(201, "")
	create(402, "Your plan allows 1 workspace. Upgrade to create more.")
	dbtest.SetPlan(t, pool, ann, "pro")
	create(201, "")
	create(201, "")
	create(402, "Your plan allows 3 workspaces. Upgrade to create more.")
	dbtest.SetPlan(t, pool, ann, "advanced")
	create(201, "")
}
