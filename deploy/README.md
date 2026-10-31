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

Point both hostnames' DNS at the server before the first start so Caddy can get certificates. The `migrate` service applies migrations and exits before either service starts (see Migrations). SMTP and other runtime settings live in `system_config` and are set from the admin app; the services cache them for up to 10s, and an admin edit applies at once.

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
| `LOG_FORMAT` | user, admin | `json` in production, else `text` | Logs go to stdout |
| `METRICS_ADDR` | user | `127.0.0.1:9090` | Internal listener for `/metrics` and `/readyz`. Admin uses `ADMIN_METRICS_ADDR` (default `127.0.0.1:9091`). Never publish it |
| `MIGRATE_ON_START` | user | `true` | Compose sets `false`: the `migrate` service runs them |
| `MIGRATE_LOCK_TIMEOUT` | migrate | `10s` | Postgres `lock_timeout` for each transactional migration |
| `SWEEP_INTERVAL` | user | `24h` | Retention sweep cadence |

## Migrations

`tether-user -migrate` applies pending migrations (embedded in the binary) and exits; compose runs it as the `migrate` service on every `up`, and `user` and `admin` wait for it to succeed. In production run it as a release step, then start the services with `MIGRATE_ON_START=false` (without it the user service migrates on start, which is the dev default). The admin service refuses to start until the newest migration it needs is applied.

- Each migration runs in its own transaction with `lock_timeout` (`MIGRATE_LOCK_TIMEOUT`), so one stuck behind a long query fails rather than blocking all traffic on that table. Rerun the release once the table is quiet.
- A file ending `.notx.sql` runs outside a transaction, one statement at a time. Use it for `create index concurrently`, preceded by `drop index concurrently if exists` so a rerun after a failure doesn't keep an invalid index.
- Old instances keep running during a deploy, so a migration must not break the previous release: add columns and tables first, drop or rename only in a later release once nothing reads the old shape (expand, then contract). There are no down migrations; roll back by deploying the previous image, which the expand-only rule keeps compatible.

## Observability

- **Logs**: one JSON line per record on stdout. Every request gets an id (the client's `X-Request-Id` when it's a short token, else a new one), returned in `X-Request-Id` and attached to its error logs. One `request` line per request at info: method, path, status, duration, bytes, user id, request id (`/healthz` skipped).
- **`/readyz`** (on `METRICS_ADDR`): 200 when the database answers within 1s and, for the user service, both LISTEN connections (realtime and collab) are up; 503 with the reason otherwise. `/app -healthcheck` GETs it and exits 0 or 1, which the compose healthchecks use (distroless has no curl). `/healthz` on the public port stays a plain liveness check.
- **`/metrics`** (Prometheus text, on `METRICS_ADDR`): `tether_http_requests_total{class}`, `tether_db_pool_*` (pool size, in use, idle, acquires, waits), `tether_realtime_sockets`, and on the user service `tether_mail_queued`, `tether_mail_failed`, `tether_mail_oldest_seconds` and `tether_pg_notification_queue_usage`. Alert on mail age growing, NOTIFY usage above ~0.5 and readyz failing.

## Retention

The user service sweeps once a day (`SWEEP_INTERVAL`, one instance at a time under an advisory lock): expired refresh tokens and used ones after 14 days, sign-in codes and reset links a day after use or expiry, read notifications after 90 days, given-up email after 30 days, old auth failures and stale presence rows. Uploads that no description, comment, unsaved live edit or avatar references are deleted 24 hours after upload, with their files; so are files on disk without a row (a deleted workspace's). It logs `issue has many live-editing updates` for issues past 1,000 stored updates, which only an editor opening the issue compacts.

## Email

Every email (sign-up and sign-in codes, password resets, invites, notifications) is queued in the `outbound_mail` table and sent by a worker in the user service, so requests never wait on SMTP. A sent row is deleted. A row that failed `MAIL_MAX_ATTEMPTS` times stays with `failed_at` and `last_error` set and the worker logs `mail gave up`; to retry it, `update outbound_mail set failed_at = null, attempts = 0, next_attempt_at = now() where id = '...'`.

## Shutdown

On SIGTERM both services stop accepting connections, let in-flight requests finish (up to 20s), close realtime sockets (tabs reconnect) and stop their background workers. Compose gives them 45s (`stop_grace_period`); keep any other orchestrator's grace period above 40s.

## Backups

Two volumes hold state: `pgdata` (`pg_dump` it) and `uploads` (copy the files). `caddy-data` holds certificates and can be recreated.
