# Required two-factor, re-authentication and the admin user page

Three pieces:

1. **Require two-factor sign-in per workspace.** Admins can require it. Members without two-factor are blocked
   from the workspace until they turn it on.
2. **Confirm it's you.** Creating an API token or deleting a workspace asks for a recent re-authentication (password,
   authenticator code, or an email code for accounts without a password).
3. **Admin user page and CSV exports.** One page per user with everything staff need, plus CSV exports of users and
   payments.

## Schema

`backend/user/internal/db/migrations/039_security_admin.sql`: `alter table workspaces add column require_mfa
boolean not null default false`.

## 1. Required two-factor (user API)

- `POST /api/workspaces/{slug}` (the existing workspace update, FormData) accepts `require_mfa` (`true`/`false`). It
  needs `workspace.edit`.
  - Turning it on when the actor has no two-factor (`user_mfa` enabled) is refused with 400 `{"fields":
    {"require_mfa": "Turn on two-factor sign-in for your account first."}}`.
  - It's audited as `workspace.require_mfa_changed` with `{"to": true|false}`.
- The workspace JSON (`GET /api/workspaces/{slug}`) gains `require_mfa: boolean` and `mfa_blocked: boolean`.
  `mfa_blocked` is true when the workspace requires two-factor and the viewer has none.
- **Enforcement.** While `mfa_blocked`, every `/api/workspaces/{slug}/…` route except these answers 403
  `{"error": "This workspace requires two-factor sign-in.", "mfa_required": true}`:
  - `GET /api/workspaces/{slug}`
  - `GET /api/workspaces/{slug}/features`
  - `GET /api/workspaces/{slug}/billing`
  - leaving the workspace
  
  The same applies to API tokens, since a token is the user's. Put this in one middleware next to the feature
  middleware, reusing its slug resolution. Workspace lists (`/api/workspaces`) stay as they are.
- `POST /api/me/mfa/disable` is refused with 409 `{"error": "<Workspace name> requires two-factor sign-in. Leave it
  or ask an admin to change that first."}` while the user is a member of a workspace that requires it.
- A test covers: a blocked member gets 403 on an issue route and 200 on the workspace GET; turning it on without
  two-factor is a 400; disable is a 409.

## 2. Confirm it's you (user API)

- `POST /api/auth/reauth` (signed in, rate limited like the other auth posts) takes FormData with one of:
  - `password`;
  - `code` (authenticator app, only when two-factor is on);
  - `email_code` (from `POST /api/auth/reauth/code`, which emails a 6-digit code via the existing OTP/code
    machinery and answers 204).

  On success it answers 204 and sets a short-lived signed proof: an HttpOnly cookie `taskwell_reauth` holding the
  user id, expiring in 10 minutes (signed like the existing `taskwell_mfa` cookie). A wrong value answers 401 with
  field errors ("That password is incorrect." / "That code is incorrect."), and wrong values count toward the usual
  lockout. It's recorded as account event `reauth`, or `reauth_failed` on failure.
- `GET /api/me/reauth` answers `{"methods": ["password", "totp", "email"]}`, the methods this account can use:
  - `password` when it has a password;
  - `totp` when two-factor is on;
  - `email` always.
- Two endpoints require a fresh proof: `POST /api/me/tokens` and `DELETE /api/workspaces/{slug}`. Without one they
  answer 403 `{"error": "Confirm it's you to continue.", "reauth": true}`, checked before anything else changes.
  Bearer (API token) requests can't do either, as today.
- A test covers: create token without proof is 403 reauth; after a correct password it's 201; a wrong password is
  401; the proof expires.

## 3. Admin user page and CSV (admin API)

- `GET /api/users/{id}` returns 200, or 404 `{"error": "User not found."}`:

  ```json
  { "user": UserRow,                       // same shape as the list's row
    "mfa": true,
    "workspaces": [ { "id": "…", "name": "…", "slug": "…", "role": "Admin", "owner": true, "members": 3, "issues": 40 } ],
    "subscriptions": [ { "subscription_id": "…", "plan": "pro", "status": "active", "interval": "month",
                         "currency": "usd", "unit_amount": 1200, "next_billed_at": "…|null", "updated_at": "…" } ],
    "payments": [ { "transaction_id": "…", "currency": "usd", "total": 1200, "status": "completed", "billed_at": "…" } ],
    "plan_history": [ { "at": "…", "actor": "staff email or null", "action": "…", "detail": "…" } ],
    "events": [ { "action": "sign_in", "detail": "password", "ip": "…", "user_agent": "…", "created_at": "…" } ] }
  ```

  - `payments` holds the newest 50.
  - `plan_history` takes the admin_audit entries targeting this user (plan set, comp, suspend) together with the
    `billing.plan_changed` entries from the workspaces the user owns, deduplicated by time, newest 50.
  - `events` holds the newest 50 account_events.
  
  Use whatever columns these tables really have; keep the JSON keys.
- `GET /api/users.csv?q=` returns `text/csv`, `Content-Disposition: attachment; filename="users.csv"`. Every
  matching user is a row: id, name, email, plan, comped, comped_until, suspended, mfa, workspaces, created_at,
  last_active_at. It uses `encoding/csv`, and a value starting with `= + - @` gets a leading `'`, so a spreadsheet
  doesn't run it as a formula.
- `GET /api/payments.csv` returns every paddle_transactions row: transaction_id, email, currency, subtotal, tax,
  total, earnings, status, billed_at. Amounts are in major units with 2 decimals (`12.00`).
- Both exports are audited in admin_audit (`users.exported`, `payments.exported`).
- A test covers the detail endpoint's shape and 404, and checks that the CSV has a header and escapes formulas.

## Frontend

### User app

- **Settings → Workspace**: a "Require two-factor sign-in" checkbox for `workspace.edit`, with the help line
  "Members without it can't open the workspace until they turn it on." The 400 field error shows under it.
- **Blocked workspace**: when `mfa_blocked`, the `_main` layout shows a full-page panel instead of the outlet.
  - Panel text: "<Workspace> requires two-factor sign-in", "Turn it on to keep working here."
  - Buttons: **Set up two-factor** (Settings → Profile, where `two-factor.tsx` lives) and **Switch workspace**.
  - Settings → Profile stays reachable. Any 403 with `mfa_required` sends the user back to the panel.
- **Confirm it's you**:
  - `api.ts` knows the 403 `reauth` answer.
  - A shared `ReauthDialog` asks with the best available method: password field, else authenticator code, else
    "Email me a code" then a code field, with links to switch between the account's methods.
  - On success it retries the original action.
  - The API token create and the delete workspace flows use it.

### Admin app

- `/users/$id`: the detail page, linked from the Users list (the name cell).
  - **Header:** name, email, plan chip, Suspended chip, "Two-factor on/off", joined, last active.
  - **Actions** (reuse the existing components): `PlanButton` (plan, comp, grace) and `SuspendButton`.
  - **Sections:** Workspaces (table), Subscriptions, Payments (money formatted like the Overview), Plan history,
    Sign-ins and security (events table: action, detail, IP, when).
  - Empty states, full width, stacked at phone width.
- **Export CSV** buttons:
  - on Users, it downloads `/api/users.csv?q=<current search>`;
  - on Overview's Recent payments card, it downloads `/api/payments.csv`.

## Not doing

- Requiring two-factor at sign-in time for the whole account (enforcement is per workspace route).
- Re-auth for other actions (role changes, member removal). `ponytail: wrap more endpoints with the same proof check
  when compliance asks.`
- Payment CSV date filters.
