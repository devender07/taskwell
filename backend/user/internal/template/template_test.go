package template

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

func date(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestNext(t *testing.T) {
	cases := []struct {
		name, from string
		every      int
		unit, want string
	}{
		{"days", "2026-11-10", 3, "day", "2026-11-13"},
		{"weeks", "2026-11-10", 2, "week", "2026-11-24"},
		{"month end", "2026-01-31", 1, "month", "2026-02-28"},
		{"leap year", "2028-01-31", 1, "month", "2028-02-29"},
		{"across a year", "2026-11-30", 3, "month", "2027-02-28"},
		{"plain month", "2026-11-10", 1, "month", "2026-12-10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := next(date(c.from), c.every, c.unit).Format(time.DateOnly); got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
	// Missed runs land on the first date after today, not a burst.
	if got := upcoming(date("2026-10-01"), 1, "week", date("2026-11-04")).Format(time.DateOnly); got != "2026-11-05" {
		t.Errorf("upcoming %s", got)
	}
}

func TestApply(t *testing.T) {
	today := date("2026-11-04")
	var v values
	errs := apply(&v, url.Values{"name": {""}, "repeat_every": {"0"}, "repeat_unit": {"year"}, "starts_on": {"2026-11-03"}}, today)
	want := map[string]string{"name": errName, "repeat_every": errEvery, "repeat_unit": errUnit, "starts_on": errStartsOn,
		"title": "", "project": ""}
	for k, msg := range want {
		if msg != "" && errs[k] != msg {
			t.Errorf("%s: %q, want %q", k, errs[k], msg)
		}
	}
	v = values{}
	errs = apply(&v, url.Values{"name": {"Weekly"}, "repeat_every": {"1"}, "repeat_unit": {"week"}, "starts_on": {"2026-11-04"}}, today)
	if errs["title"] != errTitle || errs["project"] != errProject {
		t.Errorf("repeat without title and project: %v", errs)
	}
	// Unpausing a stale repeat moves it on; all-empty repeat fields stop it.
	v = values{name: "x", title: "t", projectKey: "ENG", every: 1, unit: "day", nextOn: "2026-10-01", paused: true}
	apply(&v, url.Values{"paused": {"0"}}, today)
	if v.nextOn != "2026-11-05" {
		t.Errorf("unpaused next_on %s", v.nextOn)
	}
	apply(&v, url.Values{"repeat_every": {""}, "repeat_unit": {""}, "starts_on": {""}}, today)
	if v.every != 0 || v.nextOn != "" {
		t.Errorf("repeat not stopped: %+v", v)
	}
}

// TestTemplates: CRUD and permissions, then the runner: a due template makes exactly one issue, and one whose
// project is archived is paused instead. Ann is the admin, Otto a plain member.
func TestTemplates(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Otto")
	var wsID, slug, projectID string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select id::text, slug from workspaces where created_by = $1`, ann).Scan(&wsID, &slug); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `insert into projects (workspace_id, name, key, created_by) values ($1, 'Ops', 'OPS', $2)
			returning id::text`, wsID, ann).Scan(&projectID)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, otto, "member")

	h := New(pool, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member)
	mux := http.NewServeMux()
	h.Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}
	today := utcToday().Format(time.DateOnly)
	weekly := url.Values{"name": {"Standup notes"}, "title": {"Standup"}, "project": {"ops"}, "priority": {"2"},
		"repeat_every": {"1"}, "repeat_unit": {"week"}, "starts_on": {today}}

	do(otto, "POST", "/templates", weekly, 403)
	do(ann, "POST", "/templates", url.Values{"name": {"x"}, "project": {"NOPE"}}, 400)
	do(ann, "POST", "/templates", url.Values{"name": {"x"}, "project": {"OPS"}, "assignee_id": {otto + "0"}}, 400)
	var tp Template
	if err := json.Unmarshal([]byte(do(ann, "POST", "/templates", weekly, 201)), &tp); err != nil {
		t.Fatal(err)
	}
	if tp.Project == nil || tp.Project.Key != "OPS" || tp.Repeat == nil || tp.Repeat.NextOn != today || !tp.CanEdit {
		t.Fatalf("created %+v", tp)
	}
	var ts []Template
	if err := json.Unmarshal([]byte(do(otto, "GET", "/templates", nil, 200)), &ts); err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].CanEdit {
		t.Fatalf("otto sees %+v", ts)
	}
	do(ann, "POST", "/templates/"+tp.ID, url.Values{"title": {""}}, 400) // it repeats
	do(ann, "POST", "/templates/"+tp.ID, url.Values{"name": {"Standup"}}, 200)

	// Due a week ago: one issue, dated then, and next_on moves past today.
	if _, err := pool.Exec(ctx, `update issue_templates set next_on = $2 where id = $1`, tp.ID, utcToday().AddDate(0, 0, -7)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := h.RunDue(ctx, utcToday()); err != nil {
			t.Fatal(err)
		}
	}
	var n, priority int
	var title string
	if err := pool.QueryRow(ctx, `select count(*), max(title), max(priority) from issues where project_id = $1`, projectID).
		Scan(&n, &title, &priority); err != nil {
		t.Fatal(err)
	}
	if want := "Standup · " + utcToday().AddDate(0, 0, -7).Format("Jan 2"); n != 1 || title != want || priority != 2 {
		t.Fatalf("%d issues, %q priority %d; want 1, %q", n, title, priority, want)
	}
	var nextOn time.Time
	if err := pool.QueryRow(ctx, `select next_on from issue_templates where id = $1`, tp.ID).Scan(&nextOn); err != nil {
		t.Fatal(err)
	}
	if !nextOn.Equal(utcToday().AddDate(0, 0, 7)) {
		t.Fatalf("next_on %s", nextOn)
	}

	// Archived project: paused, no issue.
	if _, err := pool.Exec(ctx, `update projects set archived_at = now() where id = $1;`, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update issue_templates set next_on = $2 where id = $1`, tp.ID, utcToday()); err != nil {
		t.Fatal(err)
	}
	if created, err := h.RunDue(ctx, utcToday()); err != nil || created != 0 {
		t.Fatalf("archived: created %d, %v", created, err)
	}
	var paused bool
	if err := pool.QueryRow(ctx, `select paused from issue_templates where id = $1`, tp.ID).Scan(&paused); err != nil || !paused {
		t.Fatalf("paused %v, %v", paused, err)
	}

	do(otto, "DELETE", "/templates/"+tp.ID, nil, 403)
	do(ann, "DELETE", "/templates/"+tp.ID, nil, 204)
	do(ann, "DELETE", "/templates/"+tp.ID, nil, 404)
}
