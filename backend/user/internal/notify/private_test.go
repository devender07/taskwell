package notify

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// TestPrivate: someone who can't see a private project's issue isn't notified about it as a watcher, and older
// notifications about it leave their inbox, unread count and emails. Otto is outside the private project.
func TestPrivate(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, otto := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Otto")
	var wsID, secret, public string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`with w as (select id from workspaces where created_by = $1),
			      p as (insert into projects (workspace_id, name, key, private)
			            select id, k, k, k = 'SEC' from w, (values ('SEC'), ('PUB')) v(k) returning id, key, workspace_id),
			      i as (insert into issues (workspace_id, project_id, number, title, status_id)
			            select p.workspace_id, p.id, 1, p.key, (select id from statuses where workspace_id = p.workspace_id limit 1)
			            from p returning id, title)
			 select (select id::text from w), (select id::text from i where title = 'SEC'), (select id::text from i where title = 'PUB')`,
			ann).Scan(&wsID, &secret, &public)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, otto, "member")
	// Otto watched both and was notified about both before SEC went private (it's private from the start here).
	if _, err := pool.Exec(ctx, `insert into issue_watchers (issue_id, user_id) values ($1, $3), ($2, $3)`, secret, public, otto); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into notifications (user_id, workspace_id, issue_id, kind) values ($1, $2, $3, 'comment'), ($1, $2, $4, 'comment')`,
		otto, wsID, secret, public); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return Record(ctx, tx, Change{IssueID: secret, ActorID: ann, Status: map[string]string{"to": "Done"}, Mentioned: []string{otto}})
	}); err != nil {
		t.Fatal(err)
	}
	m := &model{db: pool}
	ns, _, err := m.list(ctx, otto, wsID, "")
	if err != nil || len(ns) != 1 || string(ns[0].Issue) == "" {
		t.Fatalf("inbox %+v %v", ns, err)
	}
	if n, err := m.unreadCount(ctx, otto, wsID); err != nil || n != 1 {
		t.Errorf("unread %d %v", n, err)
	}
	var stored, emailable int
	if err := pool.QueryRow(ctx, `select count(*), count(*) filter (where `+wanted+`)
		from notifications n left join notification_settings s on s.user_id = n.user_id left join issues i on i.id = n.issue_id
		where n.user_id = $1`, otto).Scan(&stored, &emailable); err != nil {
		t.Fatal(err)
	}
	if stored != 2 || emailable != 1 {
		t.Errorf("stored %d (want 2: no new ones), emailable %d (want 1)", stored, emailable)
	}
}
