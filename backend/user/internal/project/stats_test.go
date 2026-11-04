package project

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestStats counts issues by status category per active project, overdue included.
func TestStats(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, out := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Out")
	var slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, archived_at)
			      select id, n, k, a from w, (values ('Web', 'WEB', null::timestamptz), ('Api', 'API', null), ('Old', 'OLD', now())) v(n, k, a)
			      returning id, key),
			i as (insert into issues (workspace_id, project_id, number, title, status_id, due_date, rank)
			      select (select id from w), p.id, row_number() over (partition by p.id), 'x', s.id, v.due,
			             (row_number() over (partition by p.id))::text
			      from p, (values ('Backlog', current_date - 1), ('Todo', current_date), ('In progress', null::date),
			                      ('Done', current_date - 5), ('Cancelled', current_date - 5)) v(st, due)
			      join statuses s on s.name = v.st and s.workspace_id = (select id from w)
			      where p.key <> 'API')
			select slug from w`, ann).Scan(&slug)
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	code, body := dbtest.Do(t, mux, ann, "GET", "/api/workspaces/"+slug+"/project-stats", nil)
	var got []Stats
	if code != 200 || json.Unmarshal([]byte(body), &got) != nil {
		t.Fatalf("%d %s", code, body)
	}
	want := []Stats{{Key: "API"}, {Key: "WEB", Open: 3, Done: 1, Canceled: 1, Overdue: 1}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if code, _ := dbtest.Do(t, mux, out, "GET", "/api/workspaces/"+slug+"/project-stats", nil); code != 404 {
		t.Errorf("non-member: %d", code)
	}
}
