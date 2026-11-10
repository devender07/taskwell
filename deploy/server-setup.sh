#!/bin/sh
# One-time setup of a fresh Ubuntu server; safe to run again. Asks for the sudo password once.
#   updates, ufw (22/80/443), Docker, Caddy, Postgres in Docker at /opt/taskwell-db (password from systemd/user.env,
#   checked over TCP and set to match if the data volume is older), nightly backup at 03:00, you in the docker group.
# Then: deploy/config.sh, send user, deploy/system-config.sh, send admin and mcp (deploy/README.md).
# Usage, from anywhere in the repo: deploy/server-setup.sh [user@host]
set -eu

SERVER=${1:-devender@ftp.fotospace.co.in}
HERE=$(cd "$(dirname "$0")" && pwd)
PW=$(sed -n 's|^DATABASE_URL=postgres://[^:]*:\([^@]*\)@.*|\1|p' "$HERE/systemd/user.env")
[ -n "$PW" ] || { echo "no password in DATABASE_URL in deploy/systemd/user.env"; exit 1; }
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
cp "$HERE/postgres/docker-compose.yml" "$HERE/postgres/backup.sh" "$STAGE/"
printf 'POSTGRES_PASSWORD=%s\n' "$PW" >"$STAGE/db.env"

cat >"$STAGE/install.sh" <<'EOF'
set -eu
SRC=$(cd "$(dirname "$0")" && pwd)
trap 'rm -rf "$SRC"' EXIT
export DEBIAN_FRONTEND=noninteractive
apt() { command apt-get -o DPkg::Lock::Timeout=900 -y "$@"; } # waits out unattended-upgrades on a new server

echo "== packages"
apt update -q && apt upgrade -q && apt install -q ufw curl gpg debian-keyring debian-archive-keyring apt-transport-https

echo "== firewall"
ufw allow OpenSSH >/dev/null && ufw allow 80/tcp >/dev/null && ufw allow 443 >/dev/null && ufw --force enable

echo "== docker"
command -v docker >/dev/null || curl -fsSL https://get.docker.com | sh
systemctl enable --now docker
[ -n "${SUDO_USER:-}" ] && usermod -aG docker "$SUDO_USER" # deploy/system-config.sh runs docker without sudo

echo "== caddy"
if ! command -v caddy >/dev/null; then
  curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt >/etc/apt/sources.list.d/caddy-stable.list
  chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
  apt update -q && apt install -q caddy
fi

echo "== postgres"
DB=/opt/taskwell-db
mkdir -p "$DB"
install -m 644 "$SRC/docker-compose.yml" "$DB/"
install -m 755 "$SRC/backup.sh" "$DB/"
install -m 600 "$SRC/db.env" "$DB/.env"
cd "$DB" && docker compose up -d --quiet-pull
i=0; until [ "$(docker inspect -f '{{.State.Health.Status}}' taskwell-postgres)" = healthy ]; do
  i=$((i + 1)); [ $i -lt 60 ] || { docker logs --tail 30 taskwell-postgres; exit 1; }; sleep 2
done
PW=$(sed -n 's/^POSTGRES_PASSWORD=//p' "$DB/.env")
# Inside the container the socket and 127.0.0.1 are trusted, so test on its network address, where the apps' connections
# (through Docker's port forward) need the password. An older volume keeps its old password: set it to the apps' one.
pwok() { PGPASSWORD="$PW" docker exec -e PGPASSWORD taskwell-postgres sh -c 'psql -h "$(hostname -i | cut -d" " -f1)" -U taskwell -d taskwell -qAtc "select 1"' >/dev/null 2>&1; }
if ! pwok; then
  echo "alter user taskwell password :'pw'" | docker exec -i taskwell-postgres psql -U taskwell -d taskwell -q -v ON_ERROR_STOP=1 -v pw="$PW"
  echo "password reset to the one in user.env"
fi
pwok || { echo "the password in user.env still doesn't work"; exit 1; }
echo "postgres ok, password matches user.env"

echo "== backups"
echo "0 3 * * * root $DB/backup.sh >>/var/log/taskwell-backup.log 2>&1" >/etc/cron.d/taskwell-backup
grep -qs '^RCLONE_REMOTE=.' /etc/taskwell-backup.env ||
  echo "warning: backups stay on this server; set RCLONE_REMOTE in /etc/taskwell-backup.env (deploy/README.md)"

echo "server ready${SUDO_USER:+ (log in again for the docker group)}"
[ -f /var/run/reboot-required ] && echo "a reboot is pending: sudo reboot" || true
EOF

DIR=/tmp/taskwell-setup
echo "setting up $SERVER"
ssh "$SERVER" "rm -rf $DIR && mkdir -m 700 $DIR"
scp -q "$STAGE"/* "$SERVER:$DIR/"
ssh -t "$SERVER" "sudo sh $DIR/install.sh"
