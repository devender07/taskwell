package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/notify"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

// model owns every SQL query for chats and messages.
type model struct{ db *pgxpool.Pool }

var (
	errNotMember = errors.New("not a workspace member")
	errTooMany   = errors.New("too many group members")
	errBadParent = errors.New("parent is not a top-level message in the chat")
)

// scope is who's asking in which workspace.
type scope struct{ userID, wsID, slug string }

// ref is a chat the caller can open.
type ref struct{ id, kind, createdBy string }

// msgRef is a message the caller can see, and its chat.
type msgRef struct {
	id, parentID string
	mine         bool // the caller wrote it
	deleted      bool
	pinnedBy     string // who pinned it while the pin lasts; "" when unpinned or they're gone
	pinned       bool
	chat         ref
}

// root is the thread the message belongs to: its parent, or itself when it's top level.
func (m msgRef) root() string {
	if m.parentID != "" {
		return m.parentID
	}
	return m.id
}

// access is whether the user (a workspace member) may open chat c: everyone but guests in the workspace chat, members
// elsewhere.
// The workspace chat goes by role alone: a member's read marker there (chat_members) outlives a change to Guest.
const access = `(c.kind = 'workspace' and not is_guest(c.workspace_id, $1)
	or c.kind <> 'workspace' and exists (select 1 from chat_members a where a.chat_id = c.id and a.user_id = $1))`

// --- chats ---

type Chat struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Name          string          `json:"name"`
	Members       json.RawMessage `json:"members"`
	CreatedBy     *string         `json:"created_by"`
	Unread        int             `json:"unread"`
	Mentioned     bool            `json:"mentioned"`
	LastMessage   *LastMessage    `json:"last_message"`
	LastMessageAt *time.Time      `json:"last_message_at"`
}

type LastMessage struct {
	Author    json.RawMessage `json:"author"`
	Excerpt   string          `json:"excerpt"`
	CreatedAt time.Time       `json:"created_at"`
}

// unread matches the messages m in chat c that the user $1 hasn't read: by others, not deleted, after their read
// marker r and after they joined the workspace (wm).
const unread = `m.chat_id = c.id and m.id > coalesce(r.last_read_id, '00000000-0000-0000-0000-000000000000')
	and m.deleted_at is null and m.author_id is distinct from $1 and m.created_at > wm.created_at`

// chatQuery selects the user $1's chats in workspace $2 (only chat $3 unless it's null), in list order.
const chatQuery = `
	select c.id::text, c.kind, chat_name(c, $1),
	  case when c.kind = 'workspace' then '[]'::json else coalesce((
	    select json_agg(json_build_object('id', u.id, 'name', u.name) order by lower(u.name), u.id)
	    from chat_members o join users u on u.id = o.user_id
	    where o.chat_id = c.id and (c.kind = 'direct' or exists (
	      select 1 from workspace_members x where x.workspace_id = c.workspace_id and x.user_id = o.user_id))), '[]') end,
	  c.created_by::text,
	  (select count(*) from messages m where ` + unread + `),
	  exists (select 1 from messages m where ` + unread + ` and $1 = any(m.mentions)),
	  lm.author, lm.body, lm.created_at, c.last_message_at
	from chats c
	join workspace_members wm on wm.workspace_id = c.workspace_id and wm.user_id = $1
	left join chat_members r on r.chat_id = c.id and r.user_id = $1
	left join lateral (
	  select (select json_build_object('id', u.id, 'name', u.name) from users u where u.id = m.author_id) author, m.body, m.created_at
	  from messages m where m.chat_id = c.id and m.parent_id is null and m.deleted_at is null order by m.id desc limit 1) lm on true
	where c.workspace_id = $2 and (c.kind = 'workspace' and not is_guest(c.workspace_id, $1) or c.kind <> 'workspace' and r.user_id is not null)
	  and ($3::uuid is null or c.id = $3)
	order by c.kind = 'workspace' desc, c.last_message_at desc nulls last, lower(chat_name(c, $1)), c.id`

func (m *model) queryChats(ctx context.Context, userID, wsID string, chatID *string) ([]Chat, error) {
	rows, err := m.db.Query(ctx, chatQuery, userID, wsID, chatID)
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	cs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Chat, error) {
		var c Chat
		var author, body json.RawMessage
		var at *time.Time
		if err := row.Scan(&c.ID, &c.Kind, &c.Name, &c.Members, &c.CreatedBy, &c.Unread, &c.Mentioned, &author, &body, &at,
			&c.LastMessageAt); err != nil {
			return c, err
		}
		if at != nil {
			c.LastMessage = &LastMessage{Author: author, Excerpt: excerpt(body), CreatedAt: at.UTC()}
		}
		if c.LastMessageAt != nil {
			t := c.LastMessageAt.UTC()
			c.LastMessageAt = &t
		}
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}
	return cs, nil
}

// excerpt is a message's first 120 characters of plain text, on one line.
func excerpt(body []byte) string {
	text := strings.Join(strings.Fields(doc.PlainText(body)), " ")
	if r := []rune(text); len(r) > 120 {
		text = string(r[:120]) + "…"
	}
	return text
}

// list returns the user's chats in the workspace, first creating the workspace chat if missing.
func (m *model) list(ctx context.Context, userID, wsID string) ([]Chat, error) {
	if _, err := m.db.Exec(ctx, `insert into chats (workspace_id, kind) values ($1, 'workspace') on conflict do nothing`, wsID); err != nil {
		return nil, fmt.Errorf("add workspace chat: %w", err)
	}
	return m.queryChats(ctx, userID, wsID, nil)
}

// get returns the chat as the user sees it, or nil when they can't open it.
func (m *model) get(ctx context.Context, userID, wsID, chatID string) (*Chat, error) {
	cs, err := m.queryChats(ctx, userID, wsID, &chatID)
	if err != nil || len(cs) == 0 {
		return nil, err
	}
	return &cs[0], nil
}

// unreadCount is the user's unread messages in their direct chats and groups, plus the unread messages that mention
// them in the workspace chat.
func (m *model) unreadCount(ctx context.Context, userID, wsID string) (int, error) {
	var n int
	err := m.db.QueryRow(ctx,
		`select count(*) from chats c
		 join workspace_members wm on wm.workspace_id = c.workspace_id and wm.user_id = $1
		 left join chat_members r on r.chat_id = c.id and r.user_id = $1
		 join messages m on `+unread+`
		 where c.workspace_id = $2 and (c.kind = 'workspace' and $1 = any(m.mentions) and not is_guest($2, $1)
		   or c.kind in ('direct', 'group') and r.user_id is not null)`, userID, wsID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unread messages: %w", err)
	}
	return n, nil
}

// chat returns the chat when the user can open it, else nil.
func (m *model) chat(ctx context.Context, userID, wsID, chatID string) (*ref, error) {
	var r ref
	err := m.db.QueryRow(ctx,
		`select c.id::text, c.kind, coalesce(c.created_by::text, '') from chats c
		 where c.id = $2 and c.workspace_id = $3 and `+access, userID, chatID, wsID).Scan(&r.id, &r.kind, &r.createdBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load chat: %w", err)
	}
	return &r, nil
}

// chatAudience returns who sees the chat as realtime audiences, or nil when the user can't open it (including when
// they aren't in its workspace).
func (m *model) chatAudience(ctx context.Context, userID, chatID string) ([]string, error) {
	var r ref
	var wsID string
	err := m.db.QueryRow(ctx,
		`select c.id::text, c.kind, c.workspace_id::text from chats c
		 join workspace_members wm on wm.workspace_id = c.workspace_id and wm.user_id = $1
		 where c.id = $2 and `+access, userID, chatID).Scan(&r.id, &r.kind, &wsID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load chat: %w", err)
	}
	return audience(ctx, m.db, wsID, r)
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// members returns the chat's members who are in its workspace.
func members(ctx context.Context, q querier, wsID, chatID string) ([]string, error) {
	rows, err := q.Query(ctx,
		`select cm.user_id::text from chat_members cm
		 join workspace_members wm on wm.workspace_id = $1 and wm.user_id = cm.user_id where cm.chat_id = $2`, wsID, chatID)
	if err != nil {
		return nil, fmt.Errorf("list chat members: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list chat members: %w", err)
	}
	return ids, nil
}

// audience is who sees the chat's changes: the workspace for the workspace chat (each non-guest when it has guests),
// else each member.
func audience(ctx context.Context, q querier, wsID string, c ref) ([]string, error) {
	var ids []string
	var err error
	if c.kind == "workspace" {
		// ponytail: one NOTIFY per member once a workspace has a guest; add a non-guest audience to the hub if that's big.
		rows, err := q.Query(ctx, `select m.user_id::text from workspace_members m join workspace_roles r on r.id = m.role_id
			where m.workspace_id = $1 and r.builtin is distinct from 'guest'
			  and exists (select 1 from workspace_roles g join workspace_members gm on gm.role_id = g.id
			              where g.workspace_id = $1 and g.builtin = 'guest')`, wsID)
		if err != nil {
			return nil, err
		}
		if ids, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || len(ids) == 0 {
			return []string{realtime.ToWorkspace(wsID)}, err
		}
	} else if ids, err = members(ctx, q, wsID, c.id); err != nil {
		return nil, err
	}
	aud := make([]string, len(ids))
	for i, id := range ids {
		aud[i] = realtime.ToUser(id)
	}
	return aud, nil
}

// publish sends the event to each of the audiences.
func publish(ctx context.Context, tx pgx.Tx, e realtime.Event, aud []string) error {
	for _, a := range slices.Compact(slices.Sorted(slices.Values(aud))) {
		e.Audience = a
		if err := realtime.Publish(ctx, tx, e); err != nil {
			return err
		}
	}
	return nil
}

func chatChanged(sc scope, chatID string) realtime.Event {
	ws := realtime.Workspace(sc.slug)
	return realtime.Event{Kind: "chat.changed", Workspace: sc.slug, Actor: sc.userID, Paths: []string{ws + "/chats", ws + "/chats/" + chatID}}
}

// changeMembers runs change on the chat in a transaction and tells everyone who was or is a member.
func (m *model) changeMembers(ctx context.Context, sc scope, chatID string, change func(tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		before, err := members(ctx, tx, sc.wsID, chatID)
		if err != nil {
			return err
		}
		if err := change(tx); err != nil {
			return err
		}
		after, err := members(ctx, tx, sc.wsID, chatID)
		if err != nil {
			return err
		}
		var aud []string
		for _, id := range slices.Concat(before, after) {
			aud = append(aud, realtime.ToUser(id))
		}
		return publish(ctx, tx, chatChanged(sc, chatID), aud)
	})
}

// checkMembers fails with errNotMember unless every id is a member of the workspace.
func checkMembers(ctx context.Context, tx pgx.Tx, wsID string, ids []string) error {
	var n int
	if err := tx.QueryRow(ctx, `select count(*) from workspace_members where workspace_id = $1 and user_id = any($2::uuid[])`,
		wsID, ids).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return errNotMember
	}
	return nil
}

// createGroup adds a group named name with the user and the workspace members ids (distinct, not the user), and
// returns its id.
func (m *model) createGroup(ctx context.Context, sc scope, name string, ids []string) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := checkMembers(ctx, tx, sc.wsID, ids); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `insert into chats (workspace_id, kind, name, created_by) values ($1, 'group', $2, $3) returning id::text`,
			sc.wsID, name, sc.userID).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into chat_members (chat_id, user_id) select $1, unnest($2::uuid[])`,
			id, append([]string{sc.userID}, ids...)); err != nil {
			return err
		}
		aud := []string{realtime.ToUser(sc.userID)}
		for _, u := range ids {
			aud = append(aud, realtime.ToUser(u))
		}
		return publish(ctx, tx, chatChanged(sc, id), aud)
	})
	if err != nil && !errors.Is(err, errNotMember) {
		return "", fmt.Errorf("create group: %w", err)
	}
	return id, err
}

// direct returns the direct chat between the user and other (a workspace member), creating it when there's none.
func (m *model) direct(ctx context.Context, sc scope, other string) (string, error) {
	pair := min(sc.userID, other) + ":" + max(sc.userID, other)
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := checkMembers(ctx, tx, sc.wsID, []string{other}); err != nil {
			return err
		}
		err := tx.QueryRow(ctx,
			`with c as (insert into chats (workspace_id, kind, pair, created_by) values ($1, 'direct', $2, $3)
			   on conflict do nothing returning id),
			 m as (insert into chat_members (chat_id, user_id) select c.id, u from c, unnest($4::uuid[]) u)
			 select id::text from c`, sc.wsID, pair, sc.userID, []string{sc.userID, other}).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) { // it exists; a new statement sees one committed meanwhile too
			return tx.QueryRow(ctx, `select id::text from chats where workspace_id = $1 and kind = 'direct' and pair = $2`,
				sc.wsID, pair).Scan(&id)
		}
		if err != nil {
			return err
		}
		return publish(ctx, tx, chatChanged(sc, id), []string{realtime.ToUser(sc.userID), realtime.ToUser(other)})
	})
	if err != nil && !errors.Is(err, errNotMember) {
		return "", fmt.Errorf("open direct chat: %w", err)
	}
	return id, err
}

func (m *model) rename(ctx context.Context, sc scope, chatID, name string) error {
	err := m.changeMembers(ctx, sc, chatID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update chats set name = $2 where id = $1`, chatID, name)
		return err
	})
	if err != nil {
		return fmt.Errorf("rename group: %w", err)
	}
	return nil
}

// addMembers adds the workspace members ids to the group, keeping it at maxMembers or fewer.
func (m *model) addMembers(ctx context.Context, sc scope, chatID string, ids []string) error {
	err := m.changeMembers(ctx, sc, chatID, func(tx pgx.Tx) error {
		if err := checkMembers(ctx, tx, sc.wsID, ids); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select 1 from chats where id = $1 for update`, chatID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into chat_members (chat_id, user_id) select $1, unnest($2::uuid[]) on conflict do nothing`,
			chatID, ids); err != nil {
			return err
		}
		n, err := members(ctx, tx, sc.wsID, chatID)
		if err != nil {
			return err
		}
		if len(n) > maxMembers {
			return errTooMany
		}
		return nil
	})
	if err != nil && !errors.Is(err, errNotMember) && !errors.Is(err, errTooMany) {
		return fmt.Errorf("add group members: %w", err)
	}
	return err
}

// removeMember takes the user out of the group, deleting the group when no member of the workspace is left.
func (m *model) removeMember(ctx context.Context, sc scope, chatID, userID string) error {
	err := m.changeMembers(ctx, sc, chatID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select 1 from chats where id = $1 for update`, chatID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from chat_members where chat_id = $1 and user_id = $2`, chatID, userID); err != nil {
			return err
		}
		left, err := members(ctx, tx, sc.wsID, chatID)
		if err != nil || len(left) > 0 {
			return err
		}
		_, err = tx.Exec(ctx, `delete from chats where id = $1`, chatID)
		return err
	})
	if err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	return nil
}

// deleteChat deletes the chat with its messages, reactions and notifications, telling everyone who was in it.
func (m *model) deleteChat(ctx context.Context, sc scope, chatID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		ids, err := members(ctx, tx, sc.wsID, chatID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from chats where id = $1`, chatID); err != nil {
			return err
		}
		ws := realtime.Workspace(sc.slug)
		e := chatChanged(sc, chatID)
		e.Paths = append(e.Paths, ws+"/chats/unread-count", ws+"/notifications", ws+"/notifications/unread-count")
		aud := make([]string, len(ids))
		for i, id := range ids {
			aud[i] = realtime.ToUser(id)
		}
		return publish(ctx, tx, e, aud)
	})
	if err != nil {
		return fmt.Errorf("delete chat: %w", err)
	}
	return nil
}

// --- messages ---

type Message struct {
	ID          string          `json:"id"`
	ChatID      string          `json:"chat_id"`
	ParentID    *string         `json:"parent_id"`
	Author      json.RawMessage `json:"author"`
	Body        json.RawMessage `json:"body"`
	Deleted     bool            `json:"deleted"`
	CreatedAt   time.Time       `json:"created_at"`
	EditedAt    *time.Time      `json:"edited_at"`
	ReplyCount  int             `json:"reply_count"`
	LastReplyAt *time.Time      `json:"last_reply_at"`
	Repliers    json.RawMessage `json:"repliers"`
	Reactions   json.RawMessage `json:"reactions"`
	CanEdit     bool            `json:"can_edit"`
	CanDelete   bool            `json:"can_delete"`
	PinnedUntil *time.Time      `json:"pinned_until"`
	PinnedBy    json.RawMessage `json:"pinned_by"`
}

// msgCols selects a Message m as the user $1 sees it.
const msgCols = `m.id::text, m.chat_id::text, m.parent_id::text,
	(select json_build_object('id', u.id, 'name', u.name) from users u where u.id = m.author_id),
	m.body, m.deleted_at is not null, m.created_at, m.edited_at, m.reply_count, m.last_reply_at,
	coalesce((select json_agg(json_build_object('id', p.id, 'name', p.name) order by p.last desc) from (
	  select u.id, u.name, max(x.created_at) last from messages x join users u on u.id = x.author_id
	  where x.parent_id = m.id and x.deleted_at is null group by u.id, u.name order by last desc limit 3) p), '[]'),
	coalesce((select json_agg(json_build_object('emoji', e.emoji, 'count', e.n, 'mine', e.mine, 'people', e.people) order by e.first)
	  from (select x.emoji, count(*) n, bool_or(x.user_id = $1) mine, min(x.created_at) first,
	          json_agg(case when x.user_id = $1 then 'You' else u.name end order by x.created_at, u.id) people
	        from message_reactions x join users u on u.id = x.user_id where x.message_id = m.id group by x.emoji) e), '[]'),
	coalesce(m.author_id = $1 and m.deleted_at is null, false),
	case when m.pinned_until > now() and m.deleted_at is null then m.pinned_until end,
	(select json_build_object('id', u.id, 'name', u.name) from users u where u.id = m.pinned_by and m.pinned_until > now())`

func scanMessage(row pgx.CollectableRow) (Message, error) {
	var msg Message
	err := row.Scan(&msg.ID, &msg.ChatID, &msg.ParentID, &msg.Author, &msg.Body, &msg.Deleted, &msg.CreatedAt, &msg.EditedAt,
		&msg.ReplyCount, &msg.LastReplyAt, &msg.Repliers, &msg.Reactions, &msg.CanEdit, &msg.PinnedUntil, &msg.PinnedBy)
	msg.CanDelete, msg.CreatedAt = msg.CanEdit, msg.CreatedAt.UTC()
	for _, t := range []**time.Time{&msg.EditedAt, &msg.LastReplyAt, &msg.PinnedUntil} {
		if *t != nil {
			u := (*t).UTC()
			*t = &u
		}
	}
	return msg, err
}

// pageSize is how many messages or replies a page holds.
const pageSize = 50

// page returns the newest pageSize messages matching where ($2 the chat or root, $3 the cursor) before the cursor,
// oldest first, and whether there are older ones.
func (m *model) page(ctx context.Context, userID, where, id, before string) ([]Message, bool, error) {
	rows, err := m.db.Query(ctx, `select `+msgCols+` from messages m where `+where+` and ($3 = '' or m.id < $3::uuid)
		order by m.id desc limit $4`, userID, id, before, pageSize+1)
	if err != nil {
		return nil, false, fmt.Errorf("list messages: %w", err)
	}
	ms, err := pgx.CollectRows(rows, scanMessage)
	if err != nil {
		return nil, false, fmt.Errorf("list messages: %w", err)
	}
	more := len(ms) > pageSize
	if more {
		ms = ms[:pageSize]
	}
	slices.Reverse(ms)
	if ms == nil {
		ms = []Message{}
	}
	return ms, more, nil
}

// messages returns a page of the chat's top-level messages.
func (m *model) messages(ctx context.Context, userID, chatID, before string) ([]Message, bool, error) {
	return m.page(ctx, userID, `m.chat_id = $2 and m.parent_id is null`, chatID, before)
}

// replies returns a page of the thread's replies.
func (m *model) replies(ctx context.Context, userID, rootID, before string) ([]Message, bool, error) {
	return m.page(ctx, userID, `m.parent_id = $2`, rootID, before)
}

// pinned returns the chat's pinned messages, newest first.
func (m *model) pinned(ctx context.Context, userID, chatID string) ([]Message, error) {
	rows, err := m.db.Query(ctx, `select `+msgCols+` from messages m
		where m.chat_id = $2 and m.pinned_until > now() and m.deleted_at is null order by m.id desc`, userID, chatID)
	if err != nil {
		return nil, fmt.Errorf("list pinned messages: %w", err)
	}
	ms, err := pgx.CollectRows(rows, scanMessage)
	if err != nil {
		return nil, fmt.Errorf("list pinned messages: %w", err)
	}
	if ms == nil {
		ms = []Message{}
	}
	return ms, nil
}

// message returns the message as the user sees it.
func (m *model) message(ctx context.Context, userID, id string) (*Message, error) {
	rows, err := m.db.Query(ctx, `select `+msgCols+` from messages m where m.id = $2`, userID, id)
	if err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	msg, err := pgx.CollectExactlyOneRow(rows, scanMessage)
	if err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	return &msg, nil
}

// messageRef returns the message when the user can see its chat, else nil.
func (m *model) messageRef(ctx context.Context, userID, wsID, id string) (*msgRef, error) {
	var r msgRef
	err := m.db.QueryRow(ctx,
		`select m.id::text, coalesce(m.parent_id::text, ''), coalesce(m.author_id = $1, false), m.deleted_at is not null,
		        coalesce(m.pinned_until > now(), false), case when m.pinned_until > now() then coalesce(m.pinned_by::text, '') else '' end,
		        c.id::text, c.kind, coalesce(c.created_by::text, '')
		 from messages m join chats c on c.id = m.chat_id
		 where m.id = $2 and c.workspace_id = $3 and `+access, userID, id, wsID).
		Scan(&r.id, &r.parentID, &r.mine, &r.deleted, &r.pinned, &r.pinnedBy, &r.chat.id, &r.chat.kind, &r.chat.createdBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	return &r, nil
}

// messageChanged tells the chat's audience its messages changed, and the thread's replies for root.
func messageChanged(ctx context.Context, tx pgx.Tx, sc scope, c ref, root string) error {
	ws := realtime.Workspace(sc.slug)
	e := realtime.Event{Kind: "chat.message", Workspace: sc.slug, Actor: sc.userID,
		Paths: []string{ws + "/chats", ws + "/chats/unread-count", ws + "/chats/" + c.id + "/messages", ws + "/messages/" + root + "/replies"}}
	aud, err := audience(ctx, tx, sc.wsID, c)
	if err != nil {
		return err
	}
	return publish(ctx, tx, e, aud)
}

// visible returns the mentioned people who can see the chat, keeping the doc's other person mentions as plain text.
func visible(ctx context.Context, tx pgx.Tx, wsID string, c ref, body json.RawMessage) ([]string, json.RawMessage, error) {
	ids := slices.DeleteFunc(doc.Mentions(body), func(id string) bool { return !web.IsUUID(id) })
	rows, err := tx.Query(ctx,
		`select u::text from unnest($1::uuid[]) u
		 where exists (select 1 from workspace_members wm where wm.workspace_id = $2 and wm.user_id = u)
		   and ($4 = 'workspace' and not is_guest($2, u) or $4 <> 'workspace' and exists (select 1 from chat_members cm where cm.chat_id = $3 and cm.user_id = u))`,
		ids, wsID, c.id, c.kind)
	if err != nil {
		return nil, nil, fmt.Errorf("check mentions: %w", err)
	}
	seen, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, fmt.Errorf("check mentions: %w", err)
	}
	return seen, doc.KeepMentions(body, func(id string) bool { return slices.Contains(seen, id) }), nil
}

// other is the other member of a direct chat; "" for other kinds.
func other(ctx context.Context, tx pgx.Tx, sc scope, c ref) (string, error) {
	if c.kind != "direct" {
		return "", nil
	}
	ids, err := members(ctx, tx, sc.wsID, c.id)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if id != sc.userID {
			return id, nil
		}
	}
	return "", nil
}

// createMessage adds the user's message to the chat, a reply when parentID isn't "", and returns its id. It notifies
// the people mentioned and, in a direct chat, the other person.
// partnerGone reports whether the other person in the direct chat has left the workspace.
func (m *model) partnerGone(ctx context.Context, chatID, userID string) (bool, error) {
	var gone bool
	err := m.db.QueryRow(ctx, `select exists (select 1 from chat_members o join chats c on c.id = o.chat_id
		where o.chat_id = $1 and o.user_id <> $2 and not exists (
		  select 1 from workspace_members x where x.workspace_id = c.workspace_id and x.user_id = o.user_id))`, chatID, userID).Scan(&gone)
	if err != nil {
		return false, fmt.Errorf("direct chat partner: %w", err)
	}
	return gone, nil
}

func (m *model) createMessage(ctx context.Context, sc scope, c ref, parentID string, body json.RawMessage) (string, error) {
	var id string
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if parentID != "" {
			tag, err := tx.Exec(ctx,
				`update messages set reply_count = reply_count + 1, last_reply_at = now()
				 where id = $1 and chat_id = $2 and parent_id is null`, parentID, c.id)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return errBadParent
			}
		}
		mentioned, body, err := visible(ctx, tx, sc.wsID, c, body)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`insert into messages (chat_id, parent_id, author_id, body, mentions) values ($1, nullif($2, '')::uuid, $3, $4, $5)
			 returning id::text`, c.id, parentID, sc.userID, body, mentioned).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update chats set last_message_at = now() where id = $1`, c.id); err != nil {
			return err
		}
		direct, err := other(ctx, tx, sc, c)
		if err != nil {
			return err
		}
		if err := notify.RecordChat(ctx, tx, notify.ChatChange{ChatID: c.id, MessageID: id, ActorID: sc.userID,
			Excerpt: notify.Excerpt(doc.PlainText(body)), Mentioned: mentioned, Direct: direct}); err != nil {
			return err
		}
		root := id
		if parentID != "" {
			root = parentID
		}
		return messageChanged(ctx, tx, sc, c, root)
	})
	if err != nil && !errors.Is(err, errBadParent) {
		return "", fmt.Errorf("add message: %w", err)
	}
	return id, err
}

// editMessage replaces the message's body, notifying the people it newly mentions.
func (m *model) editMessage(ctx context.Context, sc scope, r msgRef, body json.RawMessage) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var old []string
		if err := tx.QueryRow(ctx, `select mentions::text[] from messages where id = $1 for update`, r.id).Scan(&old); err != nil {
			return err
		}
		mentioned, body, err := visible(ctx, tx, sc.wsID, r.chat, body)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update messages set body = $2, mentions = $3, edited_at = now() where id = $1`,
			r.id, body, mentioned); err != nil {
			return err
		}
		added := slices.DeleteFunc(slices.Clone(mentioned), func(id string) bool { return slices.Contains(old, id) })
		if err := notify.RecordChat(ctx, tx, notify.ChatChange{ChatID: r.chat.id, MessageID: r.id, ActorID: sc.userID,
			Excerpt: notify.Excerpt(doc.PlainText(body)), Mentioned: added}); err != nil {
			return err
		}
		return messageChanged(ctx, tx, sc, r.chat, r.root())
	})
	if err != nil {
		return fmt.Errorf("edit message: %w", err)
	}
	return nil
}

// deleteMessage empties the message and drops its reactions and notifications; a thread root keeps its replies.
func (m *model) deleteMessage(ctx context.Context, sc scope, r msgRef) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update messages set body = null, mentions = '{}', deleted_at = now(), pinned_until = null where id = $1`, r.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from message_reactions where message_id = $1`, r.id); err != nil {
			return err
		}
		if r.parentID != "" {
			if _, err := tx.Exec(ctx, `update messages set reply_count = greatest(reply_count - 1, 0) where id = $1`, r.parentID); err != nil {
				return err
			}
		}
		if err := notify.ForgetMessage(ctx, tx, r.id, sc.slug, sc.userID); err != nil {
			return err
		}
		return messageChanged(ctx, tx, sc, r.chat, r.root())
	})
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	return nil
}

// pin pins the message for days, or unpins it when days is 0.
func (m *model) pin(ctx context.Context, sc scope, r msgRef, days int) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update messages set pinned_until = case when $2 > 0 then now() + make_interval(days => $2) end,
			pinned_by = case when $2 > 0 then $3::uuid end where id = $1`, r.id, days, sc.userID); err != nil {
			return err
		}
		return messageChanged(ctx, tx, sc, r.chat, r.root())
	})
	if err != nil {
		return fmt.Errorf("pin message: %w", err)
	}
	return nil
}

// react adds (or with add false removes) the user's emoji reaction on the message.
func (m *model) react(ctx context.Context, sc scope, r msgRef, emoji string, add bool) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		sql := `delete from message_reactions where message_id = $1 and user_id = $2 and emoji = $3`
		if add {
			sql = `insert into message_reactions (message_id, user_id, emoji) values ($1, $2, $3) on conflict do nothing`
		}
		tag, err := tx.Exec(ctx, sql, r.id, sc.userID, emoji)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return messageChanged(ctx, tx, sc, r.chat, r.root())
	})
	if err != nil {
		return fmt.Errorf("react: %w", err)
	}
	return nil
}

// read moves the user's read marker in the chat forward to the message (the chat's newest when messageID is "";
// one not in the chat moves nothing) and marks their chat notifications up to it read.
func (m *model) read(ctx context.Context, sc scope, chatID, messageID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var upTo string
		err := tx.QueryRow(ctx,
			`select id::text from messages where chat_id = $1 and ($2 = '' or id = $2::uuid) order by id desc limit 1`,
			chatID, messageID).Scan(&upTo)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`insert into chat_members as cm (chat_id, user_id, last_read_id) values ($1, $2, $3)
			 on conflict (chat_id, user_id) do update set last_read_id = excluded.last_read_id
			 where cm.last_read_id is null or cm.last_read_id < excluded.last_read_id`, chatID, sc.userID, upTo)
		if err != nil {
			return err
		}
		if err := notify.ReadChat(ctx, tx, sc.userID, chatID, sc.slug, upTo); err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		ws := realtime.Workspace(sc.slug)
		return realtime.Publish(ctx, tx, realtime.Event{Kind: "chat.read", Workspace: sc.slug, Actor: sc.userID,
			Audience: realtime.ToUser(sc.userID), Paths: []string{ws + "/chats", ws + "/chats/unread-count"}})
	})
	if err != nil {
		return fmt.Errorf("read chat: %w", err)
	}
	return nil
}
