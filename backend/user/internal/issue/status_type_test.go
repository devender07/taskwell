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

// TestStatusTypes runs status types through the API: validation, duplicates, statuses taking a type's category and
// icon (issues too), a category change reaching its statuses, and delete turning them built-in.
func TestStatusTypes(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Engineering', 'ENG' from w)
			select slug from w`, ann).Scan(&slug)
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(step string, want int, method, path string, form url.Values, has ...string) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, ann, method, base+path, form)
		if code != want {
			t.Fatalf("%s: %d %s", step, code, body)
		}
		for _, s := range has {
			if !strings.Contains(body, s) {
				t.Fatalf("%s: %s lacks %s", step, body, s)
			}
		}
		return body
	}

	do("invalid", 400, "POST", "/status-types", url.Values{"color": {"red"}}, `"name":"Type name is required."`,
		`"category":"Choose how it counts."`, `"icon":"Choose an icon."`, `"color":"Choose a color."`)
	do("long", 400, "POST", "/status-types", url.Values{"name": {strings.Repeat("x", 31)}, "category": {"started"}, "icon": {"eye"}},
		"Type name must be 30 characters or fewer.")
	var typ StatusType
	if err := json.Unmarshal([]byte(do("create", 201, "POST", "/status-types",
		url.Values{"name": {" In review "}, "category": {"started"}, "icon": {"eye"}, "color": {"#F59E0B"}})), &typ); err != nil {
		t.Fatal(err)
	}
	if typ.Name != "In review" || typ.Category != "started" || typ.Icon != "eye" || typ.Color == nil || *typ.Color != "#f59e0b" {
		t.Fatalf("created %+v", typ)
	}
	do("duplicate", 409, "POST", "/status-types", url.Values{"name": {"in review"}, "category": {"started"}, "icon": {"eye"}},
		`"name":"That type already exists."`)

	do("unknown type", 400, "POST", "/statuses", url.Values{"name": {"Code review"}, "type_id": {"0190a3c0-0000-7000-8000-000000000000"}},
		`"type_id":"Choose a type."`)
	do("bad type", 400, "POST", "/statuses", url.Values{"name": {"Code review"}, "type_id": {"x"}}, `"type_id":"Choose a type."`)
	body := do("status", 201, "POST", "/statuses", url.Values{"name": {"Code review"}, "category": {"backlog"}, "type_id": {typ.ID}},
		`"category":"started"`, `"icon":"eye"`, `"color":"#f59e0b"`, `"type":{"id":"`+typ.ID+`","name":"In review"}`)
	statusID := body[strings.Index(body, `"id":"`)+6:][:36]
	do("list", 200, "GET", "/status-types", nil, `"status_count":1`)

	do("issue", 201, "POST", "/projects/ENG/issues", url.Values{"title": {"Review me"}, "status_id": {statusID}})
	do("issue status", 200, "GET", "/issues/ENG-1", nil, `"icon":"eye","color":"#f59e0b"`)

	do("recount", 200, "POST", "/status-types/"+typ.ID, url.Values{"name": {"In review"}, "category": {"completed"}, "icon": {"eye"}},
		`"category":"completed"`, `"color":null`)
	do("statuses follow", 200, "GET", "/statuses", nil, `"name":"Code review","category":"completed"`)
	do("missing", 404, "POST", "/status-types/0190a3c0-0000-7000-8000-000000000000",
		url.Values{"name": {"X"}, "category": {"started"}, "icon": {"eye"}}, `"error":"Type not found."`)

	do("delete", 204, "DELETE", "/status-types/"+typ.ID, nil)
	do("built-in", 200, "GET", "/statuses", nil, `"name":"Code review","category":"completed","position":`, `"icon":null,"color":null,"type":null`)
	do("deleted", 404, "DELETE", "/status-types/"+typ.ID, nil)
	do("built-in issue", 200, "GET", "/issues/ENG-1", nil, `"icon":null,"color":null`)
}
