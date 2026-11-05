package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestRecord runs Record against the scratch database (see dbtest): who gets what, and status collapsing.
func TestRecord(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cat, dan := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cat"), dbtest.User(t, pool, "Dan")
	var wsID, issueID string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`with w as (select id from workspaces where created_by = $1),
			      p as (insert into projects (workspace_id, name, key) select id, 'P', 'REC' from w returning id, workspace_id)
			 insert into issues (workspace_id, project_id, number, title, status_id, rank)
			 select p.workspace_id, p.id, 1, 'T', (select id from statuses where workspace_id = p.workspace_id limit 1), 'V' from p
			 returning workspace_id::text, id::text`, ann).Scan(&wsID, &issueID)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	dbtest.Join(t, pool, wsID, cat, "member")
	// Dan watches but isn't a member, so he gets nothing.
	if _, err := pool.Exec(ctx, `insert into issue_watchers (issue_id, user_id) values ($1, $2), ($1, $3), ($1, $4), ($1, $5)`,
		issueID, ann, bob, cat, dan); err != nil {
		t.Fatal(err)
	}
	record := func(c Change) {
		t.Helper()
		c.IssueID = issueID
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return Record(ctx, tx, c) }); err != nil {
			t.Fatal(err)
		}
	}
	// count returns the user's notifications of the kind, and the newest one's "to".
	count := func(user, kind string) (n int, to string) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			`select count(*), coalesce((array_agg(data ->> 'to' order by created_at desc, id desc))[1], '')
			 from notifications where user_id = $1 and issue_id = $2 and kind = $3`, user, issueID, kind).Scan(&n, &to); err != nil {
			t.Fatal(err)
		}
		return n, to
	}
	want := func(step, user, kind string, n int, to string) {
		t.Helper()
		if gotN, gotTo := count(user, kind); gotN != n || to != "" && gotTo != to {
			t.Errorf("%s: %s has %d %s (to %q), want %d (to %q)", step, user, gotN, kind, gotTo, n, to)
		}
	}
	move := func(from, to string) map[string]string { return map[string]string{"from": from, "to": to} }

	record(Change{ActorID: ann, Status: move("Todo", "Doing")})
	want("first move", bob, "status", 1, "Doing")
	want("first move", cat, "status", 1, "Doing")
	want("first move", ann, "status", 0, "")
	want("first move", dan, "status", 0, "")

	record(Change{ActorID: ann, Status: move("Doing", "Done")})
	want("second move collapses", bob, "status", 1, "Done")

	if _, err := pool.Exec(ctx, `update notifications set read_at = now() where user_id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	record(Change{ActorID: ann, Status: move("Done", "Todo")})
	want("after reading, a new one", bob, "status", 2, "Todo")
	want("unread collapses", cat, "status", 1, "Todo")

	record(Change{ActorID: ann, CommentID: "00000000-0000-0000-0000-000000000001", NewComment: true,
		Mentioned: []string{bob, dan, "not-a-uuid"}})
	want("comment", bob, "mention", 1, "")
	want("comment", bob, "comment", 0, "")
	want("comment", cat, "comment", 1, "")
	want("comment", dan, "mention", 0, "")

	record(Change{ActorID: bob, Assignee: cat, Status: move("Todo", "Doing")})
	want("assign and move", cat, "assigned", 1, "")
	want("assign and move", cat, "status", 1, "Doing")
	want("assign and move", ann, "status", 1, "Doing")
	want("assign and move", bob, "status", 2, "")
}

// TestDigestTimezone checks that the digest hour is read in each user's timezone: at hour 12, a user whose clock
// has passed noon today is due, and one whose clock hasn't isn't.
func TestDigestTimezone(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	const hour = 12
	// Etc/GMT+N is UTC-N. Between UTC-12 and UTC+14 some zone is past noon and some isn't, whatever the time.
	var past, before string
	for off := -12; off <= 14; off++ {
		tz := "UTC"
		if off != 0 {
			tz = fmt.Sprintf("Etc/GMT%+d", -off)
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Fatal(err)
		}
		// Keep clear of the hour itself, so the run can't straddle it.
		switch h := time.Now().In(loc).Hour(); {
		case h > hour && h < 23 && past == "":
			past = tz
		case h < hour-1 && before == "":
			before = tz
		}
	}
	if past == "" || before == "" {
		t.Fatalf("no zones found: past %q before %q", past, before)
	}
	due, notDue := dbtest.User(t, pool, "Due"), dbtest.User(t, pool, "Later")
	for user, tz := range map[string]string{due: past, notDue: before} {
		if _, err := (&model{db: pool}).update(ctx, user, ptr("digest"), nil, nil, nil, nil, nil, nil, nil, &tz); err != nil {
			t.Fatal(err)
		}
	}
	m := &model{db: pool}
	for {
		more, err := m.digestNext(ctx, hour, func(pgx.Tx, string, []message) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	sent := func(user string) bool {
		var ok bool
		if err := pool.QueryRow(ctx, `select last_digest_at is not null from notification_settings where user_id = $1`, user).
			Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !sent(due) {
		t.Errorf("user in %s (past %d:00) got no digest", past, hour)
	}
	if sent(notDue) {
		t.Errorf("user in %s (before %d:00) got a digest", before, hour)
	}
	if _, err := m.update(ctx, due, nil, nil, nil, nil, nil, nil, nil, nil, ptr("Not/AZone")); err != errBadTimezone {
		t.Errorf("unknown timezone: %v, want errBadTimezone", err)
	}
}

func ptr[T any](v T) *T { return &v }

// TestTimezoneUnset checks that a timezone is null until chosen, survives updates that leave it out, clears with ""
// and that the digest reads null as UTC.
func TestTimezoneUnset(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	m := &model{db: pool}
	user := dbtest.User(t, pool, "Zone")
	tz := func(s Settings, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if s.Timezone == nil {
			return "null"
		}
		return *s.Timezone
	}
	if got := tz(m.settings(ctx, user)); got != "null" {
		t.Fatalf("no settings: timezone %s, want null", got)
	}
	if got := tz(m.update(ctx, user, ptr("digest"), nil, nil, nil, nil, nil, nil, nil, nil)); got != "null" {
		t.Fatalf("first update without a zone: %s, want null", got)
	}
	if got := tz(m.update(ctx, user, nil, nil, nil, nil, nil, nil, nil, nil, ptr("Europe/Berlin"))); got != "Europe/Berlin" {
		t.Fatalf("set: %s", got)
	}
	if got := tz(m.update(ctx, user, nil, ptr(false), nil, nil, nil, nil, nil, nil, nil)); got != "Europe/Berlin" {
		t.Fatalf("update without a zone changed it to %s", got)
	}
	if got := tz(m.update(ctx, user, nil, nil, nil, nil, nil, nil, nil, nil, ptr(""))); got != "null" {
		t.Fatalf(`"" left %s, want null`, got)
	}
	// Hour 0 UTC has always passed today, so a null-zone digest user is due.
	for {
		more, err := m.digestNext(ctx, 0, func(pgx.Tx, string, []message) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			break
		}
	}
	var sent bool
	if err := pool.QueryRow(ctx, `select last_digest_at is not null from notification_settings where user_id = $1`, user).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Error("null timezone got no digest at hour 0 UTC")
	}
}

// TestChatEmail checks a direct message's email waits 10 minutes, and isn't sent when the message was read by then,
// while a chat mention goes after the usual settle time unless the chat setting is off.
func TestChatEmail(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, chatID string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id from workspaces where created_by = $1)
			insert into chats (workspace_id, kind, pair) select id, 'direct', 'x' from w returning workspace_id::text, id::text`, ann).
			Scan(&wsID, &chatID)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")
	send := func(kind string) string {
		t.Helper()
		var msgID string
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `insert into messages (chat_id, author_id, body) values ($1, $2, '{}') returning id::text`,
				chatID, ann).Scan(&msgID); err != nil {
				return err
			}
			c := ChatChange{ChatID: chatID, MessageID: msgID, ActorID: ann, Excerpt: "hello", Direct: bob}
			if kind == "chat_mention" {
				c = ChatChange{ChatID: chatID, MessageID: msgID, ActorID: ann, Excerpt: "hello", Mentioned: []string{bob}}
			}
			return RecordChat(ctx, tx, c)
		}); err != nil {
			t.Fatal(err)
		}
		return msgID
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	m := &model{db: pool}
	// queued returns the subjects queued for bob in one run with no settle time.
	queued := func() []string {
		t.Helper()
		if err := m.skipUnwanted(ctx, 0); err != nil {
			t.Fatal(err)
		}
		var got []string
		for {
			more, err := m.queueNext(ctx, 0, func(_ pgx.Tx, to string, msg message) error {
				if msg.actor == "Ann" && msg.chatID == chatID { // other tests' notifications may be queued too
					subject, _ := instantEmail(msg, "http://app")
					got = append(got, subject)
				}
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
	send("message")
	second := send("message")
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from notifications where user_id = $1 and kind = 'message' and message_id = $2`,
		bob, second).Scan(&n); err != nil || n != 1 {
		t.Fatalf("collapsed message notifications: %d %v", n, err)
	}
	if got := queued(); len(got) != 0 {
		t.Errorf("message emailed before 10 minutes: %v", got)
	}
	exec(`update notifications set created_at = now() - interval '11 minutes' where user_id = $1`, bob)
	if got := queued(); len(got) != 1 || got[0] != "Ann sent you a message" {
		t.Errorf("message after 10 minutes: %v", got)
	}

	send("message")
	exec(`update notifications set created_at = now() - interval '11 minutes', read_at = now() where user_id = $1 and emailed_at is null`, bob)
	if got := queued(); len(got) != 0 {
		t.Errorf("read message emailed: %v", got)
	}
	var skipped int
	if err := pool.QueryRow(ctx, `select count(*) from notifications where user_id = $1 and emailed_at is null`, bob).Scan(&skipped); err != nil || skipped != 0 {
		t.Errorf("read message not marked emailed: %d %v", skipped, err)
	}

	send("chat_mention")
	if got := queued(); len(got) != 1 || got[0] != "Ann mentioned you in a message" {
		t.Errorf("chat mention: %v", got)
	}
	if _, err := m.update(ctx, bob, nil, nil, nil, nil, nil, ptr(false), nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	send("chat_mention")
	if got := queued(); len(got) != 0 {
		t.Errorf("chat mention with chat emails off: %v", got)
	}
}

// TestInviteNotification: "Not now" on an invite leaves an invite notification from the inviter, never emailed, that
// says the invite is available until it's cancelled, and still names the invite and its workspace after.
func TestInviteNotification(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var team, teamSlug, own, ownSlug, inv string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", "Invite team"); err != nil {
			return err
		}
		if err := workspace.CreateFirst(ctx, tx, bob, "Bob", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`with a as (select id, slug from workspaces where created_by = $1), b as (select id, slug from workspaces where created_by = $2),
			      i as (insert into workspace_invites (workspace_id, email, token_hash, invited_by, expires_at)
			            select a.id, u.email, 'invite-note', $1, now() + interval '1 day' from a, users u where u.id = $2 returning id)
			 select a.id::text, a.slug, b.id::text, b.slug, i.id::text from a, b, i`, ann, bob).Scan(&team, &teamSlug, &own, &ownSlug, &inv)
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	workspace.New(pool, dbtest.Auth, nil, nil).Register(mux)
	if code, body := dbtest.Do(t, mux, bob, "POST", "/api/me/invites/"+inv+"/dismiss", url.Values{"workspace": {ownSlug}}); code != 204 {
		t.Fatalf("dismiss: %d %s", code, body)
	}
	m := &model{db: pool}
	invite := func(want string) {
		t.Helper()
		ns, _, err := m.list(ctx, bob, own, "", false)
		if err != nil || len(ns) != 1 || ns[0].Kind != "invite" {
			t.Fatalf("inbox %+v %v", ns, err)
		}
		if got := string(ns[0].Invite); got != want {
			t.Errorf("invite %s, want %s", got, want)
		}
		if js, _ := json.Marshal(ns[0]); !strings.Contains(string(js), `"issue":null,"chat":null,"project":null`) ||
			!strings.Contains(string(ns[0].Actor), ann) {
			t.Errorf("notification %s", js)
		}
	}
	invite(`{"id" : "` + inv + `", "workspace" : "Invite team", "available" : true}`)
	var emailable bool
	if err := pool.QueryRow(ctx, `select bool_or(`+wanted+`) from notifications n left join notification_settings s on s.user_id = n.user_id
		left join issues i on i.id = n.issue_id where n.user_id = $1`, bob).Scan(&emailable); err != nil || emailable {
		t.Errorf("invite notification emailable: %v %v", emailable, err)
	}
	if code, body := dbtest.Do(t, mux, ann, "DELETE", "/api/workspaces/"+teamSlug+"/invites/"+inv, nil); code != 204 {
		t.Fatalf("cancel: %d %s", code, body)
	}
	invite(`{"id" : "` + inv + `", "workspace" : "Invite team", "available" : false}`)
}
