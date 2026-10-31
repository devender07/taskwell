# Notifications plan (Phase 6)

Scope: people watch issues; changes to watched issues, assignments and mentions create notifications; an in-app inbox per workspace; email per notification or as a daily digest, with per-user settings; watch/unwatch on the issue page. Mention emails (today sent directly by `notify.go`) move onto this system. No new dependencies.

Out of scope: push/browser notifications, Slack, per-project or per-workspace email settings, snoozing, real-time inbox updates (the unread count polls).

## Schema

Migration `backend/user/internal/db/migrations/017_notifications.sql`:

```sql
create table issue_watchers (
  issue_id   uuid not null references issues on delete cascade,
  user_id    uuid not null references users on delete cascade,
  created_at timestamptz not null default now(),
  primary key (issue_id, user_id)
);
create index on issue_watchers (user_id);

create table notifications (
  id           uuid primary key default uuidv7(),
  user_id      uuid not null references users on delete cascade,      -- recipient
  workspace_id uuid not null references workspaces on delete cascade,
  issue_id     uuid not null references issues on delete cascade,
  actor_id     uuid references users on delete set null,
  kind         text not null check (kind in ('mention', 'assigned', 'status', 'comment')),
  data         jsonb not null default '{}',   -- status: {from,to}; comment/mention: {comment_id?, excerpt}
  read_at      timestamptz,
  emailed_at   timestamptz,                    -- set when sent (instant) or included in a digest
  created_at   timestamptz not null default now()
);
create index on notifications (user_id, workspace_id, id desc);
create index on notifications (emailed_at) where emailed_at is null;

create table notification_settings (
  user_id     uuid primary key references users on delete cascade,
  email       text not null default 'instant' check (email in ('instant', 'digest', 'off')),
  mentions    boolean not null default true,
  assignments boolean not null default true,
  status      boolean not null default true,
  comments    boolean not null default true,
  last_digest_at timestamptz
);
insert into system_config (key, value, description) values ('digest_hour_utc', '8', 'Hour (UTC) daily digests are sent') on conflict do nothing;
-- backfill: creators and assignees watch their existing issues
```

(Follow how earlier migrations seed `system_config`.) Missing settings row = defaults.

## Who gets what

Watching: the creator and the assignee (when assigned) start watching automatically; so do commenters and anyone mentioned. Anyone who can see the issue can watch/unwatch manually.

| Trigger (same transaction as the change) | Recipients (never the actor) | kind |
|---|---|---|
| Assignee set to someone | the new assignee | `assigned` |
| Status changed | watchers | `status` |
| Comment created | watchers, except people mentioned in it (they get `mention`) | `comment` |
| Newly mentioned in a description or comment (create/edit/live save) | the mentioned member | `mention` |

The in-app inbox always gets every notification; the per-kind toggles and email mode only control **email**. Collapse: if the recipient already has an **unread** `status` notification for the same issue from the last 10 minutes, update it (`data.to`, `created_at`) instead of inserting.

## Email delivery

A worker goroutine in the user API (started in `main.go`), every 60s:
1. **Instant**: `select … from notifications n join users … where emailed_at is null and created_at < now() - interval '30 seconds'` for recipients whose mode is `instant` and whose toggle for that kind is on, `for update of n skip locked` (safe with several API instances), send one email each, set `emailed_at`. Notifications that won't be emailed (mode off, kind toggled off) get `emailed_at = now()` without sending. The 30s delay lets the status collapse settle.
2. **Digest**: once per day after `digest_hour_utc` read in each user's `timezone` (migration 024; the key keeps its old name; null, never chosen, reads as UTC since migration 025, which also turned every existing `UTC` into null because a chosen UTC couldn't be told from the old default), for users with mode `digest` whose `last_digest_at` is before today's digest time: one email listing their not-yet-emailed notifications for enabled kinds (grouped by workspace and issue, max 50 lines + "and N more"), mark them `emailed_at`, set `last_digest_at`. Skip if nothing to send (still set `last_digest_at`). Locking: `select … from notification_settings … for update skip locked`.
3. Mail not configured → leave rows unsent and log once per run at debug level.

Email formats (plain text, sentence case subjects):
- `assigned`: "Demo assigned you ENG-12" / body "Demo assigned you ENG-12: title\n\nlink"
- `status`: "ENG-12 moved to In progress" / "Demo moved ENG-12: title from Todo to In progress\n\nlink"
- `comment`: "Demo commented on ENG-12" / "Demo commented on ENG-12: title\n\n> excerpt (first 300 chars of comment text)\n\nlink"
- `mention`: "Demo mentioned you in ENG-12" (as today)
- digest: "Your Tether digest: N updates"; footer in every email: "Change email settings: {app_base_url}/{slug}/settings/notifications".

`notify.go`'s direct mention emails are removed in favour of `mention` notifications.

## API contract

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/notifications?before={id}` | member | — | `200 {"notifications":[n…],"has_more":bool}` newest first, 30 per page | 404 workspace |
| `GET /api/workspaces/{slug}/notifications/unread-count` | member | — | `200 {"count": n}` | |
| `POST /api/workspaces/{slug}/notifications/read` | member | `ids` (repeated) or `all=true` | `204` | 400 |
| `POST /api/workspaces/{slug}/notifications/unread` | member | `ids` | `204` | 400 |
| `POST /api/workspaces/{slug}/issues/{identifier}/watch` | member | — | `200 {"watching": true}` | 404 |
| `DELETE /api/workspaces/{slug}/issues/{identifier}/watch` | member | — | `200 {"watching": false}` | 404 |
| `GET /api/me/notification-settings` | signed in | — | `200 {"email","mentions","assignments","status","comments","timezone"}` (`timezone` is `null` until the user picks one; the digest uses UTC then) | |
| `POST /api/me/notification-settings` | signed in | same fields (`email` one of the three; booleans `true`/`false`; `timezone` an IANA name, or `""` to clear it) | `200` settings | 400 `{"errors":{"email":"Choose how to get emails."}}`, `{"errors":{"timezone":"Choose a timezone from the list."}}` |

Notification JSON: `{"id","kind","actor": person|null,"issue":{"identifier","title","status": status},"data":{…},"read": bool,"created_at"}`.
Issue JSON (single issue) gains `"watching": bool`.

## Frontend (user app)

- **Navbar bell** (exists): links to `/$workspace/inbox`, shows an unread badge (pink dot with count, max "9+"), polls `unread-count` every 60s and on window focus; `aria-label="Inbox, 3 unread"`.
- **Sidebar**: "Inbox" item with count, above My issues.
- **Inbox page** `routes/_app/$workspace/_main/inbox.tsx`: title "Inbox", "Mark all as read" button, list of rows (actor avatar, one line "Demo moved **ENG-12** title from Todo to In progress", relative time, unread dot). Clicking a row marks it read and opens the issue. Each row has a toggle read/unread icon button. Tabs "All" / "Unread" (URL search `filter=unread`). "Load more" with `before`. Empty: "You're all caught up."
- **Issue page**: "Watch" / "Watching" toggle button (eye icon) in the header next to Edit, `aria-pressed`.
- **Settings → Notifications** `routes/_app/$workspace/_main/settings/notifications.tsx` (sidebar under Profile): "Email me" radio group (Right away / Once a day (daily digest) / Never), and checkboxes "Mentions", "Assigned to me", "Status changes on issues I watch", "Comments on issues I watch" (disabled when Never). Saves on change, shows "Saved" caption briefly.
- Palette (if Phase 5 is in): "Go to Inbox", "Watch issue / Unwatch issue" actions.

## Verification

- Backend: `go vet`, `go test` (recipient rules as a table test on a pure function; collapse rule), curl each endpoint; worker: instant email arrives in Mailpit for assignment/status/comment/mention; mode `off` sends nothing; digest sends one email (set `digest_hour_utc` to the current hour and `last_digest_at` to yesterday to trigger); two API instances don't double-send (run two, check Mailpit count).
- Integration: Maya assigned by demo → Maya's bell shows 1, inbox row, click → issue, read; watch/unwatch; settings save and change email behaviour.
