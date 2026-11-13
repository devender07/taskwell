# Weekly digest plan

Scope: once a week each person gets one email summarising their work across all their workspaces:
- **Closed last week:** their issues that reached a completed status in the last 7 days.
- **Due:** their open issues that are overdue or due in the next 7 days.
- **Blocked:** their open issues waiting on an open blocker, and their open issues blocking someone else's.

It reuses the daily digest's machinery (`internal/notify`: worker loop, timezone, `digest_hour_utc`, the mail queue).
It's a different email from the existing daily notification digest, and both can be on.

Out of scope:
- HTML email. Every Taskwell email is plain text today; this one is too.
- A workspace-wide or team digest (what the whole team closed). This is the person's own work only.
- Choosing the weekday. It's Monday, fixed.
- Sending it from the MCP server, a preview page, or a "Send me one now" button.

## Behaviour

- **When:** Monday at `digest_hour_utc` (default 8) in the person's notification timezone (UTC until they choose one),
  the same clock the daily digest uses.
- **Who:** everyone with a verified email who isn't suspended, has the weekly digest on (default on) and hasn't turned
  email off (`notification_settings.email = 'off'` stops this email too).
- **Empty weeks:** nothing to report in any section means no email that week. It still counts as sent.
- **What counts:**
  - Their issues are the ones assigned to them now.
  - Only issues they can see: live (not trashed), in active (not archived) projects, `can_see_project` true. Guests
    only see their projects' issues.
  - **Closed:** a `status` event into a completed status in the last 7 days (as analytics does), and the issue is still
    completed.
  - **Open:** the issue's status category isn't completed or canceled.
  - **Due:** open, with `due_date` before today + 7 days in their timezone. Overdue ones (before today) are marked
    "overdue".
  - **Blocked:** open, with at least one `issue_links` blocker that is open.
  - **Blocking:** open, blocking at least one open issue assigned to someone else.
- **Layout:** grouped by workspace (workspaces with nothing to report are left out), then the four sections in the
  order above. Each line is `KEY-12 Title` plus the issue link, with the due date for due lines and the blocker's
  identifier for blocked lines. A section shows at most 15 lines, then "and N more" linking to that workspace's My
  issues.

Example:

```
Subject: Your week in Taskwell: 3 closed, 2 due, 1 blocked

Acme

Closed last week (3)
- WEB-12 Sign-in page redesign  https://app.taskwell.dev/acme/issue/WEB-12
...

Due (2)
- WEB-20 Billing settings page, overdue since Nov 3  https://…
- API-4 Rate limits, due Thu Nov 12  https://…

Blocked (1)
- WEB-31 Export to CSV, waiting on API-9  https://…

Change email settings: https://app.taskwell.dev/acme/settings/notifications
```

The subject leaves out sections that are zero ("Your week in Taskwell: 2 due"). The settings link uses the first
workspace in the email.

## Schema (`backend/user/internal/db/migrations/022_weekly_digest.sql`)

```sql
alter table notification_settings
  add column weekly boolean not null default true,   -- the weekly digest is on
  add column last_weekly_at timestamptz default now(); -- when the last one was sent (or skipped)
```

- **Rows for members without settings:** each run first adds a settings row (weekly on, `last_weekly_at` = now) for
  every workspace member who has none. Postgres can't lock the nullable side of an outer join.
- **No surprise first email:** `last_weekly_at` starts at now (existing rows get the migration time), so nobody gets one
  on the weekday this ships or the day they sign up. The first one comes the next Monday.
- **Skipped people still count as sent:** people skipped that Monday (weekly off, email off, unverified, suspended) are
  marked sent as well, so turning it on mid-week waits for the next Monday.

No new `system_config` keys: the send hour is `digest_hour_utc`.

## API

`GET /api/me/notification-settings` and `POST /api/me/notification-settings` gain one field:

| Field | Type | Notes |
|---|---|---|
| `weekly` | JSON bool; FormData `"true"` / `"false"` | Optional on POST like the others; anything else is `400 {"errors": {"weekly": "Choose on or off."}}` |

No new endpoints.

## Backend (`internal/notify`)

- **`model.go`:** `Settings` and the settings queries get `weekly`. `update` takes a `weekly *bool`.
- **`weekly.go` (new):**
  - `weeklyNext(ctx, now, hour, queue)` follows `digestNext`.
    - In one transaction, lock one due user (`for update of s skip locked`, rows with no settings row included through a
      left join).
    - Due means Monday at `hour` this week in their timezone, computed from `date_trunc('week', $now at time zone tz)`,
      has passed and `last_weekly_at` is before it.
    - Collect the four sections, queue the email when any section has lines, upsert `last_weekly_at = $now`.
    - Return false when nobody is due.
  - `now` is a parameter, not SQL `now()`, so tests can pick the moment.
  - `weeklyEmail(sections, base) (subject, body string)` builds the text, like `digestEmail`.
- **`worker.go`:** `queueEmails` runs `weeklyNext` until it returns false, after the daily digests.
- **Controller:** read and validate `weekly` with the other booleans.

Tests:
- **`weekly_test.go`:**
  - Due timing: before Monday's hour, at it, already sent this week, a timezone where it's still Sunday.
  - Each section's rules: a completed issue reopened since, a trashed issue, an archived project, a private project the
    user can't see, a canceled blocker, a blocker in another workspace, an overdue vs. due-later issue.
  - An empty week sends nothing but records the send.
  - `email = 'off'` and `weekly = false` skip.
- **`weeklyEmail`:** a table test on the subject and the "and N more" cap.

## Frontend (user app)

- **`api.ts`:** `NotificationSettings` gets `weekly: boolean`.
- **Settings → Notifications** (`routes/_app/$workspace/_main/settings/notifications.tsx`): a "Weekly summary"
  checkbox under the email setting.
  - Caption: "Monday mornings in your digest timezone: what you closed last week, what's due and what's blocked."
    The page doesn't know the send hour (`digest_hour_utc` isn't sent to the browser), so it doesn't name one.
  - It saves on change like the other checkboxes. It's disabled, with "Turn on email to get it.", when email is off.

Sentence case everywhere.

## Verification

- `go vet`, and `go test ./internal/notify/`.
- `npm run build`, `npm run check`.
- Integration:
  1. Set your timezone.
  2. In psql, set `last_weekly_at` to last week, or set `digest_hour_utc` to the current hour on a Monday.
  3. With `NOTIFY_INTERVAL` short, check Mailpit for the email: grouped by workspace, links open the issues, and empty
     sections are left out.
  4. Turn the checkbox off and confirm nothing comes the next Monday (reset `last_weekly_at` to retry).
