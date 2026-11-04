package view

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

func TestCheckPath(t *testing.T) {
	for _, p := range []string{"/my-issues", "/my-issues?view=board", "/projects/WEB", "/projects/WEB?status=a,b&sort=due",
		"/projects/WEB/cycles/3", "/projects/WEB/cycles/3?view=list"} {
		if msg := checkPath(p); msg != "" {
			t.Errorf("%s: %s", p, msg)
		}
	}
	for _, p := range []string{"", "my-issues", "/settings", "/projects", "/projects/WEB/cycles", "/projects/WEB/cycles/x",
		"/projects/WEB/issues", "/projects/WEB#x", "https://evil.test/projects/WEB", "/projects/WEB?" + strings.Repeat("a", 2000)} {
		if msg := checkPath(p); msg != errBadPath {
			t.Errorf("%s: %q, want rejected", p, msg)
		}
	}
}

// TestViews: Ann (admin) and Mia (member) each save views; private ones stay with their owner, shared ones show to
// both, and only the owner or an admin (for shared views) can change them.
func TestViews(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")

	mux := http.NewServeMux()
	wsH := workspace.New(pool, dbtest.Auth, nil, nil)
	wsH.Register(mux)
	New(pool, dbtest.Auth, wsH.Member).Register(mux)
	base := "/api/workspaces/" + slug + "/views"
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}
	save := func(user, name string, shared bool) View {
		t.Helper()
		form := url.Values{"name": {name}, "path": {"/projects/WEB?view=board"}}
		if shared {
			form.Set("shared", "1")
		}
		var v View
		if err := json.Unmarshal([]byte(do(user, "POST", "", form, 201)), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	names := func(user string) string {
		t.Helper()
		var vs []View
		if err := json.Unmarshal([]byte(do(user, "GET", "", nil, 200)), &vs); err != nil {
			t.Fatal(err)
		}
		var ns []string
		for _, v := range vs {
			ns = append(ns, v.Name+":"+strconv.FormatBool(v.CanEdit))
		}
		return strings.Join(ns, ",")
	}

	miaPrivate := save(mia, "  mine  ", false)
	miaShared := save(mia, "Team board", true)
	annPrivate := save(ann, "Ann's", false)
	if miaPrivate.Name != "mine" || miaPrivate.Owner.ID != mia || !miaPrivate.CanEdit {
		t.Errorf("created %+v", miaPrivate)
	}
	if got := names(mia); got != "mine:true,Team board:true" {
		t.Errorf("mia sees %s", got)
	}
	if got := names(ann); got != "Ann's:true,Team board:true" {
		t.Errorf("ann sees %s", got)
	}

	// Ann (workspace.edit) may change Mia's shared view, not see her private one; Mia can't touch Ann's.
	do(ann, "POST", "/"+miaPrivate.ID, url.Values{"name": {"x"}}, 404)
	do(mia, "DELETE", "/"+annPrivate.ID, nil, 404)
	do(mia, "POST", "/not-a-uuid", url.Values{"name": {"x"}}, 404)
	if body := do(ann, "POST", "/"+miaShared.ID, url.Values{"name": {"Renamed"}}, 200); !strings.Contains(body, `"path":"/projects/WEB?view=board"`) {
		t.Errorf("rename kept the path: %s", body)
	}
	do(ann, "POST", "/"+miaShared.ID, url.Values{"shared": {"0"}}, 200)
	if got := names(ann); got != "Ann's:true" {
		t.Errorf("after unsharing ann sees %s", got)
	}
	annShared := save(ann, "Ann shared", true)
	if got := names(mia); !strings.Contains(got, "Ann shared:false") {
		t.Errorf("mia sees %s", got)
	}
	do(mia, "POST", "/"+annShared.ID, url.Values{"name": {"x"}}, 403)
	do(mia, "DELETE", "/"+annShared.ID, nil, 403)

	// A guest sees only their own views: a shared view's path can name projects they can't see.
	gus := dbtest.User(t, pool, "Gus")
	dbtest.Join(t, pool, wsID, gus, "guest")
	save(gus, "Gus's", false)
	if got := names(gus); got != "Gus's:true" {
		t.Errorf("guest sees %s", got)
	}
	do(gus, "DELETE", "/"+annShared.ID, nil, 404)

	// Validation.
	if body := do(mia, "POST", "", url.Values{"name": {" "}, "path": {"/settings"}}, 400); !strings.Contains(body, "Enter a name.") ||
		!strings.Contains(body, errBadPath) {
		t.Errorf("validation: %s", body)
	}
	if body := do(mia, "POST", "", url.Values{"name": {strings.Repeat("a", 61)}, "path": {"/my-issues"}}, 400); !strings.Contains(body, "Use 60 characters or fewer.") {
		t.Errorf("long name: %s", body)
	}

	// The cap: Mia has 2, so 48 more fit and the next fails.
	for i := range maxViews - 2 {
		save(mia, "v"+strconv.Itoa(i), false)
	}
	if body := do(mia, "POST", "", url.Values{"name": {"one more"}, "path": {"/my-issues"}}, 400); !strings.Contains(body, "You have 50 views. Delete one first.") {
		t.Errorf("cap: %s", body)
	}
	do(mia, "DELETE", "/"+miaPrivate.ID, nil, 204)
	save(mia, "one more", false)

	// Leaving the workspace takes Mia's views with her.
	code, body := dbtest.Do(t, mux, ann, "DELETE", "/api/workspaces/"+slug+"/members/"+mia, nil)
	if code != 204 {
		t.Fatalf("remove member: %d %s", code, body)
	}
	var left int
	if err := pool.QueryRow(ctx, `select count(*) from saved_views where user_id = $1`, mia).Scan(&left); err != nil || left != 0 {
		t.Errorf("mia's views after leaving: %d %v", left, err)
	}
}
