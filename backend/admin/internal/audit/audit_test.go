package audit

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestList pages the log by number: full pages of pageSize, newest first, with the total.
func TestList(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `insert into admin_audit (actor_email, action, target, ip)
		select 'audit-test@dbtest.test', 'config.update', 'page-test', '127.0.0.1' from generate_series(1, $1)`, pageSize+1); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := pool.QueryRow(ctx, `select count(*) from admin_audit`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	h := New(pool, nil)
	page := func(q string) (out struct {
		Entries []Entry `json:"entries"`
		Total   int     `json:"total"`
	}) {
		w := httptest.NewRecorder()
		if err := h.list(w, httptest.NewRequest("GET", "/api/audit"+q, nil), ""); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first, second := page(""), page("?page=2")
	if len(first.Entries) != pageSize || first.Total != total || first.Entries[0].Target != "page-test" {
		t.Fatalf("page 1: %d entries, total %d of %d", len(first.Entries), first.Total, total)
	}
	if len(second.Entries) == 0 || second.Entries[0].ID >= first.Entries[pageSize-1].ID {
		t.Fatalf("page 2 doesn't continue page 1: %+v", second.Entries[:1])
	}
	if bad := page("?page=nope"); len(bad.Entries) != pageSize || bad.Entries[0].ID != first.Entries[0].ID {
		t.Fatalf("an invalid page isn't page 1")
	}
}
