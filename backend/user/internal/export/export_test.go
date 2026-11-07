package export

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestExport exports a small seeded workspace (see dbtest; files go to the configured S3 store) as Cat, a custom
// role with workspace.export who isn't in the private LEAD project: the zip's entries, workspace.json, the CSV
// headers, LEAD and the trashed issue left out, a missing object listed in the README; then Bob without the
// permission, the rate limit, and forged and expired tokens.
func TestExport(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cat := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cat")
	var wsID, slug, webID, fileID, goneID string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Web', 'WEB', false), ('Leadership', 'LEAD', true)) v(n, k, pr) returning id, key),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id, rank, priority, description)
			      select (select id from w), p.id, n, p.key || ' issue ' || n, (select id from s), 'V' || n, 2,
			        '{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello"}]}]}'
			      from p, generate_series(1, 2) n returning id, project_id, number),
			l as (insert into labels (workspace_id, name, color) select id, 'Bug', '#ff0000' from w returning id),
			il as (insert into issue_labels select i.id, l.id from i, l where i.number = 1),
			c as (insert into comments (issue_id, author_id, body)
			      select i.id, $1, '{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Looks good"}]}]}'
			      from i where i.number = 1),
			f as (insert into files (workspace_id, uploader_id, name, content_type, size, project_id, listed)
			      select (select id from w), $1, n, 'text/plain', 5, p.id, true
			      from p, (values ('notes.txt'), ('gone.txt')) v(n) returning id, name, project_id)
			select (select id::text from w), (select slug from w), (select id::text from p where key = 'WEB'),
			  (select f.id::text from f join p on p.id = f.project_id where key = 'WEB' and name = 'notes.txt'),
			  (select f.id::text from f join p on p.id = f.project_id where key = 'WEB' and name = 'gone.txt')`, ann).
			Scan(&wsID, &slug, &webID, &fileID, &goneID)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update issues_all set deleted_at = now() where project_id = $1 and number = 2`, webID); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	if _, err := pool.Exec(ctx, `with r as (insert into workspace_roles (workspace_id, name, permissions)
		values ($1, 'Exporter', '{workspace.export}') returning id)
		insert into workspace_members (workspace_id, user_id, role_id) select $1, $2, id from r`, wsID, cat); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `delete from rate_limits where key like 'export:%'`) })

	store := storage.New(sysconfig.New(pool))
	if err := store.Put(ctx, fileID, "text/plain", "attachment", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), fileID) })

	h := New(pool, store, "test secret", dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member)
	mux := http.NewServeMux()
	h.Register(mux)
	start := func(user string, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, "POST", "/api/workspaces/"+slug+"/export", nil)
		if code != want {
			t.Fatalf("start as %s: %d %s, want %d", user, code, body, want)
		}
		var out struct{ URL string }
		_ = json.Unmarshal([]byte(body), &out)
		return out.URL
	}

	start(bob, http.StatusForbidden)
	// download returns the zip's entries by name.
	download := func(url string) map[string]string {
		t.Helper()
		code, body := dbtest.Do(t, mux, "", "GET", url, nil)
		if code != http.StatusOK {
			t.Fatalf("download: %d %s", code, body)
		}
		z, err := zip.NewReader(strings.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		entries := map[string]string{}
		for _, f := range z.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			entries[f.Name] = string(b)
		}
		return entries
	}
	url := start(cat, http.StatusOK)
	entries := download(url)
	notes := "files/" + fileID + "-notes.txt"
	for _, name := range []string{"README.txt", "workspace.json", "issues.csv", "comments.csv", notes} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("zip has no %s: %v", name, slices.Collect(maps.Keys(entries)))
		}
	}
	if entries[notes] != "hello" || len(entries) != 5 {
		t.Fatalf("files: %d entries, notes %q", len(entries), entries[notes])
	}
	if !strings.Contains(entries["README.txt"], "WEB: gone.txt") {
		t.Fatalf("README doesn't list the missing file:\n%s", entries["README.txt"])
	}

	var e Export
	if err := json.Unmarshal([]byte(entries["workspace.json"]), &e); err != nil {
		t.Fatal(err)
	}
	if len(e.Projects) != 1 || e.Projects[0].Key != "WEB" {
		t.Fatalf("projects: %+v, want only WEB", e.Projects)
	}
	if len(e.Issues) != 1 || e.Issues[0].Identifier != "WEB-1" {
		t.Fatalf("issues: %+v, want only WEB-1", e.Issues)
	}
	i := e.Issues[0]
	if i.Priority != "high" || i.DescriptionText != "Hello" || !slices.Equal(i.Labels, []string{"Bug"}) ||
		len(i.Comments) != 1 || i.Comments[0].BodyText != "Looks good" || i.Comments[0].Author == nil {
		t.Fatalf("issue: %+v", i)
	}
	if len(e.Files) != 2 || e.Files[0].Path == nil || *e.Files[0].Path != notes || e.Files[1].Path != nil {
		t.Fatalf("files: %+v", e.Files)
	}
	if len(e.Members) != 3 || len(e.Statuses) == 0 || e.ExportedBy.Name != "Cat" {
		t.Fatalf("members %d, statuses %d, by %+v", len(e.Members), len(e.Statuses), e.ExportedBy)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal([]byte(entries["workspace.json"]), &raw)
	for _, k := range []string{"exported_at", "exported_by", "workspace", "members", "roles", "statuses", "labels",
		"projects", "issues", "feedback", "files"} {
		if raw[k] == nil {
			t.Errorf("workspace.json has no %s", k)
		}
	}

	header := func(name string) []string {
		rows, err := csv.NewReader(strings.NewReader(entries[name])).ReadAll()
		if err != nil || len(rows) != 2 {
			t.Fatalf("%s: %v %v", name, rows, err)
		}
		return rows[0]
	}
	if got := header("issues.csv"); !slices.Equal(got, []string{"Identifier", "Project", "Title", "Description", "Status",
		"Priority", "Estimate", "Assignee", "Labels", "Due date", "Cycle", "Created"}) {
		t.Errorf("issues.csv header %v", got)
	}
	if got := header("comments.csv"); !slices.Equal(got, []string{"Issue", "Author", "Created", "Body"}) {
		t.Errorf("comments.csv header %v", got)
	}

	// Ann (admin) sees the private project.
	if !strings.Contains(download(start(ann, http.StatusOK))["workspace.json"], `"LEAD"`) {
		t.Fatal("admin export: LEAD missing")
	}
	// Three starts an hour per workspace: Cat's and Ann's above, one more, then 429.
	start(cat, http.StatusOK)
	start(cat, http.StatusTooManyRequests)

	expired := func(path string) {
		t.Helper()
		if code, body := dbtest.Do(t, mux, "", "GET", path, nil); code != http.StatusNotFound || !strings.Contains(body, "expired") {
			t.Fatalf("GET %s: %d %s, want 404", path, code, body)
		}
	}
	expired(url[:len(url)-2] + "xx")
	expired("/api/exports/" + sign([]byte("another key"), wsID, cat, time.Now().Add(time.Minute)))
	expired("/api/exports/" + sign(h.key, wsID, bob, time.Now().Add(time.Minute))) // signed, but Bob can't export
	h.now = func() time.Time { return time.Now().Add(tokenTTL + time.Second) }
	expired(url)
}
