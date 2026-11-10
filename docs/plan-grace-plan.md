# Plan grace (time-limited comps)

Staff can already comp a plan on a user's account from the admin app (Users → Set plan → Comped): it lasts until
someone clears it. This adds an end date, so staff can upgrade someone for a while (a trial extension, a goodwill
period while a payment is sorted out, a sales pilot) and have it end on its own.

## Behaviour

- In the admin Set plan dialog, when Comped is ticked, a "For" choice: No end date (as today), 7, 14, 30 or 90 days.
- While it lasts the account is on that plan with `plan_comped = true`, exactly like a comp today (Paddle events
  leave it alone; workspaces the user owns get the plan's limits and features).
- When it ends the comp is cleared the same way clearing it by hand works: the plan becomes that of the user's latest
  Paddle subscription granting access (active, trialing, past_due), else Free. A `billing.plan_changed` entry goes in
  every workspace they own (when the plan changes), open tabs are told, and the user gets an email.
- Setting a plan again replaces the end date (or removes it with No end date); clearing the comp clears it.

## Schema

`backend/user/internal/db/migrations/036_plan_grace.sql`: `alter table users add column plan_comped_until timestamptz`
(null: no end, or not comped), plus a partial index on it `where plan_comped_until is not null`.

## API

### Admin API (`backend/admin/internal/directory`)

- `POST /api/users/{id}/plan`, FormData `plan`, `comped` (as today) and optional `days`:
  - `comped=true` with `days` empty or absent: no end (`plan_comped_until = null`).
  - `comped=true` with `days` 1–365: `plan_comped_until = now() + days`. Anything else: 400
    `{"error": "Choose 1 to 365 days."}`.
  - `comped=false`: clears the comp and `plan_comped_until`.
- `User` JSON (list and this response) gains `comped_until` (RFC 3339 or null).
- The admin_audit target says the end: `"ann@x.com: pro (comped until 2026-11-24)"`.

### User API

- `GET /api/billing` gains `comped_until` (RFC 3339 or null).
- A job in the user API (in the `billing` package, reusing `planChanged`) runs every minute: for each user with
  `plan_comped and plan_comped_until <= now()` (row-locked, `for update skip locked`), revert as above in one
  transaction, then enqueue the email. Safe to run on several instances.
- Email (branded layout, `mail.Render`): subject "Your Taskwell <Plan> access has ended", body says they're on
  <new plan> now, with a "See plans" button to `<app>/pricing`.
- A test: a comp that ended is reverted to Free (and to the subscription's plan when there's one granting access),
  one that hasn't ended is left alone, the audit entry and email are made.

## Frontend

### Admin app

- `PlanButton` dialog: the plan and, under the Comped checkbox, a "For" choice (No end date, 7, 14, 30, 90 days), both SmartAutocomplete,
  shown only while Comped is ticked; sent as `days`.
- Users list: "· comped" becomes "· comped until Nov 24, 2026" when there's an end.
- `UserRow` gains `comped_until: string | null`.

### User app

- Settings → Billing: "Comped by Taskwell" becomes "Comped by Taskwell until Nov 24, 2026" when there's an end.
- `Billing` type gains `comped_until: string | null`.

## Not doing

- A reminder email before the end. `ponytail: add a "ends in 3 days" email from the same job if users get surprised.`
- Per-workspace grace; grace on limits only (it's the whole plan).
