#!/bin/sh
# One-off: renames the server's database and role from tether to taskwell, keeping the data and the password.
# Stops the services first (a database with connections can't be renamed) and starts them again after; install the
# env files with the new DATABASE_URL (deploy/config.sh) right after this.
# Usage, from your machine: deploy/postgres/rename-tether.sh [user@host]   (default devender@ftp.fotospace.co.in)
set -eu

if [ "${1:-}" != --here ]; then
  SERVER=${1:-devender@ftp.fotospace.co.in}
  scp -q "$0" "$SERVER:/tmp/taskwell-rename.sh"
  exec ssh -t "$SERVER" "sudo sh /tmp/taskwell-rename.sh --here; rm -f /tmp/taskwell-rename.sh"
fi

C=$(docker ps --format '{{.Names}}' | grep -E '^(tether|taskwell)-postgres$' | head -1)
[ -n "$C" ] || { echo "no tether-postgres or taskwell-postgres container running"; exit 1; }
sql() { docker exec -i "$C" psql -v ON_ERROR_STOP=1 -qAt -d postgres -U "$@"; }

if ! sql tether -c 'select 1' >/dev/null 2>&1; then
  sql taskwell -c "select 1 from pg_database where datname = 'taskwell'" | grep -q 1 && { echo "already renamed"; exit 0; }
  echo "no tether role in $C"; exit 1
fi
# Renaming a role clears an md5 password; a SCRAM one (the default since Postgres 14) survives.
[ "$(sql tether -c "select rolpassword like 'SCRAM%' from pg_authid where rolname = 'tether'")" = t ] ||
  { echo "tether's password is md5: rename by hand and set the password again"; exit 1; }

UNITS=$(systemctl list-units --plain --no-legend --type=service 'tether-*' 'taskwell-*' | awk '{print $1}')
[ -n "$UNITS" ] && systemctl stop $UNITS
# The session user can't rename itself, so a throwaway superuser does it.
sql tether -c 'create role taskwell_rename superuser login'
sql taskwell_rename -c "select pg_terminate_backend(pid) from pg_stat_activity where datname = 'tether'" >/dev/null
sql taskwell_rename -c 'alter database tether rename to taskwell' -c 'alter role tether rename to taskwell'
sql taskwell -c 'drop role taskwell_rename'
echo "renamed: database and role are taskwell in $C"
[ -n "$UNITS" ] && echo "stopped: $UNITS (start them after deploy/config.sh puts the new DATABASE_URL in place)"
