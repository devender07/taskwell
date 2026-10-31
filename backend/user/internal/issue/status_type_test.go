package issue

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestStatusType saves, changes and clears a status's free-text type, and rejects a long one.
func TestStatusType(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	var slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateDefault(ctx, tx, ann, "Ann"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select slug from workspaces where created_by = $1`, ann).Scan(&slug)
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug + "/statuses"

	code, body := dbtest.Do(t, mux, ann, "POST", base, url.Values{"name": {"Reopen"}, "category": {"started"}, "type": {" Reopened "}})
	if code != 201 || !strings.Contains(body, `"type":"Reopened"`) {
		t.Fatalf("create: %d %s", code, body)
	}
	id := body[strings.Index(body, `"id":"`)+6:][:36]
	code, body = dbtest.Do(t, mux, ann, "POST", base+"/"+id, url.Values{"name": {"Reopen"}, "category": {"started"}, "type": {""}})
	if code != 200 || strings.Contains(body, `"type"`) {
		t.Fatalf("clear: %d %s", code, body)
	}
	code, body = dbtest.Do(t, mux, ann, "POST", base+"/"+id, url.Values{"name": {"Reopen"}, "category": {"started"}, "type": {strings.Repeat("x", 31)}})
	if code != 400 || !strings.Contains(body, "Type must be 30 characters or fewer.") {
		t.Fatalf("long: %d %s", code, body)
	}
	if code, body = dbtest.Do(t, mux, ann, "GET", base, nil); code != 200 || strings.Contains(body, `"type"`) {
		t.Fatalf("list: %d %s", code, body)
	}
}
