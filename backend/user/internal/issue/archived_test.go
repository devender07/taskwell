package issue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestArchivedReadOnly: an archived project's issues can be read but not edited, moved, linked, bulk-changed or
// deleted, even by an admin, and they say so (can_edit false).
func TestArchivedReadOnly(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, archived_at)
			      select id, n, k, a from w, (values ('Live', 'LIV', null::timestamptz), ('Old', 'OLD', now())) v(n, k, a)
			      returning id, key),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id, rank)
			      select (select id from w), p.id, n, p.key || ' issue', (select id from s), 'V' || n from p, generate_series(1, 2) n)
			select slug from w`, ann).Scan(&slug)
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	api := "/api/workspaces/" + slug + "/issues/"
	do := func(method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, ann, method, path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}

	var got struct {
		CanEdit   bool `json:"can_edit"`
		CanDelete bool `json:"can_delete"`
	}
	if err := json.Unmarshal([]byte(do("GET", api+"OLD-1", nil, 200)), &got); err != nil || got.CanEdit || got.CanDelete {
		t.Fatalf("archived issue = %+v, %v", got, err)
	}
	do("POST", api+"OLD-1", url.Values{"title": {"Renamed"}}, 403)
	do("POST", api+"OLD-1/move", url.Values{"before": {"OLD-2"}}, 403)
	do("POST", api+"OLD-1/links", url.Values{"type": {"blocks"}, "issue": {"LIV-1"}}, 403)
	do("POST", api+"LIV-1/links", url.Values{"type": {"blocks"}, "issue": {"OLD-1"}}, 400)
	do("POST", api+"bulk", url.Values{"ids": {"OLD-1", "LIV-1"}, "priority": {"1"}}, 403)
	do("DELETE", api+"OLD-1", nil, 403)
	do("POST", api+"LIV-1", url.Values{"title": {"Still editable"}}, 200)
	do("POST", api+"LIV-1", url.Values{"title": {"nul \x00 byte"}}, 400) // Postgres can't store NUL: a 400, not a 500
}
