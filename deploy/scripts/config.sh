#!/bin/sh
# Copies every file under deploy/server/ to the same path on the server (deploy/server/etc/caddy/Caddyfile ->
# /etc/caddy/Caddyfile), skipping *.example, and reacts to what changed. Asks for the sudo password once.
#   *.env and .env mode 600, *.sh 755, the rest 644
#   Postgres (opt/taskwell-db) changed or not running -> docker compose up -d, then checks the apps' password works
#   units or env files changed                       -> restarts those services (user first, it runs migrations)
#   Caddyfile changed                                -> validated before it replaces the old one, then caddy reloads
# Usage, from anywhere in the repo: deploy/scripts/config.sh [user@host]
set -eu

SERVER=${1:-devender@ftp.fotospace.co.in}
TREE=$(cd "$(dirname "$0")/../server" && pwd)
for f in etc/taskwell/user.env etc/taskwell/admin.env etc/taskwell/mcp.env opt/taskwell-db/.env; do
  [ -f "$TREE/$f" ] || { echo "no deploy/server/$f"; exit 1; }
done
PW=$(sed -n 's|^DATABASE_URL=postgres://[^:]*:\([^@]*\)@.*|\1|p' "$TREE/etc/taskwell/user.env")
[ "$PW" = "$(sed -n 's/^POSTGRES_PASSWORD=//p' "$TREE/opt/taskwell-db/.env")" ] ||
  { echo "POSTGRES_PASSWORD in opt/taskwell-db/.env must match the password in DATABASE_URL in etc/taskwell/user.env"; exit 1; }

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
tar -czf "$STAGE/server.tgz" -C "$TREE" --exclude='*.example' .

# Runs on the server as root from the upload dir.
cat >"$STAGE/install.sh" <<'EOF'
set -eu
SRC=$(cd "$(dirname "$0")" && pwd)
trap 'rm -rf "$SRC"' EXIT
mkdir "$SRC/root" && tar -xzf "$SRC/server.tgz" -C "$SRC/root" && cd "$SRC/root"
id taskwell >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin taskwell
mkdir -p /etc/taskwell && chmod 700 /etc/taskwell

changed=" "
for f in $(find . -type f | sed 's|^\./||' | sort); do
  cmp -s "$f" "/$f" && continue
  case $f in *.env) mode=600 ;; *.sh) mode=755 ;; *) mode=644 ;; esac
  [ "$f" != etc/caddy/Caddyfile ] || caddy validate --config "$f" --adapter caddyfile >/dev/null
  install -D -m "$mode" "$f" "/$f"
  echo "updated /$f"
  changed="$changed$f "
done
has() { case $changed in *" $1 "*) return 0 ;; esac; return 1; }

if has opt/taskwell-db/docker-compose.yml || has opt/taskwell-db/.env || ! docker inspect taskwell-postgres >/dev/null 2>&1; then
  cd /opt/taskwell-db && docker compose up -d --quiet-pull && cd "$SRC/root"
  i=0; until [ "$(docker inspect -f '{{.State.Health.Status}}' taskwell-postgres)" = healthy ]; do
    i=$((i + 1)); [ $i -lt 60 ] || { docker logs --tail 30 taskwell-postgres; exit 1; }; sleep 2
  done
  PW=$(sed -n 's/^POSTGRES_PASSWORD=//p' /opt/taskwell-db/.env)
  # Inside the container the socket and 127.0.0.1 are trusted, so test on its network address, where the apps'
  # connections (through Docker's port forward) need the password. An older volume keeps its old password: reset it.
  pwok() { PGPASSWORD="$PW" docker exec -e PGPASSWORD taskwell-postgres sh -c 'psql -h "$(hostname -i | cut -d" " -f1)" -U taskwell -d taskwell -qAtc "select 1"' >/dev/null 2>&1; }
  if ! pwok; then
    echo "alter user taskwell password :'pw'" | docker exec -i taskwell-postgres psql -U taskwell -d taskwell -q -v ON_ERROR_STOP=1 -v pw="$PW"
    echo "postgres password reset to the one in .env"
  fi
  pwok || { echo "the postgres password still doesn't work"; exit 1; }
  echo "postgres ok"
fi

systemctl daemon-reload
restart=""
for app in user admin mcp; do
  if { has "etc/systemd/system/taskwell-$app.service" || has "etc/taskwell/$app.env"; } && [ -x "/opt/taskwell/taskwell-$app" ]; then
    restart="$restart taskwell-$app"
  fi
done
# One restart call: the units' After= start user first, so it applies migrations before admin and mcp.
if [ -n "$restart" ]; then
  systemctl enable $restart >/dev/null
  systemctl restart $restart
  echo "restarted$restart"
fi

if has etc/caddy/Caddyfile; then systemctl reload caddy && echo "reloaded caddy"; fi
grep -qs '^R2_BUCKET=.' /etc/taskwell-backup.env ||
  echo "warning: backups stay on this server; fill in deploy/server/etc/taskwell-backup.env (deploy/README.md)"
echo "config up to date"
EOF

DIR=/tmp/taskwell-config
echo "sending deploy/server to $SERVER"
ssh "$SERVER" "rm -rf $DIR && mkdir -m 700 $DIR"
scp -q "$STAGE"/* "$SERVER:$DIR/"
ssh -t "$SERVER" "sudo sh $DIR/install.sh"
