package project

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestPrivate: a private project is seen only by its members and admins, everywhere projects are listed, and its
// members can be managed. Ann is the admin, Mia a member of the project, Otto a workspace member outside it.
func TestPrivate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateDefault(ctx, tx, ann, "Ann"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, mia, "member")
	dbtest.Join(t, pool, wsID, otto, "member")
	// Mia may manage projects, so she can manage the members of the projects she sees.
	if _, err := pool.Exec(ctx, `update workspace_roles set permissions = permissions || '{projects.manage}'
		where workspace_id = $1 and builtin = 'member'`, wsID); err != nil {
		t.Fatal(err)
	}

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
	project := func(body string) Project {
		t.Helper()
		var p Project
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	names := func(body string) string {
		t.Helper()
		var ps []struct{ Key, Name string }
		if err := json.Unmarshal([]byte(body), &ps); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range ps {
			out = append(out, p.Key+p.Name)
		}
		return strings.Join(out, " ")
	}

	do(ann, "POST", "/projects", url.Values{"name": {"Leadership"}, "key": {"LEAD"}, "private": {"true"},
		"member_ids": {otto + "x"}}, 400)
	p := project(do(ann, "POST", "/projects", url.Values{"name": {"Leadership"}, "key": {"LEAD"}, "private": {"true"},
		"member_ids": {mia}}, 201))
	if !p.Private || len(p.Members) != 2 {
		t.Fatalf("created %+v", p)
	}
	do(ann, "POST", "/projects", url.Values{"name": {"Web"}, "key": {"WEB"}}, 201)

	for _, u := range []string{ann, mia} {
		if got := names(do(u, "GET", "/projects", nil, 200)); got != "LEADLeadership WEBWeb" {
			t.Errorf("%s sees %q", u, got)
		}
		if got := names(do(u, "GET", "/project-stats", nil, 200)); got != "LEAD WEB" {
			t.Errorf("%s stats %q", u, got)
		}
		do(u, "GET", "/projects/LEAD", nil, 200)
	}
	if got := names(do(otto, "GET", "/projects", nil, 200)); got != "WEBWeb" {
		t.Errorf("outsider sees %q", got)
	}
	if got := names(do(otto, "GET", "/project-stats", nil, 200)); got != "WEB" {
		t.Errorf("outsider stats %q", got)
	}
	for _, path := range []string{"/projects/LEAD", "/projects/LEAD/people"} {
		if body := do(otto, "GET", path, nil, 404); !strings.Contains(body, "Project not found.") {
			t.Errorf("%s: %s", path, body)
		}
	}
	do(otto, "POST", "/projects/LEAD", url.Values{"name": {"Mine"}, "key": {"LEAD"}}, 404)
	do(otto, "POST", "/projects/LEAD/archive", nil, 404)
	do(otto, "POST", "/projects/LEAD/members", url.Values{"user_ids": {otto}}, 404)

	var people []workspace.Member
	if err := json.Unmarshal([]byte(do(mia, "GET", "/projects/LEAD/people", nil, 200)), &people); err != nil || len(people) != 2 {
		t.Errorf("people %+v %v", people, err)
	}
	if err := json.Unmarshal([]byte(do(otto, "GET", "/projects/WEB/people", nil, 200)), &people); err != nil || len(people) != 3 {
		t.Errorf("public people %+v %v", people, err)
	}

	// Members: add and remove Otto; a public project has none.
	do(mia, "POST", "/projects/LEAD/members", url.Values{"user_ids": {"nope"}}, 400)
	do(mia, "POST", "/projects/WEB/members", url.Values{"user_ids": {otto}}, 409)
	if p := project(do(mia, "POST", "/projects/LEAD/members", url.Values{"user_ids": {otto}}, 200)); len(p.Members) != 3 {
		t.Errorf("added %+v", p)
	}
	do(otto, "GET", "/projects/LEAD", nil, 200)
	do(mia, "DELETE", "/projects/LEAD/members/"+otto, nil, 200)
	do(otto, "GET", "/projects/LEAD", nil, 404)

	// Public and back: going private again adds whoever did it.
	if p := project(do(mia, "POST", "/projects/LEAD", url.Values{"name": {"Leadership"}, "key": {"LEAD"}, "private": {"false"}}, 200)); p.Private || len(p.Members) != 0 {
		t.Errorf("made public %+v", p)
	}
	do(otto, "GET", "/projects/LEAD", nil, 200)
	if p := project(do(otto, "POST", "/projects/LEAD", url.Values{"name": {"Leadership"}, "key": {"LEAD"}, "private": {"true"}}, 200)); !p.Private || len(p.Members) != 1 || p.Members[0].ID != otto {
		t.Errorf("made private %+v", p)
	}
	do(mia, "GET", "/projects/LEAD", nil, 404)
	do(ann, "GET", "/projects/LEAD", nil, 200) // admins see every project
	// Removing yourself answers the project, then it's gone.
	do(otto, "DELETE", "/projects/LEAD/members/"+otto, nil, 200)
	do(otto, "GET", "/projects/LEAD", nil, 404)
}
