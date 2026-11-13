package issue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

func TestLabelSet(t *testing.T) {
	if got := labelSet([]string{"b", "a"}, []string{"c", "a", ""}, []string{"b", "x"}); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("got %v", got)
	}
}

// TestBulk: Ann is the admin, Mia a member of the private LEAD project, Otto a member outside it. Mia created WEB-1,
// WEB-2 and LEAD-1; Ann created WEB-3.
func TestBulk(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto")
	var wsID, slug, bug, ui string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Leadership', 'LEAD', true), ('Web', 'WEB', false)) v(n, k, pr) returning id),
			l as (insert into labels (workspace_id, name, color) select id, n, '#ff0000' from w, (values ('Bug'), ('UI')) v(n) returning id, name)
			select w.id::text, slug, (select id::text from l where name = 'Bug'), (select id::text from l where name = 'UI') from w`,
			ann).Scan(&wsID, &slug, &bug, &ui)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, otto, "member")
	if _, err := pool.Exec(ctx, `insert into project_members (project_id, user_id)
		select id, $2 from projects where workspace_id = $1 and key = 'LEAD'`, wsID, mia); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
		}
		return body
	}
	do(mia, "POST", "/projects/WEB/issues", url.Values{"title": {"One"}}, 201)
	do(mia, "POST", "/projects/WEB/issues", url.Values{"title": {"Two"}}, 201)
	do(ann, "POST", "/projects/WEB/issues", url.Values{"title": {"Three"}}, 201)
	do(mia, "POST", "/projects/LEAD/issues", url.Values{"title": {"Secret"}}, 201)
	var statuses []Status
	if err := json.Unmarshal([]byte(do(ann, "GET", "/statuses", nil, 200)), &statuses); err != nil {
		t.Fatal(err)
	}
	done := statuses[len(statuses)-1].ID
	get := func(identifier string) Issue {
		t.Helper()
		var i Issue
		if err := json.Unmarshal([]byte(do(ann, "GET", "/issues/"+identifier, nil, 200)), &i); err != nil {
			t.Fatal(err)
		}
		return i
	}
	bulk := func(user string, want int, ids []string, kv ...string) string {
		t.Helper()
		form := url.Values{"ids": ids}
		for i := 0; i < len(kv); i += 2 {
			form.Add(kv[i], kv[i+1])
		}
		return do(user, "POST", "/issues/bulk", form, want)
	}
	expect := func(body, want string) {
		t.Helper()
		if !strings.Contains(body, want) {
			t.Errorf("got %s, want %s", body, want)
		}
	}

	// All or nothing: Mia can't edit Ann's WEB-3, so WEB-1 stays as it was.
	expect(bulk(mia, 403, []string{"WEB-1", "web-3"}, "status_id", done), `"You can't edit WEB-3."`)
	if get("WEB-1").Status.ID == done {
		t.Error("WEB-1 changed by a refused request")
	}

	// Labels are added and removed, keeping the others; each issue logs its own event.
	expect(bulk(ann, 200, []string{"WEB-1", "WEB-2", "WEB-1"}, "add_label_ids", bug, "add_label_ids", ui, "priority", "2"), `{"updated":2}`)
	expect(bulk(ann, 200, []string{"WEB-1"}, "remove_label_ids", ui), `{"updated":1}`)
	if w1, w2 := get("WEB-1"), get("WEB-2"); len(w1.Labels) != 1 || w1.Labels[0].ID != bug || len(w2.Labels) != 2 || w2.Priority != 2 {
		t.Errorf("labels: WEB-1 %v, WEB-2 %v %d", w1.Labels, w2.Labels, w2.Priority)
	}
	var labelEvents int
	if err := pool.QueryRow(ctx, `select count(*) from issue_events e join issues i on i.id = e.issue_id
		where i.workspace_id = $1 and e.kind = 'labels'`, wsID).Scan(&labelEvents); err != nil || labelEvents != 3 {
		t.Errorf("label events %d %v, want 3", labelEvents, err)
	}

	// Otto can't be assigned LEAD-1, whose project he can't see; nothing changes.
	expect(bulk(ann, 400, []string{"WEB-1", "LEAD-1"}, "assignee_id", otto), `"assignee_id":"They can't see this project."`)
	if get("WEB-1").Assignee != nil {
		t.Error("WEB-1 assigned by a refused request")
	}
	expect(bulk(ann, 200, []string{"WEB-1", "LEAD-1"}, "assignee_id", mia), `{"updated":2}`)

	// Errors.
	expect(bulk(ann, 400, nil, "priority", "1"), "Pick some issues.")
	expect(bulk(ann, 400, []string{"WEB-1"}), "Choose something to change.")
	expect(bulk(ann, 400, []string{"WEB-1"}, "delete", "1", "priority", "1"), "Delete can't be combined with other changes.")
	expect(bulk(otto, 400, []string{"WEB-1", "LEAD-1"}, "priority", "1"), "Some issues weren't found.")
	expect(bulk(ann, 400, []string{"WEB-1", "WEB-99"}, "priority", "1"), "Some issues weren't found.")
	expect(bulk(ann, 400, []string{"WEB-1"}, "priority", "9"), `{"errors":{"priority":"Choose a priority."}}`)
	expect(bulk(ann, 400, []string{"WEB-1"}, "add_label_ids", "x", "remove_label_ids", "0192f0c4-7b1a-7c3e-8d2f-1a2b3c4d5e6f"),
		`{"errors":{"add_label_ids":"Choose labels from this workspace.","remove_label_ids":"Choose labels from this workspace."}}`)
	expect(bulk(ann, 400, []string{"WEB-1"}, "cycle", "0192f0c4-7b1a-7c3e-8d2f-1a2b3c4d5e6f"), `"cycle":"Cycle not found."`)

	// Delete: Mia may delete her own issues only.
	expect(bulk(mia, 403, []string{"WEB-1", "WEB-3"}, "delete", "1"), `"You can't delete WEB-3."`)
	expect(bulk(mia, 200, []string{"WEB-1", "WEB-2"}, "delete", "1"), `{"deleted":2}`)
	do(ann, "GET", "/issues/WEB-1", nil, 404)
	do(ann, "GET", "/issues/WEB-3", nil, 200)
}
