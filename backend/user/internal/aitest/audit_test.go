package aitest

import (
	"context"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
)

func TestSitesAndAccountsAudited(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, _ := testIssue(t, pool)
	m := &model{db: pool}
	key := make([]byte, 32)
	site, err := m.createSite(ctx, wsID, "x", ann, "https://app.acme.test", "app.acme.test")
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.saveAccount(ctx, key, wsID, "x", ann, "", "Admin", "ann@acme.test", "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.saveAccount(ctx, key, wsID, "x", ann, a.ID, "Admin", "ann@acme.test", "new", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.deleteAccount(ctx, wsID, "x", ann, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.deleteSite(ctx, wsID, "x", ann, site.ID); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := pool.QueryRow(ctx, `select string_agg(action || ' ' || target || ' ' || detail::text, ', ' order by id)
		from workspace_audit where workspace_id = $1 and actor_id = $2 and action like 'test\_%'`, wsID, ann).Scan(&got); err != nil {
		t.Fatal(err)
	}
	want := `test_site.added app.acme.test {}, test_account.added Admin {}, test_account.updated Admin {"password_changed": true}, ` +
		`test_account.removed Admin {}, test_site.removed app.acme.test {}`
	if got != want {
		t.Errorf("audit\n got %s\nwant %s", got, want)
	}
}
