package notify

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// monday is Monday 9 November 2026 at 09:00 UTC; the tests send at hour 8.
var monday = time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)

// runWeekly sends every weekly email due at now and returns the lines queued, by recipient address.
func runWeekly(t *testing.T, pool *pgxpool.Pool, now time.Time) map[string][]weeklyLine {
	t.Helper()
	got := map[string][]weeklyLine{}
	m := &model{db: pool}
	for {
		more, err := m.weeklyNext(context.Background(), now, 8, func(_ pgx.Tx, to string, lines []weeklyLine) error {
			got[to] = lines
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			return got
		}
	}
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func lastWeekly(t *testing.T, pool *pgxpool.Pool, user string) time.Time {
	t.Helper()
	var at time.Time
	if err := pool.QueryRow(context.Background(), `select last_weekly_at from notification_settings where user_id = $1`, user).
		Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at.UTC()
}

// TestWeeklyDue: the email is due from Monday at the hour in the user's timezone, once a week.
func TestWeeklyDue(t *testing.T) {
	pool := dbtest.Pool(t)
	utc, hawaii := dbtest.User(t, pool, "Utc"), dbtest.User(t, pool, "Hawaii")
	lastWeek := monday.AddDate(0, 0, -7)
	// Hawaii (UTC-10) was sent last Monday at 08:00 there; at 08:00 UTC this Monday it's still Sunday there.
	exec(t, pool, `insert into notification_settings (user_id, timezone, last_weekly_at) values ($1, null, $3), ($2, 'Pacific/Honolulu', $4)`,
		utc, hawaii, lastWeek, lastWeek.Add(9*time.Hour))
	at8 := monday.Add(-time.Hour)

	runWeekly(t, pool, at8.Add(-time.Minute))
	if got := lastWeekly(t, pool, utc); !got.Equal(lastWeek) {
		t.Errorf("before the hour: sent at %v", got)
	}
	runWeekly(t, pool, at8)
	if got := lastWeekly(t, pool, utc); !got.Equal(at8) {
		t.Errorf("at the hour: last sent %v, want %v", got, at8)
	}
	if got := lastWeekly(t, pool, hawaii); !got.Equal(lastWeek.Add(9 * time.Hour)) {
		t.Errorf("still Sunday in Hawaii: sent at %v", got)
	}
	runWeekly(t, pool, at8.Add(30*time.Minute))
	if got := lastWeekly(t, pool, utc); !got.Equal(at8) {
		t.Errorf("already sent this week: sent again at %v", got)
	}
	runWeekly(t, pool, monday.Add(10*time.Hour)) // 09:00 in Hawaii
	if got := lastWeekly(t, pool, hawaii); !got.Equal(monday.Add(10 * time.Hour)) {
		t.Errorf("Monday 09:00 in Hawaii: last sent %v", got)
	}
}

// TestWeeklySections checks what each section counts for Bob, a member (not admin) of Ann's workspace and Cat's.
func TestWeeklySections(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cat := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cat")
	empty, off, unwanted, fresh := dbtest.User(t, pool, "Empty"), dbtest.User(t, pool, "Off"), dbtest.User(t, pool, "Unwanted"),
		dbtest.User(t, pool, "Fresh")
	var acme, beta string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", "Acme weekly"); err != nil {
			return err
		}
		if err := workspace.CreateFirst(ctx, tx, cat, "Cat", "Beta weekly"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `select (select id::text from workspaces where created_by = $1), (select id::text from workspaces where created_by = $2)`,
			ann, cat).Scan(&acme, &beta)
	}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{bob, empty, off, unwanted, fresh} {
		dbtest.Join(t, pool, acme, u, "member")
	}
	dbtest.Join(t, pool, beta, bob, "member")
	project := func(ws, key string, private, archived bool) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `insert into projects (workspace_id, name, key, private, archived_at)
			values ($1, $2, $2, $3, case when $4 then now() end) returning id::text`, ws, key, private, archived).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	web, arc, sec, oth := project(acme, "WEB", false, false), project(acme, "ARC", false, true), project(acme, "SEC", true, false),
		project(beta, "OTH", false, false)
	// issue adds number in the project, in a status of the category, assigned to who (nil for nobody), due on due.
	issue := func(proj string, number int, category string, who any, due any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `insert into issues (workspace_id, project_id, number, title, status_id, rank, assignee_id, due_date)
			select p.workspace_id, p.id, $2::int, 'Task ' || $2::int, (select id from statuses where workspace_id = p.workspace_id and category = $3 limit 1),
			  'V' || $2::int, $4::uuid, $5::date from projects p where p.id = $1 returning id::text`, proj, number, category, who, due).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	completed := func(issueID string, ago time.Duration) {
		t.Helper()
		exec(t, pool, `insert into issue_events (issue_id, kind, data, created_at) values ($1, 'status', '{"to": {"category": "completed"}}', $2)`,
			issueID, monday.Add(-ago))
	}
	blocks := func(blocker, blocked string) {
		t.Helper()
		exec(t, pool, `insert into issue_links (blocker_id, blocked_id) values ($1, $2)`, blocker, blocked)
	}
	day := 24 * time.Hour

	completed(issue(web, 1, "completed", bob, nil), 2*day)
	completed(issue(web, 2, "started", bob, nil), 2*day) // reopened since
	completed(issue(web, 3, "completed", bob, nil), 8*day)
	trashed := issue(web, 4, "completed", bob, nil)
	completed(trashed, 2*day)
	exec(t, pool, `update issues set deleted_at = now() where id = $1`, trashed)
	completed(issue(arc, 1, "completed", bob, nil), 2*day)
	completed(issue(sec, 1, "completed", bob, nil), 2*day)
	completed(issue(oth, 1, "completed", bob, nil), day)

	issue(web, 5, "started", bob, "2026-11-08")
	issue(web, 6, "unstarted", bob, "2026-11-12")
	issue(web, 7, "unstarted", bob, "2026-11-16")
	issue(web, 8, "canceled", bob, "2026-11-08")

	blocks(issue(web, 10, "started", ann, nil), issue(web, 9, "backlog", bob, nil))
	blocks(issue(web, 12, "canceled", ann, nil), issue(web, 11, "backlog", bob, nil))
	blocks(issue(oth, 2, "started", cat, nil), issue(web, 13, "backlog", bob, nil))
	blocks(issue(web, 14, "started", bob, nil), issue(web, 15, "backlog", ann, nil))
	blocks(issue(web, 16, "started", bob, nil), issue(web, 17, "backlog", bob, nil)) // blocks only his own

	for i, u := range []string{off, unwanted, fresh} {
		issue(web, 20+i, "started", u, "2026-11-01")
	}
	lastWeek := monday.AddDate(0, 0, -7)
	exec(t, pool, `insert into notification_settings (user_id, email, weekly, last_weekly_at)
		values ($1, 'instant', true, $5), ($2, 'instant', true, $5), ($3, 'off', true, $5), ($4, 'digest', false, $5)`,
		bob, empty, off, unwanted, lastWeek)

	sent := runWeekly(t, pool, monday)
	email := func(user string) string {
		var e string
		if err := pool.QueryRow(ctx, `select email from users where id = $1`, user).Scan(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	var got []string
	for _, l := range sent[email(bob)] {
		got = append(got, fmt.Sprintf("%d %s %s", l.section, l.identifier, l.note))
	}
	want := []string{"0 WEB-1 ", "1 WEB-5 overdue since Nov 8", "1 WEB-6 due Thu Nov 12", "2 WEB-9 waiting on WEB-10", "2 WEB-17 waiting on WEB-16",
		"3 WEB-14 blocking WEB-15", "0 OTH-1 "}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Bob's lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for name, u := range map[string]string{"empty week": empty, "email off": off, "weekly off": unwanted, "no settings": fresh} {
		if lines, ok := sent[email(u)]; ok {
			t.Errorf("%s: sent %v", name, lines)
		}
		if got := lastWeekly(t, pool, u); !got.Equal(monday) {
			t.Errorf("%s: last sent %v, want %v", name, got, monday)
		}
	}
	if lines := runWeekly(t, pool, monday.AddDate(0, 0, 7))[email(fresh)]; len(lines) != 1 {
		t.Errorf("no settings, the Monday after: %v", lines)
	}
}

func TestWeeklyEmail(t *testing.T) {
	line := func(slug string, section, n int, note string) weeklyLine {
		return weeklyLine{workspace: strings.ToUpper(slug), slug: slug, section: section,
			identifier: fmt.Sprintf("WEB-%d", n), title: "Task", note: note}
	}
	many := []weeklyLine{line("acme", 0, 1, "")}
	for n := range 17 {
		many = append(many, line("acme", 1, 100+n, "due Thu Nov 12"))
	}
	many = append(many, line("beta", 2, 9, "waiting on WEB-10"))
	cases := []struct {
		name, subject string
		lines         []weeklyLine
		has, hasNot   []string
	}{
		{"one closed", "Your week in Taskwell: 1 closed", []weeklyLine{line("acme", 0, 1, "")},
			[]string{"ACME\n\nClosed last week (1)\n- WEB-1 Task  http://app/acme/issue/WEB-1\n\nChange email settings: http://app/acme/settings/notifications\n"},
			[]string{"Due"}},
		{"zeros left out", "Your week in Taskwell: 2 due, 1 blocking", []weeklyLine{line("acme", 1, 5, "overdue since Nov 3"),
			line("acme", 1, 6, "due Thu Nov 12"), line("acme", 3, 7, "blocking WEB-8")},
			[]string{"Due (2)\n- WEB-5 Task, overdue since Nov 3  http://app/acme/issue/WEB-5\n- WEB-6 Task, due Thu Nov 12",
				"Blocking others (1)\n- WEB-7 Task, blocking WEB-8"}, []string{"Closed", "Blocked ("}},
		{"capped at 15", "Your week in Taskwell: 1 closed, 17 due, 1 blocked", many,
			[]string{"Due (17)\n", "WEB-114 ", "- and 2 more  http://app/acme/my-issues\n", "\n\nBETA\n\nBlocked (1)\n- WEB-9 Task, waiting on WEB-10",
				"settings: http://app/acme/settings"},
			[]string{"WEB-115 ", "WEB-116 "}},
	}
	for _, c := range cases {
		subject, body := weeklyEmail(c.lines, "http://app/")
		if subject != c.subject {
			t.Errorf("%s: subject %q, want %q", c.name, subject, c.subject)
		}
		for _, s := range c.has {
			if !strings.Contains(body, s) {
				t.Errorf("%s: body lacks %q:\n%s", c.name, s, body)
			}
		}
		for _, s := range c.hasNot {
			if strings.Contains(body, s) {
				t.Errorf("%s: body has %q:\n%s", c.name, s, body)
			}
		}
	}
}

// TestWeeklySkipsDeletedUser: a user deleted while the job adds settings rows is skipped, not a failed run.
func TestWeeklySkipsDeletedUser(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	gone := dbtest.User(t, pool, "Gone")
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return workspace.CreateFirst(ctx, tx, gone, "Gone", "Gone weekly")
	}); err != nil {
		t.Fatal(err)
	}
	del, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer del.Rollback(ctx)
	if _, err := del.Exec(ctx, `delete from workspaces where created_by = $1`, gone); err != nil {
		t.Fatal(err)
	}
	if _, err := del.Exec(ctx, `delete from users where id = $1`, gone); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := (&model{db: pool}).weeklyNext(ctx, monday, 8, func(pgx.Tx, string, []weeklyLine) error { return nil })
		done <- err
	}()
	time.Sleep(300 * time.Millisecond) // the job is now waiting on the deleted user's row
	if err := del.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("weekly run failed on a user deleted meanwhile: %v", err)
	}
}
