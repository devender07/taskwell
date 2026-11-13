# Updating the server

How to ship a new version to the running server. Setting up a new server is in [README.md](README.md). Every command
runs from your machine at the repo root and takes `user@host` as its last argument (default
`devender@ftp.fotospace.co.in`). The scripts ask for the sudo password once.

## 1. Before you deploy

```sh
git status                                     # clean, on the commit you mean to ship
cd backend && make vet test && cd ..
cd frontend && npm run build && cd ..
git diff --stat <last deployed commit> -- backend/user/internal/db/migrations deploy/server
```

The last command shows what the release changes beyond code:

- **Migrations up to 048 are squashed into `001_schema.sql`.** A server that hasn't run `048_feedback_url.sql` must
  ship commit `b1c0096` first; a newer release refuses to start on it and says so.
- **New migrations** run by themselves when the user API starts (step 3). Read them first: a migration that
  rewrites or drops data can't be undone except from a backup.
- **New `system_config` keys** come in with their migration, seeded with defaults. Set real values in the admin app's
  Config page after the deploy.
- **Changed files under `deploy/server/`** (env files, units, Caddyfile, cron, Postgres) need `deploy/scripts/config.sh` (step 4).

## 2. Back up the database

The nightly backup runs at 03:00. Take a fresh one before any release with migrations:

```sh
ssh -t devender@<host> 'sudo /opt/taskwell-db/backup.sh'     # prints: backup ok: taskwell-<date>.dump (…) in r2:…
```

## 3. Deploy the backend, user API first

```sh
deploy/scripts/build.sh prod user backend  && deploy/scripts/send.sh prod user backend
deploy/scripts/build.sh prod admin backend && deploy/scripts/send.sh prod admin backend
deploy/scripts/build.sh prod mcp backend   && deploy/scripts/send.sh prod mcp backend
```

The user API goes first because its unit applies pending migrations (`ExecStartPre=... -migrate`) before it starts,
and the admin API and MCP server expect the new schema. Each migration runs in its own transaction and waits at most
`MIGRATE_LOCK_TIMEOUT` (10s) for a lock. If one fails, the user API doesn't start, and the old schema is still
there for that migration and every one after it. Read the logs (step 6), fix the migration, then deploy again.

Only deploy the services whose code changed. Code in `backend/shared` is in all three.

## 4. Config changes, when there are any

```sh
deploy/scripts/config.sh                              # deploy/server/ into place; restarts only what changed
deploy/scripts/system-config.sh                       # only if deploy/system.json changed
```

`system-config.sh` overwrites the keys in the file with the file's values. Keys changed in the admin app since then
are overwritten too, so prefer the Config page for a single value.

## 5. Deploy the frontends, after the API

```sh
deploy/scripts/frontend.sh user
deploy/scripts/frontend.sh admin
```

They go after the backend so a new page never calls an endpoint the server doesn't have yet. Both go to Cloudflare
Workers (log in once with `npx wrangler@4 login`). If `cdn/` changed, run `npx wrangler@4 deploy` from `cdn/`.

## 6. Check

```sh
for h in api admin-api mcp; do curl -s -o /dev/null -w "$h %{http_code}\n" https://$h.taskwell.dev/healthz; done   # 200 each
ssh devender@<host> 'systemctl is-active taskwell-user taskwell-admin taskwell-mcp caddy docker'
ssh -t devender@<host> 'sudo journalctl -u taskwell-user -n 50 --no-pager'           # when one isn't active
ssh devender@<host> "docker exec taskwell-postgres psql -U taskwell -d taskwell -Atc \
  'select version from schema_migrations order by version desc limit 3'"             # the newest migration is there
```

Then sign in at taskwell.dev and click through what the release changed.

## Rolling back

**Code only (no new migrations):** check out the last good commit and deploy it the same way:

```sh
git switch --detach <last good commit>
deploy/scripts/build.sh prod user backend && deploy/scripts/send.sh prod user backend     # and admin, mcp, frontends as needed
git switch -
```

**With migrations:** an old binary runs against the new schema as long as the migrations only added things
(tables, columns, keys). Roll back the code as above and leave the schema. If a migration changed or dropped data,
stop the services and restore the backup from step 2:

```sh
ssh -t devender@<host> 'sudo /opt/taskwell-db/backup.sh list'                       # pick the dump
ssh -t devender@<host> 'sudo systemctl stop taskwell-user taskwell-admin taskwell-mcp'
ssh -t devender@<host> "sudo sh -c '/opt/taskwell-db/backup.sh get taskwell-<date>.dump /tmp && \
  docker exec -i taskwell-postgres pg_restore -U taskwell -d taskwell --clean --if-exists < /tmp/taskwell-<date>.dump \
  && rm /tmp/taskwell-<date>.dump'"
# then deploy the last good commit as above, which starts them again
```

A restore loses everything written since the backup. The restore drill is in `docs/compliance/runbooks.md`.
