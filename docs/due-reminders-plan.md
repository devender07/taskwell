# Due-date reminders plan

Scope: the assignee of an open issue gets a notification the day before it's due ("due tomorrow") and the day after
the due date passes ("overdue"). These are ordinary notifications, so they show in the Taskwell bot, the bell and Home,
and they're emailed (instantly or in the daily digest) like the others.

Out of scope:
- Reminders to watchers or the creator. Only the assignee is reminded.
- Choosing how many days before. It's always one day before.
- Repeating overdue reminders. One per due date.
- Reminders for cycles, milestones or project target dates.

## Behaviour

Due dates are calendar dates, so "tomorrow" and "past" are measured in the assignee's notification timezone (UTC until
they choose one). Reminders go out from the existing once-a-minute notification job at `digest_hour_utc`, the same hour
the daily digest uses, so nobody is pinged at midnight.

- **Due soon** (`due_soon`): on the day before `due_date` in the assignee's timezone, once that day's
  `digest_hour_utc` has passed.
  - It doesn't fire on the due date itself or later, so it's never late with "due tomorrow".
- **Overdue** (`overdue`): on any day after `due_date`, once that day's hour has passed, at most 7 days after the due
  date.
  - The 7-day limit stops a flood for issues that were already long overdue when this ships.
- **Which issues:**
  - The issue is live (not trashed) and open: its status category isn't completed or canceled.
  - It has an assignee and a due date, and its project is active (not archived).
  - The assignee is a member of the workspace, can see the project (`can_see_project`) and isn't suspended.
- **Once each:** one `due_soon` and one `overdue` per issue, assignee and due date. Moving the due date (or reassigning)
  makes a new reminder possible for the new date or person. The check is the notification itself:
  `not exists (… notifications where user_id, issue_id, kind and data->>'due_date' match)`. No extra table.
- **Actor:** none (`actor_id` null). The app shows it as from Taskwell.
- **Email:** the reminders follow the user's email setting (instant, digest or off) and a new `due_dates` toggle
  (default on). In-app reminders are always created; the toggle only controls email, like the other kinds.

## Schema (`backend/user/internal/db/migrations/023_due_reminders.sql`)

```sql
alter table notifications drop constraint notifications_kind_check,
  add constraint notifications_kind_check check (kind in ('mention', 'assigned', 'status', 'comment', 'chat_mention',
    'message', 'project_added', 'invite', 'due_soon', 'overdue'));
alter table notification_settings add column due_dates boolean not null default true;
create index on notifications (issue_id, kind) where kind in ('due_soon', 'overdue');
```

The target check is unchanged: both kinds carry an `issue_id`. `data` is `{"due_date": "YYYY-MM-DD"}`.

## API

- **Notification JSON (existing list):** kinds `due_soon` and `overdue` look like the other issue kinds: `issue` set,
  `chat`, `project` and `invite` null, `actor` null, `data.due_date` the date reminded about.
- **`GET` / `POST /api/me/notification-settings`:** gain `due_dates` (JSON bool; FormData `"true"` / `"false"`).
  Anything else is `400 {"errors": {"due_dates": "Choose on or off."}}`, like the other booleans.
- No new endpoints.
- **Realtime:** creating reminders publishes the user's notifications change (`realtime.NotificationsChanged`), so the
  bell, the bot and the unread count update live.

## Backend (`internal/notify`)

- **`due.go` (new):** `remindDue(ctx, now, hour) (int, error)`.
  - Inserts the reminders that are due, as one `insert … select` per kind.
  - Due means: the assignee's local date (`$now at time zone coalesce(s.timezone, 'UTC')`) is the right day and the
    local time is past `hour`.
  - Users with no settings row count as UTC with `due_dates` on: left join `notification_settings`, no locking needed.
  - Publishes a notifications change per affected user and workspace.
  - `now` is a parameter, so tests choose the moment.
- **`worker.go`:** `queueEmails` calls `remindDue` before queuing instant emails, so a new reminder is emailed in the
  same run (once it has settled) or picked up by the digest.
- **`rules.go`:** handles the two kinds.
  - Instant subjects: "WEB-12 is due tomorrow" and "WEB-12 is overdue".
  - Instant bodies: "WEB-12: Title is due tomorrow (Thu, Nov 12)." and "WEB-12: Title was due Wed, Nov 11 and is still
    open.", then the issue link and footer.
  - Digest lines: "due tomorrow" and "overdue since Nov 11".
- **`model.go`:** settings gain `due_dates`; `wanted` maps both kinds to `s.due_dates`; the list and email queries
  handle a null actor (shown as "Taskwell").
- **Controller:** reads `due_dates` with the other booleans.

Tests (`due_test.go`):
- **Timing:** the day before before and after the hour, the due date itself (no due-soon), the day after (overdue),
  8 days after (none), and a timezone where it's still the previous day.
- **Rules:** completed, canceled, unassigned, trashed, archived project, and an assignee who can't see a private
  project all get none.
- **Repeats:** running twice gives one reminder. A moved due date gives a new one.
- **Email:** `due_dates = false` creates the in-app reminder but doesn't email it.
- **Text:** a `rules` test for the subjects and digest lines.

## Frontend (user app)

- **`api.ts`:** the `Notification` union's issue kinds include `due_soon` and `overdue`; `data` gains `due_date?`;
  `NotificationSettings` gains `due_dates: boolean`.
- **`components/inbox/feed-row.tsx`:**
  - `notificationText` reads "<issue> is due tomorrow" and "<issue> is overdue (due Nov 11)".
  - With no actor the row shows "Taskwell", using the bot's logo avatar if the row shows one.
- **Settings → Notifications:** "Due dates" in the "Email me about" checkboxes, caption "The day before an issue
  assigned to you is due, and when it's overdue."

Sentence case everywhere.

## Verification

- `go vet`, and `go test ./internal/notify/`.
- `npm run build`, `npm run check`.
- Integration:
  1. Assign yourself an issue due tomorrow.
  2. Set `digest_hour_utc` to the current UTC hour.
  3. Within a minute the Taskwell bot shows "is due tomorrow", and Mailpit has the email.
  4. Set the due date to yesterday: an "overdue" reminder follows.
  5. Complete the issue: nothing more comes.
