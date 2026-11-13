#!/bin/sh
# Restores the user data from a backup in R2: the newest, or FILE (a name from `backup.sh list`). Everything but
# system_config, schema_migrations, the admin tables and the workspace defaults comes back; those stay as they are.
# The database must have the dump's schema (it does once the user API has run the same migrations).
# The user tables must be empty, or pass --clear: it backs up to R2 first, then empties them as part of the restore.
# The services stop meanwhile, and it's one transaction: a failure leaves the database as it was.
#   sudo /opt/taskwell-db/restore.sh [--clear] [FILE]
set -eu
[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }
CLEAR=""
[ "${1:-}" = --clear ] && { CLEAR=1; shift; }
B=/opt/taskwell-db/backup.sh
UNITS="taskwell-user taskwell-admin taskwell-mcp"
KEPT="'system_config', 'schema_migrations', 'admins', 'admin_mfa', 'admin_sessions', 'admin_audit'"
sql() { docker exec -i taskwell-postgres psql -U taskwell -d taskwell -v ON_ERROR_STOP=1 -qAt "$@"; }

F=${1:-$("$B" list 2>/dev/null | sort -k2,3 | tail -1 | awk '{print $NF}')}
[ -n "$F" ] || { echo "no backups in R2"; exit 1; }
USERS=$(sql -c 'select count(*) from users' </dev/null)
[ "$USERS" = 0 ] || [ -n "$CLEAR" ] || { echo "the database has $USERS users: pass --clear to replace them (it backs up first)"; exit 1; }
printf 'Restore %s%s? Type restore: ' "$F" "${CLEAR:+, replacing the $USERS users there now}"
read -r OK
[ "$OK" = restore ] || { echo "stopped"; exit 1; }

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"; docker exec taskwell-postgres rm -f /tmp/restore.list' EXIT
"$B" get "$F" "$TMP" 2>&1 | grep -v 'rclone.conf" not found' || true
[ -s "$TMP/$F" ] || { echo "couldn't download $F"; exit 1; }
# The restore as SQL first, so a failure here never commits half of it.
docker exec -i taskwell-postgres pg_restore --list <"$TMP/$F" |
  grep -vE '(TABLE DATA|SEQUENCE SET) public (system_config|schema_migrations|admin|default_)' >"$TMP/restore.list"
docker cp -q "$TMP/restore.list" taskwell-postgres:/tmp/restore.list
docker exec -i taskwell-postgres pg_restore -f - --data-only --disable-triggers -L /tmp/restore.list <"$TMP/$F" >"$TMP/restore.sql"
[ -z "$CLEAR" ] || "$B"

systemctl stop $UNITS
trap 'rm -rf "$TMP"; docker exec taskwell-postgres rm -f /tmp/restore.list; systemctl start $UNITS' EXIT
{
  echo 'begin;'
  # Empty either way: with --clear it drops today's rows, without it it's a no-op on empty tables. The workspace
  # audit log refuses truncates, so its guard is off for the statement.
  echo 'alter table workspace_audit disable trigger workspace_audit_no_truncate;'
  echo "do \$\$ declare t text; begin
    select string_agg(format('%I', tablename), ', ') into t from pg_tables where schemaname = 'public'
      and tablename not in ($KEPT) and tablename not like 'default\_%';
    execute 'truncate ' || t || ' restart identity cascade';
  end \$\$;"
  echo 'alter table workspace_audit enable trigger workspace_audit_no_truncate;'
  cat "$TMP/restore.sql"
  echo 'commit;'
} | sql >/dev/null
echo "restored $F: $(sql -c "select (select count(*) from users) || ' users, ' || (select count(*) from workspaces) || ' workspaces, ' || (select count(*) from issues_all) || ' issues'" </dev/null)"
