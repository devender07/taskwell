package cycle

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/issue"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestPrivate: a private project's cycles are not found for someone outside it, even with cycles.manage; its member
// and the admin see them. Ann is the admin, Mia in the project, Otto outside it.
func TestPrivate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateDefault(ctx, tx, ann, "Ann"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private) select id, 'Leadership', 'LEAD', true from w returning id)
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, otto, "member")
	if _, err := pool.Exec(ctx, `update workspace_roles set permissions = permissions || '{cycles.manage}'
		where workspace_id = $1 and builtin = 'member'`, wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_members (project_id, user_id)
		select id, $2 from projects where workspace_id = $1 and key = 'LEAD'`, wsID, mia); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	wsH := workspace.New(pool, dbtest.Auth, nil, nil)
	New(pool, dbtest.Auth, wsH.Member, issue.New(pool, dbtest.Auth, wsH.Member).CycleIssues).Register(mux)
	base := "/api/workspaces/" + slug + "/projects/LEAD/cycles"
	dates := url.Values{"starts_on": {"2030-01-01"}, "ends_on": {"2030-01-14"}}
	for _, r := range []struct {
		user, method, path string
		want               int
	}{
		{otto, "POST", "", 404}, {mia, "POST", "", 201},
		{otto, "GET", "", 404}, {otto, "GET", "/1", 404}, {otto, "POST", "/1", 404}, {otto, "POST", "/1/rollover", 404},
		{otto, "DELETE", "/1", 404},
		{mia, "GET", "", 200}, {mia, "GET", "/1", 200}, {ann, "GET", "/1", 200}, {ann, "DELETE", "/1", 204},
	} {
		form := dates
		if r.path == "/1/rollover" {
			form = url.Values{"to": {"2"}}
		}
		if code, body := dbtest.Do(t, mux, r.user, r.method, base+r.path, form); code != r.want {
			t.Errorf("%s %s as %s: %d %s, want %d", r.method, r.path, r.user, code, body, r.want)
		}
	}
}
