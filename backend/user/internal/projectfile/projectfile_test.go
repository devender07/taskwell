package projectfile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/file"
	"github.com/devender07/taskwell/backend/user/internal/projectfile"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestFiles runs against the dev S3 store. Ann is the admin (projects.manage), Mia and Bob members, Gus a guest in WEB,
// Otto outside the workspace. WEB is public, SEC private with nobody but Ann.
func TestFiles(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Bob")
	gus, otto := dbtest.User(t, pool, "Gus"), dbtest.User(t, pool, "Otto")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Web', 'WEB', false), ('Sec', 'SEC', true)) v(n, k, pr))
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, bob, "member")
	dbtest.Join(t, pool, wsID, gus, "guest")
	if _, err := pool.Exec(ctx, `insert into project_members (project_id, user_id)
		select id, $2 from projects where workspace_id = $1 and key = 'WEB'`, wsID, gus); err != nil {
		t.Fatal(err)
	}

	cfg := sysconfig.New(pool)
	store := storage.New(cfg)
	ws := workspace.New(pool, dbtest.Auth, nil, nil)
	fileH := file.New(pool, store, dbtest.Auth, ws.Member, cfg, nil)
	mux := http.NewServeMux()
	fileH.Register(mux)
	projectfile.New(pool, store, fileH.Upload, dbtest.Auth, ws.Member).Register(mux)
	api := "/api/workspaces/" + slug
	web := api + "/projects/WEB"

	do := func(user, method, path string, form url.Values, want int) map[string]any {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(body), &out)
		return out
	}
	upload := func(user, path, name string, fields map[string]string, want int) map[string]any {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", name)
		if strings.HasSuffix(name, ".txt") {
			fw.Write([]byte("hello " + name))
		} else {
			fw.Write([]byte("\x89PNG\r\n\x1a\n" + name)) // sniffed as image/png
		}
		for k, v := range fields { // after the file: field order doesn't matter
			mw.WriteField(k, v)
		}
		mw.Close()
		r := httptest.NewRequest(http.MethodPost, path, &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.Header.Set("X-Test-User", user)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("upload %s to %s: %d %s, want %d", name, path, w.Code, w.Body, want)
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	names := func(list map[string]any, key string) []string {
		var ns []string
		for _, v := range list[key].([]any) {
			ns = append(ns, v.(map[string]any)["name"].(string))
		}
		return ns
	}
	id := func(m map[string]any) string { return m["id"].(string) }

	// Visibility: a guest outside SEC and anyone outside the workspace get 404.
	do(gus, "GET", web+"/files", nil, 200)
	do(gus, "GET", api+"/projects/SEC/files", nil, 404)
	do(mia, "GET", api+"/projects/SEC/files", nil, 404)
	do(otto, "GET", web+"/files", nil, 404)
	secret := upload(ann, api+"/projects/SEC/files", "secret.png", nil, 201)
	for user, want := range map[string]int{ann: 302, mia: 404, gus: 404, otto: 404} {
		do(user, "GET", secret["url"].(string), nil, want)
	}

	// Only images and videos.
	upload(mia, web+"/files", "notes.txt", nil, 400)
	upload(mia, api+"/files", "notes.txt", nil, 400)

	// Folders: unique names in a parent, no loops, only empty ones deleted.
	design := do(mia, "POST", web+"/folders", url.Values{"name": {"Design"}}, 201)
	do(bob, "POST", web+"/folders", url.Values{"name": {"design "}}, 400)
	do(bob, "POST", web+"/folders", url.Values{"name": {""}}, 400)
	mock := do(mia, "POST", web+"/folders", url.Values{"name": {"Mockups"}, "parent_id": {id(design)}}, 201)
	do(mia, "POST", web+"/folders", url.Values{"name": {"Design"}, "parent_id": {id(mock)}}, 201) // another parent
	do(mia, "POST", web+"/folders/"+id(design), url.Values{"parent_id": {id(mock)}}, 400)
	do(mia, "POST", web+"/folders/"+id(design), url.Values{"parent_id": {id(design)}}, 400)
	do(mia, "DELETE", web+"/folders/"+id(design), nil, 409)
	do(bob, "POST", web+"/folders/"+id(design), url.Values{"name": {"Mine"}}, 403)
	do(mia, "POST", web+"/folders", url.Values{"name": {"X"}, "parent_id": {id(secret)}}, 404)

	// Files: the uploader and projects.manage change them, others can't.
	spec := upload(mia, web+"/files", "spec.png", map[string]string{"folder_id": id(design)}, 201)
	if spec["can_edit"] != true || spec["uploader"].(map[string]any)["name"] != "Mia" {
		t.Fatalf("upload = %v", spec)
	}
	upload(mia, web+"/files", "x.png", map[string]string{"folder_id": id(secret)}, 404)
	do(bob, "POST", web+"/files/"+id(spec), url.Values{"name": {"bob.png"}}, 403)
	do(bob, "DELETE", web+"/files/"+id(spec), nil, 403)
	do(mia, "POST", web+"/files/"+id(spec), url.Values{"name": {" "}}, 400)
	got := do(mia, "POST", web+"/files/"+id(spec), url.Values{"name": {"mockup spec.png"}, "folder_id": {id(mock)}}, 200)
	if got["name"] != "mockup spec.png" {
		t.Fatalf("rename = %v", got)
	}
	list := do(gus, "GET", web+"/files?folder="+id(mock), nil, 200)
	if path := list["path"].([]any); len(path) != 2 || path[0].(map[string]any)["name"] != "Design" ||
		len(names(list, "files")) != 1 || list["files"].([]any)[0].(map[string]any)["can_edit"] != false {
		t.Fatalf("folder listing = %v", list)
	}
	do(ann, "POST", web+"/files/"+id(spec), url.Values{"folder_id": {""}}, 200)
	root := do(mia, "GET", web+"/files", nil, 200)
	if f := names(root, "files"); len(f) != 1 || f[0] != "mockup spec.png" || len(names(root, "folders")) != 1 {
		t.Fatalf("root = %v", root)
	}
	do(mia, "GET", web+"/files?folder="+id(secret), nil, 404)

	// Search spans the project; % is literal.
	found := do(gus, "GET", web+"/files?q=MOCK", nil, 200)
	if len(names(found, "folders")) != 1 || len(names(found, "files")) != 1 {
		t.Fatalf("search = %v", found)
	}
	if found := do(gus, "GET", web+"/files?q=%25", nil, 200); len(names(found, "files")) != 0 {
		t.Fatalf("search %% = %v", found)
	}

	// Attachments: linked with project, listed apart, deleted but not renamed or moved; a project the user can't see is 404.
	att := upload(mia, api+"/files", "shot.png", map[string]string{"project": "web"}, 201)
	upload(mia, api+"/files", "shot.png", map[string]string{"project": "SEC"}, 404)
	atts := do(mia, "GET", web+"/files?folder=attachments", nil, 200)
	if a := names(atts, "files"); len(a) != 1 || a[0] != "shot.png" || atts["files"].([]any)[0].(map[string]any)["can_edit"] != true {
		t.Fatalf("attachments = %v", atts)
	}
	do(gus, "GET", att["url"].(string), nil, 302)

	// An issue's files: listed by the issue's id, and among the attachments; an issue elsewhere is 404.
	var issueID string
	if err := pool.QueryRow(ctx, `insert into issues (workspace_id, project_id, number, title, status_id, rank)
		select $1, p.id, 1, 'Sign in', (select id from statuses where workspace_id = $1 order by position limit 1), 'V'
		from projects p where p.workspace_id = $1 and p.key = 'WEB' returning id::text`, wsID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	upload(mia, api+"/files", "on-issue.png", map[string]string{"project": "WEB", "issue": issueID}, 201)
	upload(mia, api+"/files", "on-issue.png", map[string]string{"project": "WEB", "issue": id(att)}, 404)
	upload(ann, api+"/files", "on-issue.png", map[string]string{"project": "SEC", "issue": issueID}, 404)
	if _, err := pool.Exec(ctx, `update workspace_roles set permissions = array_remove(permissions, 'comments.create')
		where workspace_id = $1 and builtin = 'guest'`, wsID); err != nil {
		t.Fatal(err)
	}
	upload(gus, api+"/files", "on-issue.png", map[string]string{"project": "WEB", "issue": issueID}, 403)
	if _, err := pool.Exec(ctx, `update projects set archived_at = now() where workspace_id = $1 and key = 'WEB'`, wsID); err != nil {
		t.Fatal(err)
	}
	upload(mia, api+"/files", "on-issue.png", map[string]string{"project": "WEB", "issue": issueID}, 404)
	if _, err := pool.Exec(ctx, `update projects set archived_at = null where workspace_id = $1 and key = 'WEB'`, wsID); err != nil {
		t.Fatal(err)
	}
	if f := names(do(gus, "GET", web+"/files?issue="+issueID, nil, 200), "files"); len(f) != 1 || f[0] != "on-issue.png" {
		t.Fatalf("issue files = %v", f)
	}
	do(gus, "GET", web+"/files?issue=nope", nil, 404)
	if a := names(do(mia, "GET", web+"/files?folder=attachments", nil, 200), "files"); len(a) != 2 {
		t.Fatalf("attachments with the issue's file = %v", a)
	}
	do(ann, "POST", web+"/files/"+id(att), url.Values{"name": {"y.png"}}, 404)
	do(bob, "DELETE", web+"/files/"+id(att), nil, 403)
	do(ann, "DELETE", web+"/files/"+id(att), nil, 204)
	do(gus, "GET", att["url"].(string), nil, 404)

	// Storage and quota.
	st := do(bob, "GET", api+"/storage", nil, 200)
	if st["used_bytes"].(float64) <= 0 || st["quota_bytes"].(float64) <= 0 {
		t.Fatalf("storage = %v", st)
	}
	if _, err := pool.Exec(ctx, `insert into files (workspace_id, name, content_type, size) values ($1, 'big', 'x', $2)`,
		wsID, int64(st["quota_bytes"].(float64))); err != nil {
		t.Fatal(err)
	}
	upload(mia, web+"/files", "over.png", nil, 413)
	if _, err := pool.Exec(ctx, `delete from files where workspace_id = $1 and name = 'big'`, wsID); err != nil {
		t.Fatal(err)
	}

	// Deleting: a file, then its emptied folders; the project takes the rest.
	do(mia, "DELETE", web+"/files/"+id(spec), nil, 204)
	do(mia, "GET", spec["url"].(string), nil, 404)
	do(mia, "DELETE", web+"/folders/"+id(design), nil, 409)
	do(ann, "DELETE", web+"/folders/"+id(mock), nil, 409) // holds the other "Design"
	keep := upload(mia, web+"/files", "keep.png", map[string]string{"folder_id": id(mock)}, 201)
	if _, err := pool.Exec(ctx, `delete from projects where workspace_id = $1 and key = 'WEB'`, wsID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `select (select count(*) from files where id in ($1, $2)) + (select count(*) from file_folders where id = $3)`,
		id(att), id(keep), id(design)).Scan(&left); err != nil || left != 0 {
		t.Fatalf("after deleting the project: %d rows left, %v", left, err)
	}
}
