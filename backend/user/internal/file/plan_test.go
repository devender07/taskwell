package file

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestPlanStorage: an upload's room is what's left of the plan's storage (Starter 2 GB, with its message), Ultimate
// has no quota (room is the per-file limit, quota_bytes null).
func TestPlanStorage(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into files (workspace_id, name, content_type, size) values ($1, 'big', 'x', $2)`,
		wsID, int64(2<<30)-1<<20); err != nil {
		t.Fatal(err)
	}
	cfg := sysconfig.New(pool)
	h := New(pool, nil, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, cfg).Member, cfg, nil)
	mux := http.NewServeMux()
	h.Register(mux)
	mb, err := cfg.GetInt(ctx, "upload_max_mb")
	if err != nil {
		t.Fatal(err)
	}

	limit, msg, err := h.room(ctx, wsID)
	if err != nil || limit != 1<<20 || msg != "This workspace is out of storage (2 GB). Delete some files or upgrade the plan." {
		t.Fatalf("starter room %d %q %v", limit, msg, err)
	}
	if code, body := dbtest.Do(t, mux, ann, "GET", "/api/workspaces/"+slug+"/storage", nil); code != 200 || !strings.Contains(body, `"quota_bytes":2147483648`) {
		t.Fatalf("starter storage %d %s", code, body)
	}

	if _, err := pool.Exec(ctx, `update workspaces set plan = 'ultimate' where id = $1`, wsID); err != nil {
		t.Fatal(err)
	}
	if limit, _, err := h.room(ctx, wsID); err != nil || limit != int64(mb)<<20 {
		t.Fatalf("ultimate room %d %v, want %d MB", limit, err, mb)
	}
	if code, body := dbtest.Do(t, mux, ann, "GET", "/api/workspaces/"+slug+"/storage", nil); code != 200 || !strings.Contains(body, `"quota_bytes":null`) {
		t.Fatalf("ultimate storage %d %s", code, body)
	}
}
