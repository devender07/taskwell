package plan

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
)

type txRows struct{ pgx.Tx }

// TestLoad: system_config's plan_* rows replace the defaults, a broken one keeps its default, and the migration's
// seed equals the defaults. Runs in a transaction that's rolled back, so other tests keep the seeded limits.
func TestLoad(t *testing.T) {
	var pool *pgxpool.Pool = dbtest.Pool(t)
	ctx := context.Background()
	t.Cleanup(func() { loaded.Store(nil); beta.Store(false) })
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var seeded int
	if err := tx.QueryRow(ctx, `select count(*) from system_config where key like 'plan\_%'`).Scan(&seeded); err != nil || seeded != 20 {
		t.Fatalf("%d plan_ keys seeded, want 20: %v", seeded, err)
	}
	if err := Load(ctx, txRows{tx}); err != nil {
		t.Fatal(err)
	}
	for i, p := range All() {
		if p != Plans[i] {
			t.Errorf("seeded %s = %+v, default %+v", p.ID, p, Plans[i])
		}
	}
	if _, err := tx.Exec(ctx, `update system_config set value = case key when 'plan_pro_members' then '40' when 'plan_pro_storage_gb' then '0'
		when 'plan_free_ai_usd' then '0.5' when 'plan_starter_projects' then 'many' end
		where key in ('plan_pro_members', 'plan_pro_storage_gb', 'plan_free_ai_usd', 'plan_starter_projects')`); err != nil {
		t.Fatal(err)
	}
	if err := Load(ctx, txRows{tx}); err != nil {
		t.Fatal(err)
	}
	pro, _ := Get("pro")
	free, _ := Get(Free)
	starter, _ := Get("starter")
	if pro.Members != 40 || pro.StorageBytes != 0 || pro.Workspaces != 3 || free.AIUSD != 0.5 || starter.Projects != 5 {
		t.Errorf("after edits: pro %+v, free %+v, starter %+v", pro, free, starter)
	}
	if Beta() {
		t.Error("beta on with beta_mode seeded false")
	}
	if tag, err := tx.Exec(ctx, `update system_config set value = 'true' where key = 'beta_mode'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("set beta_mode: %v %v", tag, err)
	}
	if err := Load(ctx, txRows{tx}); err != nil || !Beta() || Msg("paid", "beta") != "beta" {
		t.Errorf("beta_mode true: Beta() %v, err %v", Beta(), err)
	}
}
