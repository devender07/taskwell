#!/bin/sh
# Creates an admin account (or resets its password) in the admins table, apart from app users, and prints their
# authenticator setup key and recovery codes once, when they have none yet. One script for both places; it asks for the password so it stays out of shell
# history and the process list.
#   local dev (make up running):  deploy/create-admin.sh you@example.com ['Name']
#   server (after scp to /tmp):   sudo sh /tmp/create-admin.sh you@example.com ['Name']
set -eu

[ $# -ge 1 ] || { echo "usage: $0 EMAIL [NAME]"; exit 1; }
BIN=/opt/taskwell/taskwell-admin
if [ -x "$BIN" ] && [ "$(id -u)" != 0 ]; then echo "run with sudo"; exit 1; fi

trap 'stty echo 2>/dev/null || true' EXIT
printf 'Password for %s: ' "$1"; stty -echo 2>/dev/null || true; read -r PW; stty echo 2>/dev/null || true; echo
[ ${#PW} -ge 8 ] || { echo "password must be at least 8 characters"; exit 1; }
[ ${#PW} -le 72 ] || { echo "password is too long"; exit 1; }

# The password goes in on stdin, dollar-quoted with a random tag so any character in it is literal.
TAG=pw$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')
printf 'create extension if not exists pgcrypto;
insert into admins (name, email, password_hash)
values (:'"'"'name'"'"', lower(trim(:'"'"'email'"'"')), crypt($%s$%s$%s$, gen_salt('"'"'bf'"'"', 10)))
on conflict (email) do update set password_hash = excluded.password_hash;\n' \
  "$TAG" "$PW" "$TAG" |
  docker exec -i taskwell-postgres psql -U taskwell -d taskwell -v ON_ERROR_STOP=1 -q -v email="$1" -v name="${2:-Admin}"

if [ -x "$BIN" ]; then
  systemd-run --pty --wait --quiet --uid=taskwell -p EnvironmentFile=/etc/taskwell/admin.env "$BIN" -enrol-mfa "$1"
else
  cd "$(dirname "$0")/../backend" && go run ./admin/cmd/admin -enrol-mfa "$1"
fi
