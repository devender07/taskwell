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

// TestTrash: deleting trashes an issue (hidden everywhere, number and rank kept), the creator or issues.edit_any
// restores it, only issues.edit_any deletes it for good, bulk delete trashes, and a status with trashed issues can be
// deleted. Ann is the admin; Mia and Otto are members.
func TestTrash(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Web', 'WEB' from w returning id)
			select w.id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, otto, "member")
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}
	for _, title := range []string{"One", "Two", "Three", "Four"} {
		do(mia, "POST", "/projects/WEB/issues", url.Values{"title": {title}}, 201)
	}

	do(otto, "DELETE", "/issues/WEB-1", nil, 403)
	do(mia, "DELETE", "/issues/WEB-1", nil, 204)
	do(mia, "GET", "/issues/WEB-1", nil, 404)
	for _, path := range []string{"/projects/WEB/issues", "/issues?q=One", "/search?q=One", "/my-issues"} {
		if body := do(mia, "GET", path, nil, 200); strings.Contains(body, `"WEB-1"`) {
			t.Errorf("%s shows the trashed issue: %s", path, body)
		}
	}
	var page struct {
		Issues []struct {
			Identifier string
			DeletedBy  *Person `json:"deleted_by"`
			CanRestore bool    `json:"can_restore"`
		}
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(do(otto, "GET", "/trash", nil, 200)), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Issues) != 1 || page.Issues[0].Identifier != "WEB-1" || page.Issues[0].DeletedBy.ID != mia || page.Issues[0].CanRestore {
		t.Fatalf("trash as Otto: %+v", page)
	}

	// A new issue goes last, past the trashed one's kept rank; WEB-1 comes back with its number.
	do(mia, "POST", "/projects/WEB/issues", url.Values{"title": {"Five"}, "before": {"WEB-2"}}, 201)
	do(otto, "POST", "/trash/WEB-1/restore", nil, 403)
	if body := do(mia, "POST", "/trash/WEB-1/restore", nil, 200); !strings.Contains(body, `"identifier":"WEB-1"`) {
		t.Fatalf("restore: %s", body)
	}
	do(mia, "POST", "/trash/WEB-1/restore", nil, 404)
	if body := do(mia, "GET", "/issues/WEB-1/timeline", nil, 200); !strings.Contains(body, `"restored"`) {
		t.Errorf("no restored event: %s", body)
	}

	// Bulk delete trashes; deleting for good needs issues.edit_any.
	do(mia, "POST", "/issues/bulk", url.Values{"ids": {"WEB-2", "WEB-3"}, "delete": {"1"}}, 200)
	if err := json.Unmarshal([]byte(do(mia, "GET", "/trash", nil, 200)), &page); err != nil || len(page.Issues) != 2 {
		t.Fatalf("after bulk delete: %+v %v", page, err)
	}
	do(mia, "DELETE", "/trash/WEB-2", nil, 403)
	do(ann, "DELETE", "/trash/WEB-2", nil, 204)
	do(ann, "POST", "/trash/WEB-2/restore", nil, 404)

	// A status only trashed issues use can go; they move to another.
	var status string
	if err := pool.QueryRow(ctx, `select status_id::text from issues_all i join projects p on p.id = i.project_id
		where p.workspace_id = $1 and i.number = 3`, wsID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update issues set status_id = (select id from statuses where workspace_id = $1 and id <> $2 limit 1)
		where workspace_id = $1 and status_id = $2`, wsID, status); err != nil {
		t.Fatal(err)
	}
	do(ann, "DELETE", "/statuses/"+status, nil, 204)

	// Empty the trash.
	do(mia, "DELETE", "/trash", nil, 403)
	do(ann, "DELETE", "/trash", nil, 204)
	if err := json.Unmarshal([]byte(do(ann, "GET", "/trash", nil, 200)), &page); err != nil || len(page.Issues) != 0 {
		t.Fatalf("after emptying: %+v %v", page, err)
	}
}
