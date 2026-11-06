package issue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestTeamRules: a QA role with issues.status_any changes any issue's status (and only that), and a member who hands
// their issue to someone else can still edit it.
func TestTeamRules(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, dev, other, qa := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Dev"), dbtest.User(t, pool, "Other"), dbtest.User(t, pool, "Quinn")
	var wsID, slug, done string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Web', 'WEB' from w returning id),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id, rank, created_by)
			      select (select id from w), p.id, 1, 'Ann''s issue', (select id from s), 'V', $1 from p)
			select id::text, slug, (select id::text from statuses where workspace_id = w.id and category = 'completed' limit 1) from w`,
			ann).Scan(&wsID, &slug, &done)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, dev, "member")
	dbtest.Join(t, pool, wsID, other, "member")
	dbtest.Join(t, pool, wsID, qa, "member")
	if _, err := pool.Exec(ctx, `with r as (insert into workspace_roles (workspace_id, name, permissions)
		values ($1, 'QA', '{issues.create,comments.create,issues.status_any}') returning id)
		update workspace_members set role_id = (select id from r) where workspace_id = $1 and user_id = $2`, wsID, qa); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	api := "/api/workspaces/" + slug + "/issues/"
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, path, form)
		if code != want {
			t.Fatalf("%s %s %s: %d %s, want %d", user, method, path, code, body, want)
		}
		return body
	}

	// #WEB / #WEB- find the project's issues by identifier prefix.
	for _, q := range []string{"WEB", "web-", "WEB-1"} {
		if body := do(other, "GET", "/api/workspaces/"+slug+"/issues?q="+q, nil, 200); !strings.Contains(body, `"WEB-1"`) {
			t.Errorf("search %q: %s", q, body)
		}
	}

	// QA: status yes (also with a board placement), anything else no; can_change_status says so.
	var got struct {
		CanEdit         bool `json:"can_edit"`
		CanChangeStatus bool `json:"can_change_status"`
	}
	if err := json.Unmarshal([]byte(do(qa, "GET", api+"WEB-1", nil, 200)), &got); err != nil || got.CanEdit || !got.CanChangeStatus {
		t.Fatalf("qa sees %+v, %v", got, err)
	}
	do(qa, "POST", api+"WEB-1", url.Values{"status_id": {done}}, 200)
	do(qa, "POST", api+"WEB-1", url.Values{"status_id": {done}, "title": {"Sneaky"}}, 403)
	do(qa, "POST", api+"WEB-1", url.Values{"title": {"Sneaky"}}, 403)
	do(other, "POST", api+"WEB-1", url.Values{"status_id": {done}}, 403)

	// Dev hands an issue Ann assigned him to Other and keeps editing it; before that it wasn't his to edit.
	do(dev, "POST", api+"WEB-1", url.Values{"title": {"x"}}, 403) // not Dev's, not assigned by Dev
	do(ann, "POST", api+"WEB-1", url.Values{"assignee_id": {dev}}, 200)
	do(dev, "POST", api+"WEB-1", url.Values{"assignee_id": {other}}, 200)
	do(dev, "POST", api+"WEB-1", url.Values{"assignee_id": {dev}}, 200)
}
