package notify

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// wednesday is Wednesday 11 November 2026 at 09:00 UTC; the tests remind at hour 8.
var wednesday = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

// dueFixture is a workspace of Ann's with Bob as a member and projects WEB, ARC (archived) and SEC (private).
type dueFixture struct {
	pool          *pgxpool.Pool
	ws            string
	ann, bob      string
	web, arc, sec string
}

func newDueFixture(t *testing.T) dueFixture {
	t.Helper()
	ctx := context.Background()
	f := dueFixture{pool: dbtest.Pool(t)}
	f.ann, f.bob = dbtest.User(t, f.pool, "Ann"), dbtest.User(t, f.pool, "Bob")
	if err := pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, f.ann, "Ann", "Acme due"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select id::text from workspaces where created_by = $1`, f.ann).Scan(&f.ws)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, f.pool, f.ws, f.bob, "member")
	project := func(key string, private, archived bool) string {
		var id string
		if err := f.pool.QueryRow(ctx, `insert into projects (workspace_id, name, key, private, archived_at)
			values ($1, $2, $2, $3, case when $4 then now() end) returning id::text`, f.ws, key, private, archived).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.web, f.arc, f.sec = project("WEB", false, false), project("ARC", false, true), project("SEC", true, false)
	return f
}

// issue adds number in the project, in a status of the category, assigned to who (nil for nobody), due on due.
func (f dueFixture) issue(t *testing.T, proj string, number int, category string, who any, due string) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(context.Background(), `insert into issues (workspace_id, project_id, number, title, status_id, rank, assignee_id, due_date)
		select p.workspace_id, p.id, $2::int, 'Task ' || $2::int, (select id from statuses where workspace_id = p.workspace_id and category = $3 limit 1),
		  'V' || $2::int, $4::uuid, $5::date from projects p where p.id = $1 returning id::text`, proj, number, category, who, due).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// remind runs remindDue at now and returns the user's reminders as "kind WEB-1 2026-11-12", sorted.
func (f dueFixture) remind(t *testing.T, now time.Time, user string) []string {
	t.Helper()
	ctx := context.Background()
	if _, err := (&model{db: f.pool}).remindDue(ctx, now, 8); err != nil {
		t.Fatal(err)
	}
	rows, err := f.pool.Query(ctx, `select n.kind || ' ' || p.key || '-' || i.number || ' ' || (n.data ->> 'due_date')
		from notifications n join issues_all i on i.id = n.issue_id join projects p on p.id = i.project_id
		where n.user_id = $1 and n.actor_id is null order by 1`, user)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDueTiming(t *testing.T) {
	f := newDueFixture(t)
	hawaii := dbtest.User(t, f.pool, "Hawaii")
	dbtest.Join(t, f.pool, f.ws, hawaii, "member")
	exec(t, f.pool, `insert into notification_settings (user_id, timezone) values ($1, 'Pacific/Honolulu')`, hawaii)
	f.issue(t, f.web, 1, "unstarted", f.bob, "2026-11-12") // tomorrow
	f.issue(t, f.web, 2, "started", f.bob, "2026-11-11")   // today
	f.issue(t, f.web, 3, "started", f.bob, "2026-11-10")   // yesterday
	f.issue(t, f.web, 4, "started", f.bob, "2026-11-04")   // 7 days ago
	f.issue(t, f.web, 5, "started", f.bob, "2026-11-03")   // 8 days ago
	// 23:00 on 10 November in Hawaii: WEB-6 is due tomorrow there, WEB-7 the day after.
	f.issue(t, f.web, 6, "started", hawaii, "2026-11-11")
	f.issue(t, f.web, 7, "started", hawaii, "2026-11-12")

	if got := f.remind(t, wednesday.Add(-61*time.Minute), f.bob); len(got) != 0 {
		t.Errorf("before the hour: %v", got)
	}
	want := []string{"due_soon WEB-1 2026-11-12", "overdue WEB-3 2026-11-10", "overdue WEB-4 2026-11-04"}
	if got := f.remind(t, wednesday, f.bob); !slices.Equal(got, want) {
		t.Errorf("after the hour: %v, want %v", got, want)
	}
	if got := f.remind(t, wednesday, hawaii); !slices.Equal(got, []string{"due_soon WEB-6 2026-11-11"}) {
		t.Errorf("Hawaii: %v", got)
	}
	// A day later WEB-2 is overdue and WEB-1 is due today; a day after that WEB-1 is overdue.
	want = []string{"due_soon WEB-1 2026-11-12", "overdue WEB-2 2026-11-11", "overdue WEB-3 2026-11-10", "overdue WEB-4 2026-11-04"}
	if got := f.remind(t, wednesday.AddDate(0, 0, 1), f.bob); !slices.Equal(got, want) {
		t.Errorf("Thursday: %v, want %v", got, want)
	}
	if got := f.remind(t, wednesday.AddDate(0, 0, 2), f.bob); !slices.Contains(got, "overdue WEB-1 2026-11-12") {
		t.Errorf("Friday: %v", got)
	}
}

func TestDueRules(t *testing.T) {
	f := newDueFixture(t)
	f.issue(t, f.web, 1, "completed", f.bob, "2026-11-12")
	f.issue(t, f.web, 2, "canceled", f.bob, "2026-11-12")
	f.issue(t, f.web, 3, "started", nil, "2026-11-12")
	trashed := f.issue(t, f.web, 4, "started", f.bob, "2026-11-12")
	exec(t, f.pool, `update issues set deleted_at = now() where id = $1`, trashed)
	f.issue(t, f.arc, 1, "started", f.bob, "2026-11-12")
	f.issue(t, f.sec, 1, "started", f.bob, "2026-11-12")
	suspended := dbtest.User(t, f.pool, "Gone")
	dbtest.Join(t, f.pool, f.ws, suspended, "member")
	exec(t, f.pool, `update users set suspended_at = now() where id = $1`, suspended)
	f.issue(t, f.web, 5, "started", suspended, "2026-11-12")
	f.issue(t, f.web, 6, "started", f.bob, "2026-11-12")

	if got := f.remind(t, wednesday, f.bob); !slices.Equal(got, []string{"due_soon WEB-6 2026-11-12"}) {
		t.Errorf("Bob: %v", got)
	}
	if got := f.remind(t, wednesday, suspended); len(got) != 0 {
		t.Errorf("suspended: %v", got)
	}
}

func TestDueRepeats(t *testing.T) {
	f := newDueFixture(t)
	id := f.issue(t, f.web, 1, "started", f.bob, "2026-11-12")
	f.remind(t, wednesday, f.bob)
	if got := f.remind(t, wednesday.Add(time.Hour), f.bob); !slices.Equal(got, []string{"due_soon WEB-1 2026-11-12"}) {
		t.Errorf("twice: %v", got)
	}
	exec(t, f.pool, `update issues set due_date = '2026-11-13' where id = $1`, id)
	want := []string{"due_soon WEB-1 2026-11-12", "due_soon WEB-1 2026-11-13"}
	if got := f.remind(t, wednesday.AddDate(0, 0, 1), f.bob); !slices.Equal(got, want) {
		t.Errorf("moved: %v, want %v", got, want)
	}
}

// TestDueEmail: Bob turned due-date emails off, so his reminder is in-app only; Ann's is emailed from Taskwell.
func TestDueEmail(t *testing.T) {
	f := newDueFixture(t)
	ctx := context.Background()
	m := &model{db: f.pool}
	if _, err := m.update(ctx, f.bob, nil, nil, nil, nil, nil, nil, nil, ptr(false), nil, nil); err != nil {
		t.Fatal(err)
	}
	f.issue(t, f.web, 1, "started", f.bob, "2026-11-12")
	f.issue(t, f.web, 2, "started", f.ann, "2026-11-10")
	if got := f.remind(t, wednesday, f.bob); len(got) != 1 {
		t.Fatalf("Bob in-app: %v", got)
	}
	if err := m.skipUnwanted(ctx, 0); err != nil {
		t.Fatal(err)
	}
	var emails map[string]string
	if err := f.pool.QueryRow(ctx, `select json_object_agg(id, email) from users where id in ($1, $2)`, f.ann, f.bob).Scan(&emails); err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for {
		more, err := m.queueNext(ctx, 0, func(_ pgx.Tx, to string, msg message) error {
			subject, _ := instantEmail(msg, "http://app")
			got[to] = append(got[to], msg.actor+": "+subject)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	if s := got[emails[f.bob]]; len(s) != 0 {
		t.Errorf("Bob emailed: %v", s)
	}
	if s := got[emails[f.ann]]; !slices.Equal(s, []string{"Taskwell: WEB-2 is overdue"}) {
		t.Errorf("Ann emailed: %v", s)
	}
}

func TestDueEmails(t *testing.T) {
	m := message{kind: "due_soon", actor: "Taskwell", identifier: "WEB-12", title: "Fix login", workspace: "Acme", slug: "acme",
		dueDate: "2026-11-12"}
	if subject, body := instantEmail(m, "http://app"); subject != "WEB-12 is due tomorrow" ||
		!strings.HasPrefix(body, "WEB-12: Fix login is due tomorrow (Thu, Nov 12).\n\nhttp://app/acme/issue/WEB-12\n\nChange email") {
		t.Errorf("due soon: %q %q", subject, body)
	}
	o := m
	o.kind, o.dueDate = "overdue", "2026-11-11"
	if subject, body := instantEmail(o, "http://app"); subject != "WEB-12 is overdue" ||
		!strings.HasPrefix(body, "WEB-12: Fix login was due Wed, Nov 11 and is still open.\n\nhttp://app/acme/issue/WEB-12") {
		t.Errorf("overdue: %q %q", subject, body)
	}
	_, body := digestEmail([]message{m, o}, "http://app")
	if !strings.Contains(body, "WEB-12: Fix login\nhttp://app/acme/issue/WEB-12\n- due tomorrow\n- overdue since Nov 11\n") {
		t.Errorf("digest: %q", body)
	}
}
