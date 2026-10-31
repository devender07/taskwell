package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestVerifySignupIsAtomic checks that a sign-up is verified together with its default workspace, or not at all.
func TestVerifySignupIsAtomic(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	email := dbtest.Name("signup-") + "@dbtest.test"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from workspaces where created_by = (select id from users where email = $1)`, email)
		_, _ = pool.Exec(ctx, `delete from users where email = $1`, email)
	})
	m := &model{db: pool, onCreated: func(context.Context, pgx.Tx, string, string) error { return errors.New("boom") }}
	code := hash("123456.nonce")
	if err := m.startSignup(ctx, "Ada Lovelace", email, "x", code, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := m.verifySignup(ctx, email, code); err == nil {
		t.Fatal("verified although the workspace failed")
	}
	var verified bool
	if err := pool.QueryRow(ctx, `select email_verified_at is not null from users where email = $1`, email).Scan(&verified); err != nil || verified {
		t.Fatalf("after a failed workspace: verified=%v err=%v, want unverified", verified, err)
	}
	m.onCreated = workspace.CreateDefault
	u, err := m.verifySignup(ctx, email, code) // the code wasn't used up by the rolled-back try
	if err != nil || u == nil {
		t.Fatalf("verify: %v %v", u, err)
	}
	var name string
	if err := pool.QueryRow(ctx, `select w.name from workspaces w join workspace_members m on m.workspace_id = w.id where m.user_id = $1`,
		u.ID).Scan(&name); err != nil || name != "Ada's workspace" {
		t.Fatalf("default workspace: %q %v", name, err)
	}
}
