package wsaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
)

// TestRecordRollsBack: an entry recorded in a transaction that rolls back is gone with it.
func TestRecordRollsBack(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var wsID string
	if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by) values ('Audit', $1, $2) returning id::text`,
		dbtest.Name("audit-"), ann).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := Record(ctx, tx, wsID, ann, "role.deleted", "Triage", nil); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if es, _, err := List(ctx, pool, wsID, "", nil); err != nil || len(es) != 0 {
		t.Fatalf("after rollback: %+v %v", es, err)
	}
	if err := Record(ctx, pool, wsID, ann, "role.deleted", "Triage", nil); err != nil {
		t.Fatal(err)
	}
	if es, _, err := List(ctx, pool, wsID, "", nil); err != nil || len(es) != 1 || es[0].Actor.Name != "Ann" {
		t.Fatalf("after commit: %+v %v", es, err)
	}
}
