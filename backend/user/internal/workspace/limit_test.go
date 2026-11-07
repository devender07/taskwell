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

// TestPlanLimits: on Starter (5) members plus pending invites to other emails fill the plan, so a new invite is a
// 402 while re-inviting a pending email isn't counted twice; joining is refused only once members alone fill it.
// Pro raises the limit and Ultimate has none.
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
	if code != 402 || !strings.Contains(body, "Your plan allows 5 members. Upgrade to invite more.") {
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
	if code != 402 || !strings.Contains(body, "This workspace is full. Ask an admin to upgrade its plan.") {
		t.Fatalf("Cara joins a full workspace: %d %s", code, body)
	}

	for p, want := range map[string]int{"pro": 10, "ultimate": 0} {
		if _, err := pool.Exec(ctx, `update workspaces set plan = $2 where id = $1`, ws.ID, p); err != nil {
			t.Fatal(err)
		}
		if full, limit, err := m.seatsFull(ctx, ws.ID, "new@dbtest.test"); err != nil || full || limit != want {
			t.Fatalf("%s: full %v limit %d %v", p, full, limit, err)
		}
	}
	if code, body := dbtest.Do(t, mux, cara, "POST", "/api/me/invites/"+caraInv+"/accept", nil); code != 200 {
		t.Fatalf("Cara joins on Ultimate: %d %s", code, body)
	}
}
