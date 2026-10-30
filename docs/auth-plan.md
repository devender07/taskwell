# Auth backend plan

Scope: email auth for the user app (sign-up, password sign-in, email-code sign-in, forgot/reset password, sign-out, current user). Google sign-in is out of scope for now; its keys get added to `system_config` when we build it.

## Infra

`docker-compose.yml` at the repo root, compose project `name: tether` so it shows as one group in Docker Desktop.

| Service | Image | Container | Host port | Notes |
|---|---|---|---|---|
| postgres | `postgres:18-alpine` | `tether-postgres` | `5433` → 5432 | 5432 is taken by another project. User/password/db `tether`. Volume `tether-pgdata` mounted at `/var/lib/postgresql` (PG 18 layout). Healthcheck `pg_isready`. |
| mailpit | `axllent/mailpit:v1.27` | `tether-mailpit` | `1025` SMTP, `8025` web inbox | Dev SMTP server; every email lands at http://localhost:8025. |

Start: `docker compose up -d` from the repo root.

## Config

Only bootstrap values come from env (the service needs them before it can reach the DB):

| Env | Default | |
|---|---|---|
| `DATABASE_URL` | `postgres://tether:tether@localhost:5433/tether?sslmode=disable` | |
| `PORT` | `8090` | 8080 is taken locally |

Everything else lives in the `system_config` table and is read from the DB when used (no restart after an admin edits it; the admin app will manage this table later).

```sql
create table system_config (
  key         text primary key,
  value       text not null default '',
  secret      boolean not null default false,  -- admin UI masks these
  description text not null default '',
  updated_at  timestamptz not null default now()
);
```

Seeded keys (dev values point at mailpit):

| key | seed value | secret |
|---|---|---|
| `smtp_host` | `localhost` | |
| `smtp_port` | `1025` | |
| `smtp_username` | `` | |
| `smtp_password` | `` | yes |
| `smtp_from` | `Tether <no-reply@tether.local>` | |
| `app_base_url` | `http://localhost:5173` | |
| `refresh_ttl_hours` | `720` | |
| `access_ttl_minutes` | `15` | |
| `jwt_secret` | generated on first start | yes |
| `otp_ttl_minutes` | `10` | |
| `reset_ttl_minutes` | `30` | |

## Schema

Plain SQL migrations embedded in the binary, applied in order at startup and tracked in `schema_migrations(version)`.

```sql
users(id uuid pk default uuidv7(), name text not null, email text not null unique /* stored lowercased */,
      password_hash text /* null = no password set */, created_at timestamptz not null default now())
refresh_tokens(token_hash bytea pk /* sha256 of cookie token */, user_id uuid references users on delete cascade,
               family_id uuid not null /* one per sign-in */, used_at timestamptz /* set on rotation */,
               expires_at timestamptz not null, created_at timestamptz not null default now())
auth_tokens(id uuid pk default uuidv7(), user_id uuid references users on delete cascade,
            purpose text not null check (purpose in ('signin_code','password_reset')),
            token_hash bytea not null, expires_at timestamptz not null,
            attempts int not null default 0, used_at timestamptz, created_at timestamptz not null default now())
```

## API contract (user service)

- All endpoints under `/api`. Request bodies are **FormData** (`multipart/form-data` or `application/x-www-form-urlencoded`; the server reads both via `r.FormValue`). Responses are JSON.
- Tokens (all cookies HttpOnly, SameSite=Lax, Secure when `app_base_url` is https):
  - `tether_access`: HS256 JWT (`iss=tether`, `sub=<user id>`, `exp`), Path=/api, lives `access_ttl_minutes`. Verified without a DB lookup; not revocable before it expires.
  - `tether_refresh`: opaque random token, Path=/api/auth, lives `refresh_ttl_hours` from its last use. Only its SHA-256 is stored. Every refresh rotates it; presenting an already-rotated token more than 10 s later revokes the whole sign-in family (theft detection).
  - Remember me (`remember` = `on`/`true`/`1` on sign-in): persistent cookies, sign-in lasts `refresh_ttl_hours` (30 days). Otherwise both cookies are browser-session cookies and the sign-in is capped at `short_refresh_ttl_hours` (24 h). Both count from sign-in; refreshes keep the family's expiry. Sign-up always remembers.
  - The frontend retries any 401 (outside `/auth/*`) once after `POST /api/auth/refresh`.
- User JSON: `{"id": "...", "name": "...", "email": "..."}`
- Validation error: `400 {"errors": {"<field>": "<message>"}}` with the same messages and rules as `frontend/shared/src/validate/validate.ts` (email, name, password = 8+ chars and ≤72 bytes).
- Other errors: `{"error": "<message>"}` with 401 / 409 / 429 / 503 / 500.

| Method & path | Form fields | Success | Failures |
|---|---|---|---|
| `POST /api/auth/signup` | `name`, `email`, `password` | `201` user, sets cookie | 400 field errors; 409 `{"errors":{"email":"An account with this email already exists."}}` |
| `POST /api/auth/signin` | `email`, `password`, `remember` (optional) | `200` user, sets cookie | 400; 401 `{"error":"Wrong email or password."}` |
| `POST /api/auth/otp/send` | `email` | `204` always (no account enumeration); emails a 6-digit code if the account exists | 400; 503 if SMTP is not configured |
| `POST /api/auth/otp/verify` | `email`, `code`, `remember` (optional) | `200` user, sets cookie | 400; 401 `{"error":"That code is invalid or expired."}`; max 5 attempts per code, then it's dead |
| `POST /api/auth/password/forgot` | `email` | `204` always; emails `{app_base_url}/reset-password?token=…` if the account exists | 400; 503 |
| `POST /api/auth/password/reset` | `token`, `password` | `204`; revokes all refresh tokens of that user (access tokens run out within `access_ttl_minutes`) | 400; 401 `{"error":"This reset link is invalid or expired."}` |
| `POST /api/auth/refresh` | — | `204`, new access + refresh cookies | `401`, clears cookies |
| `POST /api/auth/signout` | — | `204`, clears both cookies, revokes the sign-in family | — |
| `GET /api/me` | — | `200` user | `401` |

Sending a new code or reset link invalidates earlier unused ones of the same purpose for that user.

## Backend layout (`backend/user`)

```
cmd/user/main.go          wire: env → pgxpool → migrate → handlers → http.ListenAndServe
internal/db/              pool, migrate, migrations/*.sql (embed)
internal/sysconfig/       Get(ctx, key) / GetInt from system_config
internal/mail/            Send(ctx, to, subject, body) via net/smtp using sysconfig
internal/auth/            routes.go, controller.go (HTTP), model.go (SQL), session.go (cookies/JWT), validate.go
```

Dependencies: `github.com/jackc/pgx/v5` (Postgres), `golang.org/x/crypto/bcrypt` (passwords). Everything else stdlib (`net/http` ServeMux patterns, `net/smtp`, `crypto/rand`).

## Frontend (user app)

- Vite dev proxy: `/api` → `http://localhost:8090`.
- `src/api.ts`: one small `fetch` helper that posts FormData, parses JSON, and throws a typed error carrying `status`, `errors` (field map) and `error`.
- TanStack Router does the data and auth gating:
  - Pathless layout `routes/_app.tsx` with `beforeLoad` calling `GET /api/me`; on 401 `throw redirect({ to: '/signin' })`; returns `{ user }` into route context. The dashboard moves to `routes/_app/index.tsx` and shows "Hi {name}" plus a sign-out button.
  - `(auth)` routes get a `beforeLoad` that redirects signed-in users to `/`.
- Forms keep their client-side checks, then post their `FormData` straight to the API, map `errors` onto fields and `error` onto a form-level message, and on success `router.invalidate()` + navigate to `/`.
- New page `routes/(auth)/reset-password.tsx` (`?token=`), new password form → `POST /api/auth/password/reset` → to `/signin?method=password` with a success note.
- Google button stays inert.

## Verification

- Backend: `docker compose up -d`, run the service, exercise every endpoint with `curl -F`, read codes/links from mailpit's API (`GET http://localhost:8025/api/v1/messages`), and confirm the tables in Docker Desktop / `psql`.
- Frontend: `npm run build`, then click through every flow against the running backend.
