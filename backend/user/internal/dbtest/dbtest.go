// Package dbtest gives integration tests a migrated scratch database, TEST_DATABASE_URL (for example
// postgres://taskwell:taskwell@localhost:5433/taskwell_test?sslmode=disable). Tests that use it are skipped without one.
// Only tests import it.
package dbtest

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/db"
)

var (
	once    sync.Once
	pool    *pgxpool.Pool
	openErr error
)

// Pool returns the package's pool on TEST_DATABASE_URL, migrated on first use, or skips the test.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	once.Do(func() {
		ctx := context.Background()
		if pool, openErr = db.Open(ctx, url); openErr == nil {
			openErr = db.Migrate(ctx, pool)
		}
	})
	if openErr != nil {
		t.Fatal(openErr)
	}
	return pool
}

// Name returns prefix plus a random lowercase suffix, for unique slugs, emails and keys.
func Name(prefix string) string { return prefix + strings.ToLower(rand.Text()[:8]) }

// User adds a verified user and returns its id. When the test ends it deletes the workspaces the user created
// (with everything in them), then the user.
func User(t testing.TB, p *pgxpool.Pool, name string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := p.QueryRow(ctx, `insert into users (name, email, email_verified_at) values ($1, $2, now()) returning id::text`,
		name, Name(strings.ToLower(name)+"-")+"@dbtest.test").Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, `delete from workspaces where created_by = $1`, id); err != nil {
			t.Error(err)
		}
		if _, err := p.Exec(ctx, `delete from users where id = $1`, id); err != nil {
			t.Error(err)
		}
	})
	return id
}

// Join makes the user a member of the workspace with its builtin role ("admin" or "member").
func Join(t testing.TB, p *pgxpool.Pool, workspaceID, userID, builtin string) {
	t.Helper()
	if _, err := p.Exec(context.Background(),
		`insert into workspace_members (workspace_id, user_id, role_id)
		 select $1, $2, id from workspace_roles where workspace_id = $1 and builtin = $3`, workspaceID, userID, builtin); err != nil {
		t.Fatal(err)
	}
}

// Auth is an authenticate func for handlers under test: the user is the X-Test-User header.
func Auth(r *http.Request) (string, error) { return r.Header.Get("X-Test-User"), nil }

// Do sends a request as the user (form as the urlencoded body when not nil) and returns the status and body.
func Do(t testing.TB, h http.Handler, userID, method, path string, form url.Values) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Header.Set("X-Test-User", userID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}
