#!/bin/sh
# Runs on the server as root: creates (or resets the password of) a verified user, then makes them a platform admin.
# Prints the authenticator setup key and recovery codes once.
#   sudo sh create-admin.sh you@example.com 'password' ['Name']
set -eu

[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }
[ $# -ge 2 ] || { echo "usage: $0 EMAIL PASSWORD [NAME]"; exit 1; }

docker exec -i tether-postgres psql -U tether -d tether -v ON_ERROR_STOP=1 -q \
  -v email="$1" -v pw="$2" -v name="${3:-Admin}" <<'SQL'
create extension if not exists pgcrypto;
insert into users (name, email, password_hash, email_verified_at)
values (:'name', lower(trim(:'email')), crypt(:'pw', gen_salt('bf', 10)), now())
on conflict (email) do update
  set password_hash = excluded.password_hash, email_verified_at = coalesce(users.email_verified_at, now());
SQL

systemd-run --pty --wait --quiet --uid=tether -p EnvironmentFile=/etc/tether/admin.env \
  /opt/tether/tether-admin -promote "$1"
