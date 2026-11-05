# Postgres server

Postgres 18 in Docker for Taskwell, tuned for 4 vCPU / 4 GB RAM (about 1–1.3 GB of it in use), with nightly backups.
The app (user, admin, MCP, Caddy) is deployed separately and connects to it.

| File | What it is |
| --- | --- |
| `docker-compose.yml` | Postgres with Taskwell's user, database and tuned settings, on the `taskwell` Docker network |
| `backup.sh` | Dumps the database, keeps 14 days, optionally copies each dump off the server |

## Install

On the server, with Docker installed. Copy this folder to `/opt/taskwell-db` (or clone the repo and use it in place).

```sh
sudo mkdir -p /opt/taskwell-db && cd /opt/taskwell-db
# copy docker-compose.yml and backup.sh here, then:
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" | sudo tee .env
sudo chmod 600 .env
sudo docker compose up -d
sudo docker compose ps        # STATUS: healthy
```

Check it:

```sh
sudo docker exec -it taskwell-postgres psql -U taskwell -d taskwell \
  -c "show shared_buffers; select version(); select pg_encoding_to_char(encoding) from pg_database where datname = 'taskwell';"
```

Expect `1GB`, `PostgreSQL 18.x` and `UTF8`.

Don't create tables: Taskwell's `migrate` step (or the user service with `MIGRATE_ON_START=true`) creates them and the
`pg_trgm` extension.

### `database "taskwell" does not exist`

The image creates the database only the first time it starts on an empty volume. If a `pgdata` volume already existed,
it skips that. Create it by hand (empty database, so this is safe):

```sh
sudo docker exec -it taskwell-postgres createdb -U taskwell -O taskwell -E UTF8 -T template0 --locale=C.UTF-8 taskwell
```

Or start over while there's no data: `sudo docker compose down -v && sudo docker compose up -d` (`-v` deletes the volume).

## Connect the app

Password: `sudo cat /opt/taskwell-db/.env`.

- App containers on this server (recommended): add them to the network (`networks: [taskwell]`, and
  `networks: { taskwell: { external: true } }` at the bottom of the app's compose file), then
  ```
  DATABASE_URL=postgres://taskwell:<password>@taskwell-postgres:5432/taskwell?sslmode=disable
  ```
- App running on the server outside Docker:
  ```
  DATABASE_URL=postgres://taskwell:<password>@127.0.0.1:5432/taskwell?sslmode=disable
  ```

Pool sizes that fit `max_connections=60`: `DB_MAX_CONNS=20` (user API, plus its 2 permanent LISTEN connections),
`ADMIN_DB_MAX_CONNS=5`, `TESTER_DB_MAX_CONNS=5` with AI testing. The rest is headroom for backups and you.

Port 5432 is bound to `127.0.0.1` only. Keep it closed in the firewall; an app on another server should reach it over a
private network with TLS (`ssl=on`, `sslmode=require`) and a `pg_hba.conf` rule for that one address.

## Backups

Every night at 03:00, keeping 14 days in `/backups/taskwell`:

```sh
sudo cp /opt/taskwell-db/backup.sh /usr/local/bin/taskwell-backup
echo '0 3 * * * root /usr/local/bin/taskwell-backup >> /var/log/taskwell-backup.log 2>&1' | sudo tee /etc/cron.d/taskwell-backup
sudo /usr/local/bin/taskwell-backup     # run once now to check it works
```

Settings (put them in the cron line, e.g. `0 3 * * * root KEEP_DAYS=30 /usr/local/bin/taskwell-backup ...`):

| Variable | Default | |
| --- | --- | --- |
| `BACKUP_DIR` | `/backups/taskwell` | Where dumps go |
| `KEEP_DAYS` | `14` | Older dumps are deleted |
| `RCLONE_REMOTE` | empty | e.g. `r2:taskwell-backups`: also copies each dump there with `rclone` (install and `rclone config` it first) |

A backup that stays on the same server is lost with the server, so set `RCLONE_REMOTE` (Cloudflare R2, S3, Backblaze) or
copy `/backups/taskwell` off regularly.

### Restore

```sh
sudo docker exec -i taskwell-postgres pg_restore -U taskwell -d taskwell --clean --if-exists < /backups/taskwell/taskwell-2026-11-03-0300.dump
```

Stop the app first so nothing writes during the restore. Test a restore once into a scratch database before you need it:

```sh
sudo docker exec -it taskwell-postgres createdb -U taskwell restore_test
sudo docker exec -i taskwell-postgres pg_restore -U taskwell -d restore_test < /backups/taskwell/<file>.dump
sudo docker exec -it taskwell-postgres psql -U taskwell -d restore_test -c "select count(*) from users;"
sudo docker exec -it taskwell-postgres dropdb -U taskwell restore_test
```

## Day to day

| Task | Command |
| --- | --- |
| SQL shell | `sudo docker exec -it taskwell-postgres psql -U taskwell -d taskwell` |
| Database size | `... -c "select pg_size_pretty(pg_database_size('taskwell'))"` |
| Logs (queries over 500 ms show here) | `sudo docker logs -f taskwell-postgres` |
| Change a setting | Edit `command:` in `docker-compose.yml`, then `sudo docker compose up -d` (restarts in seconds) |
| Minor upgrade (18.x) | `sudo docker compose pull && sudo docker compose up -d` |

A major upgrade (18 → 19) needs a dump and restore into a new volume, or `pg_upgrade`; take a backup first.
