# Chat plan

Scope: the Inbox becomes the workspace's chat. Members talk in a **workspace chat** everyone is in, a **personal chat** (notes to yourself), **direct messages** (1:1) and **private groups**. Messages are rich text with `@person` and `#issue` mentions, file and image attachments, emoji reactions, threads (one level of replies), editing and deleting your own messages, typing indicators and unread counts. Everything is live over the existing realtime socket. Today's notifications stay as the **Activity** entry at the top of the chat list (unchanged list + issue preview). Chat @mentions and unread direct messages become notifications, so they show in Activity and are emailed by the existing notification settings. No new dependencies.

Out of scope: public channels and browsing/joining them, message search, pinning, link previews, read receipts per person, voice/video, editing other people's messages, moderating (deleting others' messages), message retention limits, per-chat mute, chat across workspaces, push notifications.

## Concepts

| Kind | Who's in it | Created | Name shown |
|---|---|---|---|
| `workspace` | every member of the workspace (implicit, no member rows needed for access) | one per workspace, automatically | the workspace's name |
| `self` | just you | one per user per workspace, automatically | "Notes" (subtitle "Only you can see this") |
| `direct` | exactly two members | on first message / "New message" | the other person's name |
| `group` | 2–50 members, private | by any member | its name (1–80 chars) |

Access: you must be a member of the workspace, and for `self`/`direct`/`group` also a row in `chat_members`. A chat you can't access answers 404 `{"error":"Chat not found."}`. Removing someone from the workspace removes their access everywhere (the workspace check), so their `chat_members` rows can stay.

## Schema

Migration `backend/user/internal/db/migrations/027_chat.sql`:

```sql
create table chats (
  id              uuid primary key default uuidv7(),
  workspace_id    uuid not null references workspaces on delete cascade,
  kind            text not null check (kind in ('workspace', 'self', 'direct', 'group')),
  name            text,                    -- groups only
  pair            text,                    -- direct: '<smaller user id>:<larger user id>'; self: '<user id>'
  created_by      uuid references users on delete set null,
  last_message_at timestamptz,
  created_at      timestamptz not null default now()
);
create unique index on chats (workspace_id) where kind = 'workspace';
create unique index on chats (workspace_id, pair) where kind in ('direct', 'self');

create table chat_members (
  chat_id      uuid not null references chats on delete cascade,
  user_id      uuid not null references users on delete cascade,
  last_read_id uuid,                       -- newest message (top level or reply) the user has read; null = none
  joined_at    timestamptz not null default now(),
  primary key (chat_id, user_id)
);
create index on chat_members (user_id);

create table messages (
  id            uuid primary key default uuidv7(),
  chat_id       uuid not null references chats on delete cascade,
  parent_id     uuid references messages on delete cascade,   -- thread root; replies can't have replies
  author_id     uuid references users on delete set null,
  body          jsonb,                     -- ProseMirror doc; null once deleted
  created_at    timestamptz not null default now(),
  edited_at     timestamptz,
  deleted_at    timestamptz,
  reply_count   int not null default 0,
  last_reply_at timestamptz
);
create index on messages (chat_id, id) where parent_id is null;
create index on messages (parent_id, id) where parent_id is not null;

create table message_reactions (
  message_id uuid not null references messages on delete cascade,
  user_id    uuid not null references users on delete cascade,
  emoji      text not null check (char_length(emoji) between 1 and 16),
  created_at timestamptz not null default now(),
  primary key (message_id, user_id, emoji)
);

-- notifications: chat kinds; issue becomes optional
alter table notifications alter column issue_id drop not null;
alter table notifications add column chat_id uuid references chats on delete cascade;
alter table notifications add column message_id uuid references messages on delete cascade;
alter table notifications drop constraint notifications_kind_check;   -- use the real constraint name
alter table notifications add constraint notifications_kind_check
  check (kind in ('mention', 'assigned', 'status', 'comment', 'chat_mention', 'message'));
alter table notifications add constraint notifications_target_check
  check ((issue_id is not null) <> (chat_id is not null));
alter table notification_settings add column chat boolean not null default true;
```

Backfill in the same migration: a `workspace` chat for every workspace. `self` chats and the workspace chat for new workspaces are created **on demand** by `GET …/chats` (idempotent `insert … on conflict do nothing`), so workspace creation and invites don't change.

`chat_members.last_read_id` is the read marker; for the workspace chat a member's row is inserted on their first read (no row = nothing read, but messages created before they joined the workspace don't count as unread: compare with `workspace_members.created_at`, or whatever that table records).

## API contract

Conventions as before: FormData in, JSON out, 401 `Not signed in.`, 404 `{"error":"Workspace not found."}` for non-members, `{"error"}` / `{"errors":{field}}`. All under `/api/workspaces/{slug}`.

**Person** = `{"id","name"}` (avatars come from `/api/users/{id}/avatar` as everywhere).

**Chat JSON**
```json
{"id","kind":"workspace|self|direct|group","name":"Acme"|"Notes"|"Maya Member"|"Design team",
 "members":[person…],            // direct: both; group: all; self: you; workspace: [] (it's everyone)
 "created_by":"<user id>"|null,
 "unread":3,                     // top-level messages + replies by others after your last_read_id
 "mentioned":true,               // an unread message mentions you
 "last_message":{"author":person|null,"excerpt":"first 120 chars of plain text","created_at"}|null,
 "last_message_at":"…"|null}
```

**Message JSON**
```json
{"id","chat_id","parent_id":null|"…","author":person|null,
 "body":doc|null,                // null when deleted
 "deleted":false,"created_at","edited_at":null|"…",
 "reply_count":0,"last_reply_at":null|"…","repliers":[person…],   // up to 3 most recent distinct repliers
 "reactions":[{"emoji":"👍","count":2,"mine":true,"people":["Maya Member","You"]}],
 "can_edit":true,"can_delete":true}
```
`can_edit` / `can_delete`: you're the author and it isn't deleted.

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `GET /chats` | — | `200 [chat…]`: the workspace chat, your `self` chat, then your groups and direct chats by `last_message_at` desc (never-used ones last, by name). Creates the workspace and `self` chats if missing. | |
| `GET /chats/unread-count` | — | `200 {"count": n}`: unread messages in your direct chats and groups, plus unread messages that mention you in the workspace chat (the inbox badge adds notifications' unread count) | |
| `POST /chats` (new group) | `name`, `member_ids` (repeated; workspace members; you're added automatically) | `201 chat` | 400 `{"errors":{"name":"Name the group."}}` / `"Keep the name under 80 characters."`; `{"errors":{"member_ids":"Add at least one person."}}` / `"Groups can have up to 50 people."` / `"Someone you picked isn't in this workspace."` |
| `POST /chats/direct` | `user_id` (a workspace member, not you) | `200 chat` (get or create) | 400 `{"errors":{"user_id":"Choose someone in this workspace."}}` |
| `GET /chats/{id}` | — | `200 chat` | 404 |
| `POST /chats/{id}` (group only) | `name` | `200 chat` | 400 as above; 403 `{"error":"Only groups can be renamed."}` |
| `POST /chats/{id}/members` (group only) | `user_ids` (repeated) | `200 chat` | 400 as above; 403 `{"error":"Only groups can have people added."}` |
| `DELETE /chats/{id}/members/{userId}` (group only) | — | `204`. Yourself = leave (any member). Someone else = only the group's creator. The last member leaving deletes the group. | 403 `{"error":"Only the group's creator can remove people."}` |
| `GET /chats/{id}/messages?before={message id}` | — | `200 {"messages":[msg…],"has_more":bool}`: top-level messages, the 50 newest before `before` (exclusive; none = newest), **oldest first** | 400 `{"error":"Invalid cursor."}` |
| `POST /chats/{id}/messages` | `body` (doc JSON), optional `parent_id` (a top-level message in this chat) | `201 msg` | 400 `{"errors":{"body":"Write a message."}}` (empty) / doc messages with "Message" (max 10,000 chars); `{"errors":{"parent_id":"You can only reply to a message in this chat."}}` |
| `GET /messages/{id}/replies?before={id}` | — | `200 {"root":msg,"replies":[msg…],"has_more"}`: 50 newest replies before `before`, oldest first | 404 `{"error":"Message not found."}` |
| `POST /messages/{id}` | `body` | `200 msg` (sets `edited_at`) | 400; 403 `{"error":"You can only edit your own messages."}`; 404 |
| `DELETE /messages/{id}` | — | `204`: soft delete (body null, reactions removed); a thread root keeps its replies | 403 `{"error":"You can only delete your own messages."}`; 404 |
| `POST /messages/{id}/reactions` | `emoji` | `200 msg` (adding the same emoji twice is a no-op) | 400 `{"errors":{"emoji":"Choose an emoji."}}` |
| `DELETE /messages/{id}/reactions?emoji=…` | — | `200 msg` | |
| `POST /chats/{id}/read` | optional `message_id` (default: the chat's newest message incl. replies) | `204`: moves `last_read_id` forward only, and marks your unread `chat_mention`/`message` notifications for messages up to it read | |

Message docs use the description rules (`checkDoc`), plus **issue mentions**: a `mention` node whose `attrs.mentionSuggestionChar` is `"#"` has `attrs.id` = an issue identifier (`ENG-12`) and `attrs.label` = the identifier. Allowed in every document (descriptions and comments too). Notification code only treats `@` mentions (`mentionSuggestionChar` missing or `"@"`) as people. Message mentions must be people who can see the chat; others are kept as plain text labels in the doc but notify no one.

Issue suggestions for `#` use the existing `GET /issues?q=` search.

### Typing (socket)

- Client → server text frame: `{"type":"typing","chat":"<chat id>","thread":"<root message id>"|null}`. The client sends it at most once every 3 s while the composer has focus and its content changes.
- The server checks access (cache the result per connection and chat for 60 s), then `pg_notify`s it to the chat's audience. It doesn't go to the sender's own connections.
- Server → client: `{"type":"typing","chat":"<id>","thread":null|"<id>","user":person}`. Clients show it for 5 s after the last one: "Maya is typing…", "Maya and Ravi are typing…", "Several people are typing…".

### Realtime events

| Change | kind | audience | paths |
|---|---|---|---|
| Message created / edited / deleted, reaction changed | `chat.message` | the workspace chat: workspace; other chats: each member (`ToUser`) | `{ws}/chats`, `{ws}/chats/unread-count`, `{ws}/chats/{id}/messages`, and for replies or a root `{ws}/messages/{root}/replies` |
| Chat created, renamed, members changed | `chat.changed` | every member before and after the change | `{ws}/chats`, `{ws}/chats/{id}` |
| Read marker moved | `chat.read` | the reader | `{ws}/chats`, `{ws}/chats/unread-count` |
| New chat notification | `notifications.changed` (existing) | recipient | as today |

The client throttles `{ws}/chats` and `{ws}/chats/unread-count` to one refetch per second (add them to `realtime.ts` alongside the list throttle, but with 1 s).

### Notifications from chat

- **`chat_mention`**: a new message (or an edit adding the mention) that `@`mentions someone who can see the chat, not the author. `data`: `{"excerpt"}` (first 300 chars); `chat_id`, `message_id` set.
- **`message`**: a new message in a `direct` chat to the other person. At most one **unread** `message` notification per (recipient, chat): a newer message updates its `message_id`, `data.excerpt` and `created_at` instead of inserting.
- Email: the existing worker. `chat_mention` follows the "Mentions" toggle and the new `chat` toggle must also be on; `message` follows the new `chat` toggle and waits **10 minutes** instead of 30 s, and is marked `emailed_at` without sending if it was read by then. Subjects: "Maya mentioned you in Design team" / "Maya sent you a message"; body has the excerpt and a link to `{app_base_url}/{slug}/inbox/c/{chat id}`. Digests list them as "Maya: excerpt" under the workspace.
- Notification JSON gains `"chat": {"id","kind","name"}|null`, and `issue` is `null` for chat kinds. `GET/POST /api/me/notification-settings` gains boolean `chat`.
- Reading a chat (`POST /chats/{id}/read`) marks its notifications up to that message read.

## Backend

- New package `internal/chat` (routes / controller / model, `validate.go`). Reuses the issue package's doc check and mention extraction: export them (e.g. `issue.CheckDoc`, `issue.Mentions`) or move them to a small `internal/doc` package, backend agent's call; one source of truth.
- `notify.Record` grows the two chat kinds (or a sibling function) so all inserts and the collapse live in `notify`.
- Typing: a new socket frame type in `internal/realtime` with a narrow hook, e.g. `realtime.Handler` gets a `ChatAudience func(ctx, userID, workspaceSlug, chatID) (audience []string, err error)` provided by `chat` in `main.go`.
- Tests (integration, `TEST_DATABASE_URL`): access rules per kind (non-member 404, removed-from-workspace 404), direct get-or-create is unique for both orders, group create/rename/add/leave/remove-by-creator, last member leaving deletes, message paging oldest-first with `before`, thread replies + `reply_count`, edit/delete own only, soft delete keeps replies, reactions add/remove/count/mine, unread counts and `mentioned`, read marker only moves forward, `chat_mention` and `message` notifications incl. collapse and mark-read on read, email delay/skip for `message`, issue mention nodes accepted and not treated as people.
- Verify with curl and a socket check for `typing` and the events.

## Frontend (user app)

### Routes

- `routes/_app/$workspace/_main/inbox.tsx` becomes the chat layout: chat list on the left, `<Outlet />` on the right.
- `inbox/index.tsx`: redirects to the workspace chat (`/inbox/c/{id}`), the default.
- `inbox/activity.tsx` + `inbox/activity/$identifier.tsx`: today's grouped notification list and issue preview, moved here unchanged (links, J/K, Esc, "+N earlier updates"). Chat notifications in the list open their chat (`/inbox/c/{chat id}`, scrolled to the message).
- `inbox/c/$chatId.tsx`: one chat. `validateSearch`: `thread` (root message id) opens the thread panel; `message` (id) scrolls to and highlights a message.
- The navbar bell and Home's Inbox block link to `/inbox/activity`. The sidebar "Inbox" badge = notifications unread + `chats/unread-count`.

### Layout

- **≥ 1024px**: page height fills the window (no page scroll, like Home): chat list column (260px) | chat (flex) | thread panel (360px, only when `thread` is set).
- **< 1024px**: one column. `/inbox` shows the chat list (`inbox/index.tsx` redirects to the workspace chat only at ≥ 1024px); an open chat replaces the list and its header has a "← Chats" link; the thread replaces the chat with a "← Back to chat" link.

### Chat list

- "Activity" (bell icon, unread notification count).
- Workspace chat (workspace logo/initial, name), "Notes" (your self chat).
- **Groups** heading with a "+" icon button ("New group"): `Dialog` "New group" with Name `Input` and a member picker (`SmartAutocomplete` multi-select of workspace members), "Create group".
- **Direct messages** heading with "+" ("New message"): `Dialog` with a member picker; picking someone calls `POST /chats/direct` and opens it.
- Each row: avatar (person / group initials), name, last message excerpt (one line, secondary), relative time, unread count pill (pink when `mentioned`, otherwise secondary). Unread rows' names are 590 weight. Active row highlighted like the sidebar.

### Chat view

- **Header**: name; for groups the member avatars (up to 5, "+N") and a "⋯" `Popover`: "Rename group", "Add people", "Leave group" (confirm `Dialog`), and for the creator "Remove" next to each person in an "People" dialog. Direct: the person's name. Notes: "Only you can see this".
- **Messages**, newest at the bottom, the list scrolls (not the page):
  - Day dividers ("Today", "Yesterday", "Saturday, October 31").
  - Consecutive messages by the same author within 5 minutes collapse (no repeated avatar/name; time on hover).
  - Each message: avatar, name, time (full date in `title`), body via read-only `RichText`, "(edited)", reactions row (emoji + count pills, `aria-pressed` when mine, `title` = people; click toggles), "N replies · last reply 5m ago" with replier avatars → opens the thread.
  - Hover/focus toolbar (always visible on touch via a "⋯" button): add reaction (small emoji picker: 👍 ❤️ 😂 🎉 😮 😢 👀 ✅ — a fixed set, no library), "Reply in thread", and for your own: "Edit" (swaps in an editor, Enter saves, Esc cancels) and "Delete" (confirm `Dialog` "Delete message?").
  - Deleted: "This message was deleted." in secondary text (keeps the thread link).
  - Loading older: an `IntersectionObserver` at the top loads `before=` the oldest id, keeping the scroll position. Opening a chat scrolls to the first unread message ("New" divider) or the bottom. New messages auto-scroll only when already at the bottom; otherwise a "New messages ↓" pill.
  - `@` mentions render like elsewhere; `#ENG-12` mentions link to the issue page (`/issue/ENG-12`).
- **Composer** (bottom): `RichEditor` with `@` (chat members; everyone for the workspace chat; nothing for Notes) and `#` (issue search) suggestions, attachments (`upload` = `uploadFile`), placeholder "Message {name}" / "Write a note". Enter sends, Shift+Enter new line, ↑ in an empty composer edits your last message. Disabled while sending; clears on success; errors under it.
- **Typing line** above the composer.
- **Read marker**: when the chat is open and the window focused, `POST /chats/{id}/read` (debounced 1 s) with the newest visible message.

### Thread panel

Root message, its replies (same rendering), its own composer ("Reply…"), typing for that thread. "Close" (×) clears `thread`.

### Shared editor change

`@tether/shared` `RichEditor` gains an optional `issues?: (q: string) => Promise<{ id: string; label: string; title: string }[]>` prop: a second Mention suggestion with char `#`, rendering suggestions as "ENG-12 Fix login". `RichText` renders `#` mentions as a link (`href` built by a new optional `issueHref?: (identifier: string) => string` prop; without it, plain text). Enter-to-send: optional `onSubmit` prop; when set, Enter (without Shift, outside lists/code blocks and when no suggestion is open) calls it instead of a new line.

### Palette and shortcuts

- Palette: "Go to Activity", "Go to {chat name}" for each chat, "New message", "New group".
- `Ctrl/⌘+K` stays; in a chat, `Esc` closes the thread, then blurs the composer.

## Verification

- Backend: `go vet`, `go test -race` with `TEST_DATABASE_URL`, curl every endpoint incl. 404s for outsiders, socket check for events and typing.
- Frontend: `npm run build`, `npm run check` (add a `chat.check.ts` for grouping consecutive messages, day dividers and the typing text).
- Integration (headless Chrome, demo and Maya, also 820px and 390px):
  - Both see the workspace chat and their own Notes; demo's Notes never appear for Maya (404 on the URL).
  - Demo messages Maya directly: Maya's chat list shows it live with an unread pill and the inbox badge counts it; opening it clears both.
  - Typing shows on the other side and disappears.
  - Group: demo creates "Design team" with Maya, renames it, Maya leaves.
  - `@Maya` in the workspace chat → pink pill for Maya, a `chat_mention` row in Activity, email in Mailpit; `#ENG-1` renders as a link.
  - Thread reply shows "1 reply" under the root on both sides; reactions toggle and count live; edit shows "(edited)"; delete shows "This message was deleted."
  - Image attachment shows inline.
  - Activity still works as before (grouped rows, preview, J/K).
- `frontend/e2e/user.mjs`: a chat check (direct message live + unread clears, thread reply, reaction).

## As built

- Thread panel sits beside the chat from 1440px; from 1024 to 1439 it replaces the chat ("← Back to chat"). Activity's list and preview sit side by side from 1440px too.
- `GET /notifications/unread-count` leaves out the chat kinds; the inbox badge (sidebar and bell) is that plus `chats/unread-count`, so an unread DM counts once.
- Add-people errors use the field `user_ids`; removing from a non-group is 403 "Only groups can have people removed."; chat notification `data` also has `message_id` (Activity opens `?message=`).
- Doc rules moved to `internal/doc` (one source for issues and chat); `#` mentions never notify. A mention of someone who can't see the chat is stored as plain "@Name".
- Deleting a message deletes its notifications. Deleted messages without replies are hidden; with replies they show "This message was deleted."
- The "New" divider is placed from the unread count; older pages don't refresh live (both `ponytail:`).
- Activity was removed after launch: the chat list has no Activity entry and `/inbox/activity` is gone. Notifications live in a popover on the navbar bell (the 10 newest, unread dots, "Mark all read"; a row marks itself read and opens its issue or chat). The bell counts every unread notification (chat kinds included again); the sidebar's Inbox badge counts unread chat messages only. Home's block is called "Notifications".
- `DELETE /chats/{id}` deletes a whole chat with its messages, reactions and notifications for everyone in it: a direct chat by either person, Notes by their owner (they come back empty), a group by its creator (403 "Only the group's creator can delete it."); the workspace chat can't be (403 "The workspace chat can't be deleted."). Publishes `chat.changed` to its members. In the chat header's ⋯ menu as "Delete chat" / "Delete notes" / "Delete group", with a confirm dialog.
