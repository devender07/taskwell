package project

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestUpdates: who may post, edit and delete updates, the latest one's health on the project, and paging. Ann is the
// admin, Mia a plain member of the private LEAD project, Otto a plain member outside it.
func TestUpdates(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto")
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
	dbtest.Join(t, pool, wsID, otto, "member")
	dbtest.SetPlan(t, pool, ann, "ultimate") // room for several projects

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
	decode := func(body string, v any) {
		t.Helper()
		if err := json.Unmarshal([]byte(body), v); err != nil {
			t.Fatal(err)
		}
	}
	health := func(key string) string {
		t.Helper()
		var p Project
		decode(do(ann, "GET", "/projects/"+key, nil, 200), &p)
		var rows []RoadmapProject
		decode(do(ann, "GET", "/roadmap", nil, 200), &rows)
		for _, r := range rows {
			if r.Key == key && (r.Health == nil) != (p.Health == nil) {
				t.Fatalf("roadmap health %v, project %v", r.Health, p.Health)
			}
		}
		if p.Health == nil {
			return ""
		}
		return *p.Health
	}
	text := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Shipping Friday."}]}]}`
	post := func(health string) url.Values { return url.Values{"health": {health}, "body": {text}} }

	do(ann, "POST", "/projects", url.Values{"name": {"Web"}, "key": {"WEB"}}, 201)
	do(ann, "POST", "/projects", url.Values{"name": {"Leadership"}, "key": {"LEAD"}, "private": {"true"}, "member_ids": {mia}}, 201)

	if health("WEB") != "" {
		t.Fatal("health before any update")
	}
	do(ann, "POST", "/projects/WEB/updates", url.Values{"health": {"fine"}, "body": {""}}, 400)
	do(mia, "POST", "/projects/WEB/updates", post("at_risk"), 403) // not a manager, and WEB is public
	do(otto, "POST", "/projects/LEAD/updates", post("at_risk"), 404)
	var first, second Update
	decode(do(ann, "POST", "/projects/WEB/updates", post("on_track"), 201), &first)
	decode(do(ann, "POST", "/projects/WEB/updates", post("off_track"), 201), &second)
	if !second.CanEdit || second.Author == nil || second.Author.ID != ann {
		t.Fatalf("created %+v", second)
	}
	if h := health("WEB"); h != "off_track" {
		t.Fatalf("health %q, want off_track", h)
	}

	do(mia, "POST", "/project-updates/"+second.ID, url.Values{"health": {"on_track"}}, 403)
	do(mia, "DELETE", "/project-updates/"+second.ID, nil, 403)
	do(otto, "POST", "/projects/WEB/updates", post("on_track"), 403)
	var page struct {
		Updates []Update `json:"updates"`
		More    bool     `json:"has_more"`
	}
	decode(do(mia, "GET", "/projects/WEB/updates", nil, 200), &page)
	if len(page.Updates) != 2 || page.Updates[0].CanEdit {
		t.Fatalf("mia sees %+v", page)
	}
	do(ann, "DELETE", "/project-updates/"+second.ID, nil, 204)
	do(ann, "DELETE", "/project-updates/"+second.ID, nil, 404)
	if h := health("WEB"); h != "on_track" {
		t.Fatalf("health after delete %q, want on_track", h)
	}

	// A private project's member may post without projects.manage, and edit their own update.
	var mine Update
	decode(do(mia, "POST", "/projects/LEAD/updates", post("at_risk"), 201), &mine)
	do(mia, "POST", "/project-updates/"+mine.ID, url.Values{"body": {`{"type":"doc"}`}}, 400)
	decode(do(mia, "POST", "/project-updates/"+mine.ID, url.Values{"health": {"on_track"}}, 200), &mine)
	if mine.Health != "on_track" || mine.EditedAt == nil || string(mine.Body) == "" {
		t.Fatalf("edited %+v", mine)
	}
	do(otto, "DELETE", "/project-updates/"+mine.ID, nil, 404)

	do(ann, "POST", "/projects/WEB/archive", nil, 200)
	do(ann, "POST", "/projects/WEB/updates", post("on_track"), 409)
	do(ann, "POST", "/projects/WEB/unarchive", nil, 200)

	// Paging: 25 more updates, a minute apart.
	if _, err := pool.Exec(ctx, `insert into project_updates (project_id, author_id, health, body, created_at)
		select p.id, $2, 'on_track', '{"type":"doc"}', now() - make_interval(mins => g)
		from projects p, generate_series(1, 25) g where p.workspace_id = $1 and p.key = 'WEB'`, wsID, ann); err != nil {
		t.Fatal(err)
	}
	decode(do(ann, "GET", "/projects/WEB/updates", nil, 200), &page)
	if len(page.Updates) != 20 || !page.More || page.Updates[0].ID != first.ID {
		t.Fatalf("first page: %d updates, more %v", len(page.Updates), page.More)
	}
	before := page.Updates[19].CreatedAt.Format(time.RFC3339Nano)
	decode(do(ann, "GET", "/projects/WEB/updates?before="+url.QueryEscape(before), nil, 200), &page)
	if len(page.Updates) != 6 || page.More {
		t.Fatalf("second page: %d updates, more %v", len(page.Updates), page.More)
	}
	do(ann, "GET", "/projects/WEB/updates?before=yesterday", nil, 400)
	do(otto, "GET", "/projects/LEAD/updates", nil, 404)
}
