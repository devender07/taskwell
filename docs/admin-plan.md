# Admin app plan

Scope: the internal admin app (Tether staff, not workspace admins): sign in, edit `system_config` (secrets masked), list workspaces and users, suspend/unsuspend a user, basic usage stats. Backend `backend/admin` (port 8080), frontend `frontend/admin` (port 5174).

Out of scope: impersonation, deleting users/workspaces from admin, audit log, 2FA for admins, billing.

## Who is an admin

- Migration `backend/user/internal/db/migrations/018_platform_admin.sql` (the user service owns migrations; admin service shares the DB and runs none):
  ```sql
  alter table users add column platform_admin boolean not null default false;
  alter table users add column suspended_at timestamptz;
  ```
- Promote from the command line: `tether-admin -promote you@example.com` (sets `platform_admin = true`, prints the result, exits). No UI for promoting.
- Admin sign-in uses the user's normal email + password (`password_hash` in `users`, same hashing as the user service — reuse by copying the small verify function, no cross-module import), only for `platform_admin` users that aren't suspended. Wrong email/password/not admin → same message: "Email or password is incorrect."
- Session: an HS256 JWT signed with the `jwt_secret` from `system_config` but with `aud: "admin"` (user-service access tokens must not work here, and admin tokens must not work in the user service — check the user service's `parseAccess` rejects tokens with that audience; add the check if needed), 8h lifetime, cookie `tether_admin` (`Path=/api`, HttpOnly, SameSite=Strict, Secure when `app_base_url` is https). No refresh token; sign in again after 8h.
- The admin backend needs `pgx` in `backend/admin/go.mod` and `golang-jwt` — both already used by the user module (same versions), so not new to the project.

## Suspension (user service changes)

- Suspended users can't sign in (any method: password, email code, Google; message "This account is suspended."), can't refresh (refresh → 401 and the cookie is cleared), and suspending deletes their refresh tokens so they're out within the access-token TTL.
- `Authenticate` doesn't hit the DB per request (`ponytail:` up to `access_ttl_minutes` of access after suspension).

## Admin API (`backend/admin`, `/api/...`)

Same conventions as the user service: FormData in, JSON out, `{"error": msg}` / `{"errors": {field: msg}}`. Every endpoint except sign-in needs the admin cookie (401 `{"error":"Not signed in."}`).

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `POST /api/auth/signin` | `email`, `password` | `200 {"mfa":"verify"}` or `200 {"mfa":"enrol","secret","otpauth_url"}` + a pending cookie (10 min, only good for `/api/auth/mfa`) | 400 field errors (required); 401 `{"error":"Email or password is incorrect."}`; 429 |
| `POST /api/auth/mfa` | `code` (6 digits) or `recovery_code` | `200 {"id","name","email","recovery_codes"?}` + the real cookie; `recovery_codes` (10) only when this code confirmed enrolment | 400 `{"errors":{"code"}}` (empty); 401 `{"errors":{"code"\|"recovery_code":"That code is incorrect."}}`; 401 `{"error":"Your sign-in timed out. Sign in again."}`; 429 |
| `POST /api/auth/signout` | — | `204` deletes the session row, clears cookie | |
| `GET /api/audit?before=` | — | `200 {"entries":[{"id","at","actor_id","actor_email","action","target","ip"}],"more"}` 100 per page, newest first; `before` = last id shown | 400 `{"error":"Invalid cursor."}` |
| `GET /api/me` | — | `200 {"id","name","email"}` | 401 |
| `GET /api/config` | — | `200 [{"key","value","secret","description","updated_at"}]` ordered by key; secret values are `""` with `"has_value": bool` | |
| `POST /api/config/{key}` | `value` | `200` the row (masked) | 404 `{"error":"Setting not found."}`; 400 `{"errors":{"value": msg}}` for keys with known formats: `*_ttl_*`, `*_minutes`, `*_hours`, `*_days`, `smtp_port`, `upload_max_mb`, `digest_hour_utc` must be whole numbers in range (port 1–65535, hour 0–23, others ≥ 1); `app_base_url` an http(s) URL; `smtp_from` a mail address. `jwt_secret` can't be edited here (400 "This setting can't be changed here.") |
| `GET /api/workspaces?q=&page=` | — | `200 {"workspaces":[{"id","name","slug","members","projects","issues","created_at"}],"total"}` 50 per page, newest first, `q` matches name/slug | |
| `GET /api/users?q=&page=` | — | `200 {"users":[{"id","name","email","workspaces","created_at","suspended": bool,"platform_admin": bool,"last_active_at": string\|null}],"total"}` (`last_active_at` = newest refresh token use/creation, whatever the auth tables have) | |
| `POST /api/users/{id}/suspend` | — | `200 user` | 404; 400 `{"error":"You can't suspend yourself."}` |
| `POST /api/users/{id}/unsuspend` | — | `200 user` | 404 |
| `GET /api/stats` | — | `200 {"totals":{"users","workspaces","projects","issues","comments"},"active_users_7d","signups":[{"date","count"}…30 days],"issues_created":[{"date","count"}…30 days]}` (days with 0 included) | |

Layout: `backend/admin/internal/{auth,audit,config,directory,stats}` (routes/controller/model each) + `cmd/admin/main.go` (env `DATABASE_URL` same default as user service, `PORT` default 8080). Runnable check: config value validation table test.

## Hardening (P1 audit)

Schema in `backend/user/internal/db/migrations/022_security_hardening.sql` (the user service owns migrations).

**Sessions.** The cookie is still an HS256 JWT with `aud: admin`, but its `jti` names an `admin_sessions` row, and `Authenticate` checks the row on every request: it must have passed MFA (`mfa_at`), be under 8 h old (`expires_at`, absolute) and used within the last hour (`last_seen_at`, bumped by each request), and belong to an unsuspended platform admin. Sign-out deletes the row, so a copied cookie dies with it. Expired rows are swept on each sign-in.

**MFA (TOTP, RFC 6238).** stdlib HMAC-SHA1, 30 s steps, 6 digits, ±1 step for drift (`auth/totp.go`). A used step is stored (`admin_mfa.last_step`) so a code can't be replayed. Enrolment happens at the first sign-in after the password: the response carries a fresh base32 secret and an `otpauth://` URL, shown as text (no QR library); each sign-in replaces the secret until a code confirms it. Confirming issues 10 recovery codes, shown once and stored as SHA-256 (60 random bits each, so a fast hash is enough); each works once. Wrong codes count in `auth_failures` (`admin-mfa:<user id>`, 10 per hour) on top of the per-IP limiter.
- Trade-off: whoever passes the password first enrols. Promote someone and have them sign in right away; `-reset-mfa` clears a factor.
- ponytail: the TOTP secret sits in Postgres in plain text like the other secrets (O5); encrypt it with a key from env if the database gets its own threat model.
- Lost device: `tether-admin -reset-mfa <email>` deletes the factor and the admin's sessions; their next sign-in enrols again.

**Audit log.** `admin_audit` is append-only (a trigger rejects update, delete and truncate; `actor_id` has no foreign key so rows outlive users, and the actor's email is copied in). Recorded, in the same transaction as the change where there is one: `config.update` (the key, never the value), `user.suspend` / `user.unsuspend` (the email), `admin.promote` and `admin.mfa_reset` (CLI, ip `cli`), `signin`, `signin.failed` (email tried), `signin.mfa_failed`, `mfa.enrolled`, `mfa.recovery_used`. IP is `web.ClientIP` (proxy-aware, like the user service's).

Not done: an IP allowlist (put it at the proxy), and a guard on `app_base_url` changes beyond the audit entry.

## Frontend (`frontend/admin`)

Same stack and shared components as the user app. `vite.config.ts`: proxy `/api` → `http://localhost:8080`, `server.host: true`. `src/api.ts` like the user app's (no refresh; a 401 sends to `/signin`).

- `/signin`: email + password form (reuse shared Input/Button; same card look as the user app's sign-in), then the code step: the setup key and otpauth link on enrolment, else the code or a recovery code; after enrolling, the recovery codes once.
- `/audit`: the audit log table, newest first, with an "Older" link (`?before=`).
- Layout with sidebar: Overview, Users, Workspaces, Config; signed-in email + "Sign out" at the bottom.
- `/` **Overview**: stat tiles (users, workspaces, projects, issues, comments, active users 7d) + two small bar charts (signups, issues created, last 30 days) drawn with plain SVG/CSS (no chart library), each bar with a `title` tooltip.
- `/users`: search input (URL `q`), table (name, email, workspaces, joined, last active, status chip "Suspended"/"Admin"), Suspend/Unsuspend button with a confirm `Dialog`, pagination (URL `page`).
- `/workspaces`: search, table (name, slug, members, projects, issues, created), pagination.
- `/config`: table of settings grouped by prefix (SMTP, Auth/tokens, App, Uploads, Notifications, Google), each row: key (mono), description, value input (password-type input showing "••••••" placeholder for secrets with a value; empty secret input = keep), Save per row, inline errors and a "Saved" caption.
- Tables scroll horizontally on narrow screens.

## Verification

- `go vet`, `go test` for both modules; curl every admin endpoint incl. 401s; suspend demo user → user-service sign-in returns "This account is suspended." and refresh fails; unsuspend → works.
- `npm run build`; integration in headless Chrome against admin API + Vite on 5174 (start them in the scratchpad), promote `demo@tether.test` via the CLI for testing.
