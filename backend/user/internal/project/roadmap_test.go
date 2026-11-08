package project

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

// TestRoadmap covers project dates, milestones and the roadmap. Ann is the admin, Mia a member who may manage
// projects and is in the private one, Otto a member who may not and is outside it.
func TestRoadmap(t *testing.T) {
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
	dbtest.SetPlan(t, pool, ann, "advanced") // room for several projects
	if _, err := pool.Exec(ctx, `with r as (insert into workspace_roles (workspace_id, name, permissions) values ($1, 'Lead', '{projects.manage}') returning id)
		update workspace_members set role_id = (select id from r) where workspace_id = $1 and user_id = $2`, wsID, mia); err != nil {
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
	dates := func(body string) string {
		t.Helper()
		var p Project
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		s := func(d *string) string {
			if d == nil {
				return "null"
			}
			return *d
		}
		return s(p.StartDate) + " " + s(p.TargetDate)
	}

	// Dates: validated, kept when absent, cleared when empty, ordered against the stored one.
	if body := do(ann, "POST", "/projects", url.Values{"name": {"Web"}, "key": {"WEB"}, "start_date": {"2030-02-30"},
		"target_date": {"soon"}}, 400); !strings.Contains(body, `"start_date":"Enter a valid date."`) ||
		!strings.Contains(body, `"target_date":"Enter a valid date."`) {
		t.Errorf("bad dates: %s", body)
	}
	if body := do(ann, "POST", "/projects", url.Values{"name": {"Web"}, "key": {"WEB"}, "start_date": {"2030-03-01"},
		"target_date": {"2030-02-01"}}, 400); !strings.Contains(body, "Target date must be on or after the start date.") {
		t.Errorf("backwards: %s", body)
	}
	if got := dates(do(ann, "POST", "/projects", url.Values{"name": {"Web"}, "key": {"WEB"}, "start_date": {"2030-02-01"},
		"target_date": {"2030-03-01"}}, 201)); got != "2030-02-01 2030-03-01" {
		t.Errorf("created %q", got)
	}
	if got := dates(do(ann, "POST", "/projects/WEB", url.Values{"name": {"Web"}, "key": {"WEB"}}, 200)); got != "2030-02-01 2030-03-01" {
		t.Errorf("absent dates changed: %q", got)
	}
	do(ann, "POST", "/projects/WEB", url.Values{"name": {"Web"}, "key": {"WEB"}, "target_date": {"2030-01-01"}}, 400)
	if got := dates(do(ann, "POST", "/projects/WEB", url.Values{"name": {"Web"}, "key": {"WEB"}, "start_date": {""}}, 200)); got != "null 2030-03-01" {
		t.Errorf("cleared start: %q", got)
	}
	if got := dates(do(ann, "GET", "/projects/WEB", nil, 200)); got != "null 2030-03-01" {
		t.Errorf("get %q", got)
	}

	// Milestones: projects.manage writes, visibility reads, archived is read-only.
	do(ann, "POST", "/projects", url.Values{"name": {"Leadership"}, "key": {"LEAD"}, "private": {"true"}, "member_ids": {mia},
		"start_date": {"2030-01-15"}}, 201)
	do(ann, "POST", "/projects", url.Values{"name": {"Api"}, "key": {"API"}}, 201)
	do(ann, "POST", "/projects", url.Values{"name": {"Old"}, "key": {"OLD"}, "start_date": {"2029-01-01"}}, 201)
	if body := do(mia, "POST", "/projects/WEB/milestones", url.Values{"name": {" "}, "date": {"x"}}, 400); !strings.Contains(body, "Milestone name is required.") ||
		!strings.Contains(body, "Enter a valid date.") {
		t.Errorf("bad milestone: %s", body)
	}
	do(mia, "POST", "/projects/WEB/milestones", url.Values{"name": {strings.Repeat("a", 61)}, "date": {"2030-02-10"}}, 400)
	do(otto, "POST", "/projects/WEB/milestones", url.Values{"name": {"Beta"}, "date": {"2030-02-10"}}, 403)
	do(mia, "POST", "/projects/NOPE/milestones", url.Values{"name": {"Beta"}, "date": {"2030-02-10"}}, 404)
	var beta Milestone
	if err := json.Unmarshal([]byte(do(mia, "POST", "/projects/WEB/milestones", url.Values{"name": {"Beta"}, "date": {"2030-02-20"}}, 201)), &beta); err != nil ||
		beta.Name != "Beta" || beta.Date != "2030-02-20" {
		t.Fatalf("created %+v %v", beta, err)
	}
	do(mia, "POST", "/projects/WEB/milestones", url.Values{"name": {"Alpha"}, "date": {"2030-02-10"}}, 201)
	do(mia, "POST", "/projects/WEB/milestones/"+beta.ID, url.Values{"name": {"Beta 2"}, "date": {"2030-02-25"}}, 200)
	do(mia, "POST", "/projects/WEB/milestones/nope", url.Values{"name": {"Beta"}, "date": {"2030-02-25"}}, 404)
	do(mia, "POST", "/projects/API/milestones/"+beta.ID, url.Values{"name": {"Beta"}, "date": {"2030-02-25"}}, 404)
	if body := do(otto, "GET", "/projects/WEB/milestones", nil, 200); !strings.Contains(body, `"Alpha"`) ||
		strings.Index(body, "Alpha") > strings.Index(body, "Beta 2") {
		t.Errorf("milestones %s", body)
	}
	lead := do(mia, "POST", "/projects/LEAD/milestones", url.Values{"name": {"Offsite"}, "date": {"2030-01-20"}}, 201)
	var offsite Milestone
	if err := json.Unmarshal([]byte(lead), &offsite); err != nil {
		t.Fatal(err)
	}
	do(otto, "GET", "/projects/LEAD/milestones", nil, 404)
	do(ann, "GET", "/projects/LEAD/milestones", nil, 200)
	if _, err := pool.Exec(ctx, `update workspace_roles set permissions = permissions || '{projects.manage}'
		where workspace_id = $1 and builtin = 'member'`, wsID); err != nil {
		t.Fatal(err)
	}
	do(otto, "POST", "/projects/LEAD/milestones", url.Values{"name": {"Mine"}, "date": {"2030-01-20"}}, 404)
	do(otto, "DELETE", "/projects/LEAD/milestones/"+offsite.ID, nil, 404)
	do(mia, "POST", "/projects/OLD/milestones", url.Values{"name": {"Gone"}, "date": {"2029-02-01"}}, 201)
	do(ann, "POST", "/projects/OLD/archive", nil, 200)
	if body := do(mia, "POST", "/projects/OLD/milestones", url.Values{"name": {"Late"}, "date": {"2029-02-01"}}, 409); !strings.Contains(body, "This project is archived.") {
		t.Errorf("archived: %s", body)
	}
	do(mia, "DELETE", "/projects/LEAD/milestones/"+offsite.ID, nil, 204)
	do(mia, "DELETE", "/projects/LEAD/milestones/"+offsite.ID, nil, 404)
	do(mia, "POST", "/projects/LEAD/milestones", url.Values{"name": {"Offsite"}, "date": {"2030-01-20"}}, 201)

	// Roadmap: active projects the viewer sees, dated first, with progress, milestones and cycles.
	if _, err := pool.Exec(ctx, `with i as (insert into issues (workspace_id, project_id, number, title, status_id, rank)
		  select $1, p.id, row_number() over (), 'x', s.id, (row_number() over ())::text from projects p, (values ('Todo'), ('Done'), ('Done'), ('Cancelled')) v(st)
		  join statuses s on s.name = v.st and s.workspace_id = $1 where p.workspace_id = $1 and p.key = 'WEB')
		insert into cycles (workspace_id, project_id, number, starts_on, ends_on)
		select $1, id, n, d, d + 13 from projects, (values (2, date '2030-02-15'), (1, date '2030-02-01')) v(n, d)
		where workspace_id = $1 and key = 'WEB'`, wsID); err != nil {
		t.Fatal(err)
	}
	type row struct {
		Key        string
		Progress   struct{ Total, Completed, Canceled int }
		Milestones []Milestone
		Cycles     []RoadmapCycle
	}
	roadmap := func(user string) []row {
		t.Helper()
		var rs []row
		if err := json.Unmarshal([]byte(do(user, "GET", "/roadmap", nil, 200)), &rs); err != nil {
			t.Fatal(err)
		}
		return rs
	}
	keys := func(rs []row) string {
		var ks []string
		for _, r := range rs {
			ks = append(ks, r.Key)
		}
		return strings.Join(ks, " ")
	}
	rs := roadmap(mia)
	if got := keys(rs); got != "LEAD WEB API" {
		t.Errorf("mia's roadmap %q", got)
	}
	if got := keys(roadmap(otto)); got != "WEB API" {
		t.Errorf("otto's roadmap %q", got)
	}
	web, lead2 := rs[1], rs[0]
	if web.Progress.Total != 4 || web.Progress.Completed != 2 || web.Progress.Canceled != 1 {
		t.Errorf("progress %+v", web.Progress)
	}
	if len(web.Milestones) != 2 || web.Milestones[0].Name != "Alpha" || len(lead2.Milestones) != 1 || len(rs[2].Milestones) != 0 {
		t.Errorf("milestones %+v %+v", web.Milestones, lead2.Milestones)
	}
	if len(web.Cycles) != 2 || web.Cycles[0].Number != 1 || web.Cycles[0].Name != "Cycle 1" || web.Cycles[0].State != "upcoming" ||
		web.Cycles[1].EndsOn != "2030-02-28" || rs[2].Cycles == nil {
		t.Errorf("cycles %+v", web.Cycles)
	}
	if code, _ := dbtest.Do(t, mux, dbtest.User(t, pool, "Out"), "GET", base+"/roadmap", nil); code != 404 {
		t.Errorf("non-member: %d", code)
	}
}
