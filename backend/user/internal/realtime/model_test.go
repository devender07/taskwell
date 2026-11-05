package realtime

import (
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
)

func TestClaim(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := t.Context()
	m := &model{db: pool}
	user := dbtest.User(t, pool, "Cap")
	claim := func(conn, inst string) bool {
		t.Helper()
		ok, err := m.claim(ctx, conn, user, inst, 2)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !claim("cap-a", "i1") || !claim("cap-b", "i2") {
		t.Fatal("refused a connection under the cap")
	}
	if claim("cap-c", "i1") {
		t.Fatal("third connection across instances got in")
	}
	if err := m.release(ctx, "cap-a"); err != nil {
		t.Fatal(err)
	}
	if !claim("cap-c", "i1") {
		t.Fatal("released place wasn't freed")
	}
	if _, err := pool.Exec(ctx, `update realtime_conns set seen_at = now() - interval '2 minutes' where instance = 'i2'`); err != nil {
		t.Fatal(err)
	}
	if !claim("cap-d", "i1") {
		t.Fatal("a stale row still counted")
	}
	if err := m.forget(ctx, "i1"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from realtime_conns where user_id = $1 and instance = 'i1'`, user).Scan(&n); err != nil || n != 0 {
		t.Fatalf("forget left %d rows (err %v)", n, err)
	}
}
