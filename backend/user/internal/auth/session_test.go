package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/secret"
)

// TestAuthenticateBearer checks that a personal API token signs its user in, and a revoked token, a suspended
// user's token and a non-tth bearer don't.
func TestAuthenticateBearer(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	h := &Handler{model: &model{db: pool}}
	userID := dbtest.User(t, pool, "Bearer")
	auth := func(header string) string {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/workspaces", nil)
		r.Header.Set("Authorization", header)
		id, err := h.Authenticate(r)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	raw := apiTokenPrefix + secret.NewToken()
	tok, err := h.model.insertAPIToken(ctx, userID, "laptop", secret.Hash(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := auth("Bearer " + raw); got != userID {
		t.Fatalf("valid token: got %q, want %q", got, userID)
	}
	var used bool
	if err := pool.QueryRow(ctx, `select last_used_at is not null from api_tokens where id = $1`, tok.ID).Scan(&used); err != nil || !used {
		t.Errorf("last_used_at not set: %v %v", used, err)
	}
	if got := auth("Bearer " + secret.NewToken()); got != "" {
		t.Errorf("non-tth bearer signed in as %q", got)
	}

	if _, err := pool.Exec(ctx, `update users set suspended_at = now() where id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if got := auth("Bearer " + raw); got != "" {
		t.Errorf("suspended user's token signed in as %q", got)
	}
	if _, err := pool.Exec(ctx, `update users set suspended_at = null where id = $1`, userID); err != nil {
		t.Fatal(err)
	}

	if ok, err := h.model.deleteAPIToken(ctx, userID, tok.ID); err != nil || !ok {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	if got := auth("Bearer " + raw); got != "" {
		t.Errorf("revoked token signed in as %q", got)
	}
}
