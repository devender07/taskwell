package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/realtime"
)

// Record writes the change's notifications and new watchers in tx, the change's own transaction, and tells the
// recipients' tabs. Recipients and watchers who aren't members of the issue's workspace are skipped. A status
// notification the recipient hasn't read from the last 10 minutes on the same issue is updated (new "to", time and
// actor, emailed again) instead of adding another.
func Record(ctx context.Context, tx pgx.Tx, c Change) error {
	// Mention ids come from documents users write; one that isn't a uuid can't be a member.
	c.Mentioned = slices.DeleteFunc(slices.Clone(c.Mentioned), func(id string) bool { return !uuidRe.MatchString(id) })
	var watchers []string
	if c.Status != nil || c.NewComment {
		rows, err := tx.Query(ctx, `select user_id::text from issue_watchers where issue_id = $1`, c.IssueID)
		if err != nil {
			return fmt.Errorf("list watchers: %w", err)
		}
		if watchers, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return fmt.Errorf("list watchers: %w", err)
		}
	}
	ns := notes(c, watchers)
	users, kinds, datas := make([]string, len(ns)), make([]string, len(ns)), make([]string, len(ns))
	for i, n := range ns {
		data, err := json.Marshal(n.data)
		if err != nil {
			return err
		}
		users[i], kinds[i], datas[i] = n.userID, n.kind, string(data)
	}
	var notified []string
	if len(ns) > 0 {
		// Watchers are unique, so each user has at most one status note to bump.
		rows, err := tx.Query(ctx,
			`with n as (select * from unnest($3::uuid[], $4::text[], $5::jsonb[]) as n(user_id, kind, data)),
			 bumped as (
			   update notifications t set data = jsonb_set(t.data, '{to}', n.data -> 'to'), actor_id = $2, created_at = now(),
			     emailed_at = null
			   from n
			   where n.kind = 'status' and t.id = (select id from notifications where user_id = n.user_id and issue_id = $1
			     and kind = 'status' and read_at is null and created_at > now() - interval '10 minutes' order by id desc limit 1)
			   returning t.user_id),
			 added as (
			   insert into notifications (user_id, workspace_id, issue_id, actor_id, kind, data)
			   select n.user_id, i.workspace_id, i.id, $2, n.kind, n.data
			   from n join issues i on i.id = $1 join workspace_members m on m.workspace_id = i.workspace_id and m.user_id = n.user_id
			   where not (n.kind = 'status' and n.user_id in (select user_id from bumped))
			   returning user_id)
			 select distinct user_id::text from (select user_id from bumped union all select user_id from added) x`,
			c.IssueID, c.ActorID, users, kinds, datas)
		if err != nil {
			return fmt.Errorf("add notifications: %w", err)
		}
		if notified, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return fmt.Errorf("add notifications: %w", err)
		}
	}
	if len(notified) > 0 {
		var wsID, slug string
		if err := tx.QueryRow(ctx, `select w.id::text, w.slug from issues i join workspaces w on w.id = i.workspace_id where i.id = $1`,
			c.IssueID).Scan(&wsID, &slug); err != nil {
			return fmt.Errorf("notified workspace: %w", err)
		}
		for _, u := range notified {
			if err := realtime.Publish(ctx, tx, changed(slug, c.ActorID, u)); err != nil {
				return err
			}
		}
	}
	watch := slices.Concat(c.Watch, c.Mentioned)
	if c.Assignee != "" {
		watch = append(watch, c.Assignee)
	}
	if len(watch) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`insert into issue_watchers (issue_id, user_id)
		 select i.id, m.user_id from issues i join workspace_members m on m.workspace_id = i.workspace_id
		 where i.id = $1 and m.user_id = any($2::uuid[])
		 on conflict do nothing`, c.IssueID, watch); err != nil {
		return fmt.Errorf("add watchers: %w", err)
	}
	return nil
}

// ChatChange is a new or edited chat message that may notify people, recorded with RecordChat in its transaction.
type ChatChange struct {
	ChatID, MessageID, ActorID string
	Excerpt                    string   // the message's opening text (Excerpt)
	Mentioned                  []string // people newly mentioned who can see the chat
	Direct                     string   // the other person in a direct chat, for a new message; "" otherwise
}

// RecordChat writes the message's notifications in tx and tells the recipients' tabs: a chat_mention to each person
// mentioned, and a message to the other person in a direct chat (unless mentioned). The recipient's unread message
// notification for the chat, if any, is updated to this message instead of adding another. Never the actor, and
// only workspace members.
func RecordChat(ctx context.Context, tx pgx.Tx, c ChatChange) error {
	ns := chatNotes(c)
	if len(ns) == 0 {
		return nil
	}
	users, kinds := make([]string, len(ns)), make([]string, len(ns))
	for i, n := range ns {
		users[i], kinds[i] = n.userID, n.kind
	}
	data, err := json.Marshal(map[string]string{"excerpt": c.Excerpt, "message_id": c.MessageID})
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx,
		`with n as (select * from unnest($1::uuid[], $2::text[]) as n(user_id, kind)),
		 bumped as (
		   update notifications t set message_id = $4, data = $6, actor_id = $5, created_at = now(), emailed_at = null
		   from n
		   where n.kind = 'message' and t.id = (select id from notifications where user_id = n.user_id and chat_id = $3
		     and kind = 'message' and read_at is null order by id desc limit 1)
		   returning t.user_id),
		 added as (
		   insert into notifications (user_id, workspace_id, chat_id, message_id, actor_id, kind, data)
		   select n.user_id, c.workspace_id, c.id, $4, $5, n.kind, $6
		   from n join chats c on c.id = $3 join workspace_members m on m.workspace_id = c.workspace_id and m.user_id = n.user_id
		   where not (n.kind = 'message' and n.user_id in (select user_id from bumped))
		   returning user_id)
		 select distinct user_id::text from (select user_id from bumped union all select user_id from added) x`,
		users, kinds, c.ChatID, c.MessageID, c.ActorID, data)
	if err != nil {
		return fmt.Errorf("add chat notifications: %w", err)
	}
	notified, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("add chat notifications: %w", err)
	}
	if len(notified) == 0 {
		return nil
	}
	var slug string
	if err := tx.QueryRow(ctx, `select w.slug from chats c join workspaces w on w.id = c.workspace_id where c.id = $1`,
		c.ChatID).Scan(&slug); err != nil {
		return fmt.Errorf("notified workspace: %w", err)
	}
	for _, u := range notified {
		if err := realtime.Publish(ctx, tx, changed(slug, c.ActorID, u)); err != nil {
			return err
		}
	}
	return nil
}

// ReadChat marks the user's unread chat notifications for the chat's messages up to upTo read, in tx, and tells their
// tabs when there were any.
func ReadChat(ctx context.Context, tx pgx.Tx, userID, chatID, slug, upTo string) error {
	tag, err := tx.Exec(ctx,
		`update notifications set read_at = now()
		 where user_id = $1 and chat_id = $2 and read_at is null and message_id <= $3`, userID, chatID, upTo)
	if err != nil {
		return fmt.Errorf("read chat notifications: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return realtime.Publish(ctx, tx, changed(slug, userID, userID))
}

// ForgetMessage deletes the notifications about a deleted message, in tx, and tells the recipients' tabs.
func ForgetMessage(ctx context.Context, tx pgx.Tx, messageID, slug, actorID string) error {
	rows, err := tx.Query(ctx, `delete from notifications where message_id = $1 returning user_id::text`, messageID)
	if err != nil {
		return fmt.Errorf("delete message notifications: %w", err)
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("delete message notifications: %w", err)
	}
	for _, u := range slices.Compact(slices.Sorted(slices.Values(users))) {
		if err := realtime.Publish(ctx, tx, changed(slug, actorID, u)); err != nil {
			return err
		}
	}
	return nil
}

// --- inbox ---

type Notification struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Actor     json.RawMessage `json:"actor"`
	Issue     json.RawMessage `json:"issue"` // null for chat kinds
	Chat      json.RawMessage `json:"chat"`  // {id, kind, name}; null for issue kinds
	Data      json.RawMessage `json:"data"`
	Read      bool            `json:"read"`
	CreatedAt time.Time       `json:"created_at"`
}

const pageSize = 30

type model struct{ db *pgxpool.Pool }

// list returns a page of the user's notifications in the workspace, newest first, older than before unless it's "",
// and whether there are more.
func (m *model) list(ctx context.Context, userID, workspaceID, before string) ([]Notification, bool, error) {
	rows, err := m.db.Query(ctx,
		`select n.id::text, n.kind, (select json_build_object('id', a.id, 'name', a.name) from users a where a.id = n.actor_id),
		        case when i.id is not null then json_build_object('identifier', p.key || '-' || i.number, 'title', i.title,
		          'status', json_build_object('id', s.id, 'name', s.name, 'category', s.category, 'position', s.position)) end,
		        case when c.id is not null then json_build_object('id', c.id, 'kind', c.kind, 'name', chat_name(c, n.user_id)) end,
		        n.data, n.read_at is not null, n.created_at
		 from notifications n left join issues i on i.id = n.issue_id left join projects p on p.id = i.project_id
		 left join statuses s on s.id = i.status_id left join chats c on c.id = n.chat_id
		 where n.user_id = $1 and n.workspace_id = $2 and ($3 = '' or n.id < $3::uuid)
		 order by n.id desc limit $4`, userID, workspaceID, before, pageSize+1)
	if err != nil {
		return nil, false, fmt.Errorf("list notifications: %w", err)
	}
	ns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notification, error) {
		var n Notification
		err := row.Scan(&n.ID, &n.Kind, &n.Actor, &n.Issue, &n.Chat, &n.Data, &n.Read, &n.CreatedAt)
		n.CreatedAt = n.CreatedAt.UTC()
		return n, err
	})
	if err != nil {
		return nil, false, fmt.Errorf("list notifications: %w", err)
	}
	if len(ns) > pageSize {
		return ns[:pageSize], true, nil
	}
	return ns, false, nil
}

func (m *model) unreadCount(ctx context.Context, userID, workspaceID string) (int, error) {
	var n int
	// Chat kinds are left out: the inbox badge adds chats/unread-count, which already counts those messages.
	err := m.db.QueryRow(ctx,
		`select count(*) from notifications
		 where user_id = $1 and workspace_id = $2 and read_at is null and kind not in ('chat_mention', 'message')`, userID, workspaceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return n, nil
}

// changed tells the recipient's tabs their notifications in the workspace changed.
func changed(slug, actorID, recipientID string) realtime.Event {
	ws := realtime.Workspace(slug)
	return realtime.Event{Kind: "notifications.changed", Workspace: slug, Actor: actorID, Audience: realtime.ToUser(recipientID),
		Paths: []string{ws + "/notifications", ws + "/notifications/unread-count"}}
}

// setRead marks the user's notifications in the workspace with the ids (all of them when all is true) read or unread.
func (m *model) setRead(ctx context.Context, userID, workspaceID, slug string, ids []string, all, read bool) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`update notifications set read_at = case when $5 then coalesce(read_at, now()) end
			 where user_id = $1 and workspace_id = $2 and ($4 or id = any($3::uuid[]))`, userID, workspaceID, ids, all, read); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, changed(slug, userID, userID))
	})
	if err != nil {
		return fmt.Errorf("mark notifications: %w", err)
	}
	return nil
}

// --- settings ---

type Settings struct {
	Email       string  `json:"email"`
	Mentions    bool    `json:"mentions"`
	Assignments bool    `json:"assignments"`
	Status      bool    `json:"status"`
	Comments    bool    `json:"comments"`
	Chat        bool    `json:"chat"`     // chat mentions (with Mentions) and direct messages
	Timezone    *string `json:"timezone"` // IANA name the digest goes out at digest_hour_utc in; nil (UTC) until chosen
}

const settingsCols = `email, mentions, assignments, status, comments, chat, timezone`

func scanSettings(row pgx.Row) (Settings, error) {
	var s Settings
	err := row.Scan(&s.Email, &s.Mentions, &s.Assignments, &s.Status, &s.Comments, &s.Chat, &s.Timezone)
	return s, err
}

// errBadTimezone is a timezone Go knows but Postgres doesn't.
var errBadTimezone = errors.New("unknown timezone")

// settings returns the user's settings; the defaults when they have none.
func (m *model) settings(ctx context.Context, userID string) (Settings, error) {
	s, err := scanSettings(m.db.QueryRow(ctx, `select `+settingsCols+` from notification_settings where user_id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{"instant", true, true, true, true, true, nil}, nil
	}
	if err != nil {
		return s, fmt.Errorf("notification settings: %w", err)
	}
	return s, nil
}

// update changes the user's non-nil settings and returns them all, telling their other tabs. A timezone of ""
// clears it (back to never chosen).
func (m *model) update(ctx context.Context, userID string, email *string, mentions, assignments, status, comments, chat *bool,
	timezone *string) (Settings, error) {
	var s Settings
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if timezone != nil && *timezone != "" {
			// The digest query converts with it, so one Postgres doesn't know would fail every digest.
			var ok bool
			if err := tx.QueryRow(ctx, `select exists(select 1 from pg_timezone_names where name = $1)`, *timezone).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return errBadTimezone
			}
		}
		var err error
		if s, err = scanSettings(tx.QueryRow(ctx,
			`insert into notification_settings as s (user_id, email, mentions, assignments, status, comments, chat, timezone)
			 values ($1, coalesce($2, 'instant'), coalesce($3, true), coalesce($4, true), coalesce($5, true), coalesce($6, true),
			   coalesce($8, true), nullif($7, ''))
			 on conflict (user_id) do update set email = coalesce($2, s.email), mentions = coalesce($3, s.mentions),
			   assignments = coalesce($4, s.assignments), status = coalesce($5, s.status), comments = coalesce($6, s.comments),
			   chat = coalesce($8, s.chat), timezone = case when $7::text is null then s.timezone else nullif($7, '') end
			 returning `+settingsCols, userID, email, mentions, assignments, status, comments, timezone, chat)); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, realtime.Event{Kind: "profile.changed", Actor: userID, Audience: realtime.ToUser(userID),
			Paths: []string{"/me/notification-settings"}})
	})
	if errors.Is(err, errBadTimezone) {
		return s, err
	}
	if err != nil {
		return s, fmt.Errorf("update notification settings: %w", err)
	}
	return s, nil
}

// --- email worker ---

const (
	// wanted is whether the recipient's settings s (left joined) want the notification n emailed at all. A direct
	// message read before its email was due isn't emailed.
	wanted = `(coalesce(s.email <> 'off' and case n.kind when 'mention' then s.mentions when 'assigned' then s.assignments
		when 'status' then s.status when 'comment' then s.comments when 'chat_mention' then s.mentions and s.chat
		else s.chat end, true) and not (n.kind = 'message' and n.read_at is not null))`
	// settled is whether n is old enough to email: settle seconds ($1), or messageDelay ($2) for a direct message.
	settled = `n.created_at < now() - make_interval(secs => case n.kind when 'message' then $2::float8 else $1::float8 end)`
	msgCols = `n.id::text, n.kind, coalesce(a.name, 'Someone'), coalesce(p.key || '-' || i.number, ''), coalesce(i.title, ''),
		w.name, w.slug, coalesce(n.data #>> '{from,name}', ''), coalesce(n.data #>> '{to,name}', ''),
		coalesce(n.data ->> 'excerpt', ''), n.data ? 'comment_id', coalesce(c.id::text, ''), coalesce(c.kind, ''),
		coalesce(chat_name(c, n.user_id), '')`
	msgFrom = ` from notifications n left join issues i on i.id = n.issue_id left join projects p on p.id = i.project_id
		left join chats c on c.id = n.chat_id
		join workspaces w on w.id = n.workspace_id left join users a on a.id = n.actor_id
		left join notification_settings s on s.user_id = n.user_id`
)

// messageDelay is how long a direct message notification waits before it's emailed, so a chat read meanwhile isn't.
const messageDelay = 10 * time.Minute

func scanMessage(row pgx.Row, extra ...any) (string, message, error) {
	var id string
	var m message
	err := row.Scan(append([]any{&id, &m.kind, &m.actor, &m.identifier, &m.title, &m.workspace, &m.slug, &m.from, &m.to,
		&m.excerpt, &m.onComment, &m.chatID, &m.chatKind, &m.chatName}, extra...)...)
	return id, m, err
}

// skipUnwanted marks the settled notifications nobody wants emailed as done.
func (m *model) skipUnwanted(ctx context.Context, settle time.Duration) error {
	_, err := m.db.Exec(ctx,
		`update notifications t set emailed_at = now()
		 from notifications n left join notification_settings s on s.user_id = n.user_id
		 where t.id = n.id and n.emailed_at is null and `+settled+` and not `+wanted, settle.Seconds(), messageDelay.Seconds())
	if err != nil {
		return fmt.Errorf("skip unwanted emails: %w", err)
	}
	return nil
}

// queueNext locks the oldest settled notification due an instant email, queues it with queue in the same transaction
// and marks it emailed. It returns false when there's none. Rows another instance holds are skipped, so each email is
// queued once.
func (m *model) queueNext(ctx context.Context, settle time.Duration, queue func(tx pgx.Tx, to string, msg message) error) (bool, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var to string
		var msg message
		var err error
		id, msg, err = scanMessage(tx.QueryRow(ctx,
			`select `+msgCols+`, u.email`+msgFrom+` join users u on u.id = n.user_id
			 where n.emailed_at is null and `+settled+`
			   and coalesce(s.email, 'instant') = 'instant' and `+wanted+`
			 order by n.created_at limit 1
			 for update of n skip locked`, settle.Seconds(), messageDelay.Seconds()), &to)
		if errors.Is(err, pgx.ErrNoRows) {
			id = ""
			return nil
		}
		if err != nil {
			return err
		}
		if err := queue(tx, to, msg); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update notifications set emailed_at = now() where id = $1`, id)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("queue notification email: %w", err)
	}
	return id != "", nil
}

// digestNext locks a digest user whose digest for today (at hour in their timezone) is due, queues their unemailed wanted
// notifications with queue in the same transaction (nothing when there are none), marks them emailed and records the
// digest. It returns false when nobody is due.
func (m *model) digestNext(ctx context.Context, hour int, queue func(tx pgx.Tx, to string, msgs []message) error) (bool, error) {
	var userID string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var to string
		err := tx.QueryRow(ctx,
			`select s.user_id::text, u.email from notification_settings s join users u on u.id = s.user_id
			 cross join lateral (select coalesce(s.timezone, 'UTC') tz) z
			 cross join lateral (select (date_trunc('day', now() at time zone z.tz) + make_interval(hours => $1))
			   at time zone z.tz) d(at)
			 where s.email = 'digest' and now() >= d.at and (s.last_digest_at is null or s.last_digest_at < d.at)
			 limit 1 for update of s skip locked`, hour).Scan(&userID, &to)
		if errors.Is(err, pgx.ErrNoRows) {
			userID = ""
			return nil
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `select `+msgCols+msgFrom+`
			where n.user_id = $1 and n.emailed_at is null and `+wanted+`
			order by w.name, w.id, p.key nulls first, i.number, n.id`, userID)
		if err != nil {
			return err
		}
		type row struct {
			id  string
			msg message
		}
		rs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
			id, msg, err := scanMessage(r)
			return row{id, msg}, err
		})
		if err != nil {
			return err
		}
		if len(rs) > 0 {
			ids, msgs := make([]string, len(rs)), make([]message, len(rs))
			for i, r := range rs {
				ids[i], msgs[i] = r.id, r.msg
			}
			if err := queue(tx, to, msgs); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `update notifications set emailed_at = now() where id = any($1::uuid[])`, ids); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `update notification_settings set last_digest_at = now() where user_id = $1`, userID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("queue digest: %w", err)
	}
	return userID != "", nil
}
