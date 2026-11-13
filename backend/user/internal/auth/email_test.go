package auth

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/secret"
)

// TestEmailChange: the email moves only with both codes, a wrong one is named and counted, the codes work once, and an
// address taken meanwhile is refused.
func TestEmailChange(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	m := &model{db: pool}
	to := dbtest.Name("new-") + "@dbtest.test"
	oldCode, newCode := secret.Hash("111111"), secret.Hash("222222")
	if err := m.startEmailChange(ctx, ann, to, oldCode, newCode, 10); err != nil {
		t.Fatal(err)
	}

	if _, _, bad, err := m.changeEmail(ctx, ann, secret.Hash("000000"), newCode, nil, meta{}); err != nil || !slices.Equal(bad, []string{"old_code"}) {
		t.Fatalf("wrong old code: bad %v err %v", bad, err)
	}
	if _, _, bad, err := m.changeEmail(ctx, bob, oldCode, newCode, nil, meta{}); err != nil || len(bad) != 2 {
		t.Fatalf("someone else's codes: bad %v err %v", bad, err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `select attempts from auth_tokens where user_id = $1 and purpose = $2`, ann, purposeEmailOld).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("attempts %d %v", attempts, err)
	}

	from, got, bad, err := m.changeEmail(ctx, ann, oldCode, newCode, nil, meta{})
	if err != nil || len(bad) != 0 || got != to || from == to {
		t.Fatalf("change: %q -> %q bad %v err %v", from, got, bad, err)
	}
	var email string
	if err := pool.QueryRow(ctx, `select email from users where id = $1`, ann).Scan(&email); err != nil || email != to {
		t.Fatalf("email now %q %v", email, err)
	}
	if _, _, bad, err := m.changeEmail(ctx, ann, oldCode, newCode, nil, meta{}); err != nil || len(bad) != 2 {
		t.Fatalf("codes reused: bad %v err %v", bad, err)
	}

	// Bob asks for Ann's new address; she has it by the time he confirms.
	if err := m.startEmailChange(ctx, bob, to, oldCode, newCode, 10); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := m.changeEmail(ctx, bob, oldCode, newCode, nil, meta{}); !errors.Is(err, errEmailTaken) {
		t.Fatalf("taken address: %v", err)
	}
}
