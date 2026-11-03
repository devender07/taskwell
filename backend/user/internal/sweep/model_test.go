package sweep

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/db"
)

// testTx migrates TEST_DATABASE_URL (e.g. postgres://tether:tether@localhost:5433/tether_test?sslmode=disable) and
// returns a transaction that's rolled back when the test ends; the test is skipped without one.
func testTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

func TestDeleteUnreferencedFiles(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}
	user := one(`insert into users (name, email) values ('Sweep', 'sweep-' || gen_random_uuid() || '@test') returning id::text`)
	ws := one(`insert into workspaces (name, slug, created_by) values ('W', 'sweep-' || gen_random_uuid(), $1) returning id::text`, user)
	project := one(`insert into projects (workspace_id, name, key) values ($1, 'P', 'SWP') returning id::text`, ws)
	status := one(`insert into statuses (workspace_id, name, category, position) values ($1, 'Sweep test', 'unstarted', 99) returning id::text`, ws)
	file := func(age time.Duration, avatar bool) string {
		var wsID *string
		if !avatar {
			wsID = &ws
		}
		return one(`insert into files (workspace_id, uploader_id, name, content_type, size, created_at)
			values ($1, $2, 'f.png', 'image/png', 1, now() - make_interval(secs => $3)) returning id::text`, wsID, user, age.Seconds())
	}
	old := 48 * time.Hour
	var (
		inImage   = file(old, false)
		inLink    = file(old, false)
		inComment = file(old, false)
		inLive    = file(old, false)
		avatar    = file(old, true)
		orphan    = file(old, false)
		fresh     = file(time.Hour, false)
		oldAvatar = file(old, true) // a replaced avatar nobody points at
	)
	desc := `{"type":"doc","content":[{"type":"image","attrs":{"src":"/api/files/` + inImage + `/f.png"}},
		{"type":"paragraph","content":[{"type":"text","text":"see","marks":[{"type":"link","attrs":{"href":"/api/files/` + inLink + `/f.png"}}]}]}]}`
	issue := one(`insert into issues (workspace_id, project_id, number, title, status_id, created_by, description, rank)
		values ($1, $2, 1, 'I', $3, $4, $5, 'V') returning id::text`, ws, project, status, user, desc)
	one(`insert into comments (issue_id, author_id, body) values ($1, $2, $3) returning id::text`, issue, user,
		`{"type":"doc","content":[{"type":"image","attrs":{"src":"/api/files/`+inComment+`/f.png"}}]}`)
	one(`insert into issue_doc_updates (issue_id, data) values ($1, '\x0102'::bytea || convert_to('/api/files/' || $2 || '/f.png', 'UTF8')) returning id::text`, issue, inLive)
	one(`update users set avatar_file_id = $2 where id = $1 returning id::text`, user, avatar)

	ids, err := model{tx}.deleteUnreferencedFiles(ctx, fileGrace)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{orphan, oldAvatar} {
		if !slices.Contains(ids, id) {
			t.Errorf("unreferenced file %s kept", id)
		}
	}
	kept := map[string]string{inImage: "image src", inLink: "link href", inComment: "comment", inLive: "live update",
		avatar: "avatar", fresh: "new upload"}
	for id, why := range kept {
		if slices.Contains(ids, id) {
			t.Errorf("deleted the file referenced by its %s", why)
		}
	}
	gone, err := model{tx}.missingFiles(ctx, []string{orphan, inImage})
	if err != nil || !slices.Equal(gone, []string{orphan}) {
		t.Fatalf("missingFiles = %v, %v", gone, err)
	}
}

func TestRetention(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	var user string
	if err := tx.QueryRow(ctx, `insert into users (name, email) values ('R', 'ret-' || gen_random_uuid() || '@test') returning id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	_, err := tx.Exec(ctx, `insert into refresh_tokens (token_hash, user_id, family_id, expires_at, used_at) values
		('\x01'::bytea || uuid_send(gen_random_uuid()), $1, uuidv7(), now() - interval '1 hour', null),
		('\x02'::bytea || uuid_send(gen_random_uuid()), $1, uuidv7(), now() + interval '1 day', now() - interval '20 days'),
		('\x03'::bytea || uuid_send(gen_random_uuid()), $1, uuidv7(), now() + interval '1 day', now() - interval '1 day'),
		('\x04'::bytea || uuid_send(gen_random_uuid()), $1, uuidv7(), now() + interval '1 day', null)`, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range retention {
		if _, err := (model{tx}).deleteAll(ctx, r.table, r.where); err != nil {
			t.Fatalf("%s: %v", r.table, err)
		}
	}
	var left int
	if err := tx.QueryRow(ctx, `select count(*) from refresh_tokens where user_id = $1`, user).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 2 {
		t.Fatalf("%d refresh tokens left, want the live and the recently used one", left)
	}
}
