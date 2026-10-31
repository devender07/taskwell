# Deploy

One host runs everything with Docker Compose: Postgres 18, the user and admin Go services, and Caddy, which serves both SPAs, proxies `/api` (WebSockets included) and handles HTTPS.

| File | What it is |
| --- | --- |
| `backend.Dockerfile` | Either Go service (`--build-arg SVC=user` or `SVC=admin`): static binary on distroless, runs as nonroot |
| `web.Dockerfile` | Builds both SPAs and bakes them into a Caddy image with `Caddyfile` |
| `Caddyfile` | `APP_HOST` → user SPA + user API, `ADMIN_HOST` → admin SPA + admin API, security headers, caching |
| `docker-compose.prod.yml` | The stack |
| `env.example` | Every setting, no secrets |

## Run

```sh
cp deploy/env.example deploy/.env    # fill in hosts, POSTGRES_PASSWORD, JWT_SECRET, ADMIN_JWT_SECRET
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env up -d --build
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env exec admin /app -promote you@example.com
```

Point both hostnames' DNS at the server before the first start so Caddy can get certificates. The user service runs migrations on start; admin only connects. SMTP and other runtime settings live in `system_config` and are set from the admin app.

Build one image on its own (from the repo root): `docker build -f deploy/backend.Dockerfile --build-arg SVC=user .`, `... SVC=admin .`, `docker build -f deploy/web.Dockerfile .`.

## Environment

| Variable | Service | Default | Notes |
| --- | --- | --- | --- |
| `APP_HOST`, `ADMIN_HOST` | caddy | | Public hostnames. `http://name` serves plain HTTP (local testing only) |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | postgres | `tether` / required / `tether` | Compose builds `DATABASE_URL` from these |
| `DATABASE_URL` | user, admin | local dev DB | Set by compose |
| `APP_ENV` | user, admin | `production` | Anything else relaxes production checks |
| `JWT_SECRET` | user | | Required. 32+ random bytes |
| `ADMIN_JWT_SECRET` | admin | | Required. Different from `JWT_SECRET` |
| `DB_MAX_CONNS` | user | `20` | Pool size. Admin uses `ADMIN_DB_MAX_CONNS` (default 5) |
| `DB_STATEMENT_TIMEOUT`, `DB_IDLE_TX_TIMEOUT` | user, admin | `30s`, `2min` | Postgres `statement_timeout` and `idle_in_transaction_session_timeout` for every pooled session |
| `UPLOAD_DIR` | user | `/data/uploads` | The `uploads` volume. Back it up with the database |
| `NOTIFY_INTERVAL`, `NOTIFY_SETTLE` | user | `1m`, `30s` | Notification worker cadence, quiet time before an email |
| `MAIL_TIMEOUT` | user | `20s` | Longest one SMTP send may take, dial to QUIT |
| `MAIL_RETRY_BASE`, `MAIL_MAX_ATTEMPTS` | user | `30s`, `8` | A failed send retries after `MAIL_RETRY_BASE`, doubling each time (at most 1h), until `MAIL_MAX_ATTEMPTS` sends have failed |
| `PORT` | user, admin | `8090`, `8080` | Set by compose; Caddy expects these |

## Email

Every email (sign-up and sign-in codes, password resets, invites, notifications) is queued in the `outbound_mail` table and sent by a worker in the user service, so requests never wait on SMTP. A sent row is deleted. A row that failed `MAIL_MAX_ATTEMPTS` times stays with `failed_at` and `last_error` set and the worker logs `mail gave up`; to retry it, `update outbound_mail set failed_at = null, attempts = 0, next_attempt_at = now() where id = '...'`.

## Shutdown

On SIGTERM both services stop accepting connections, let in-flight requests finish (up to 20s), close realtime sockets (tabs reconnect) and stop their background workers. Compose gives them 45s (`stop_grace_period`); keep any other orchestrator's grace period above 40s.

## Backups

Two volumes hold state: `pgdata` (`pg_dump` it) and `uploads` (copy the files). `caddy-data` holds certificates and can be recreated.
