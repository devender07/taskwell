#!/bin/sh
# One-time setup of a fresh Ubuntu server; safe to run again. Asks for the sudo password once.
#   updates, ufw (22/80/443), Docker, Caddy, rclone (backups to R2), you in the docker group.
# Then deploy/scripts/config.sh puts deploy/server/ in place and starts Postgres (deploy/README.md).
# Usage, from anywhere in the repo: deploy/scripts/server-setup.sh [user@host]
set -eu

SERVER=${1:-devender@ftp.fotospace.co.in}
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

cat >"$STAGE/install.sh" <<'EOF'
set -eu
SRC=$(cd "$(dirname "$0")" && pwd)
trap 'rm -rf "$SRC"' EXIT
export DEBIAN_FRONTEND=noninteractive
apt() { command apt-get -o DPkg::Lock::Timeout=900 -y "$@"; } # waits out unattended-upgrades on a new server

echo "== packages"
apt update -q && apt upgrade -q && apt install -q ufw curl gpg rclone debian-keyring debian-archive-keyring apt-transport-https

echo "== firewall"
ufw allow OpenSSH >/dev/null && ufw allow 80/tcp >/dev/null && ufw allow 443 >/dev/null && ufw --force enable

echo "== docker"
command -v docker >/dev/null || curl -fsSL https://get.docker.com | sh
systemctl enable --now docker
[ -n "${SUDO_USER:-}" ] && usermod -aG docker "$SUDO_USER" # deploy/scripts/system-config.sh runs docker without sudo

echo "== caddy"
if ! command -v caddy >/dev/null; then
  curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt >/etc/apt/sources.list.d/caddy-stable.list
  chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
  apt update -q && apt install -q caddy
fi

echo "server ready${SUDO_USER:+ (log in again for the docker group)}"
[ -f /var/run/reboot-required ] && echo "a reboot is pending: sudo reboot" || true
EOF

DIR=/tmp/taskwell-setup
echo "setting up $SERVER"
ssh "$SERVER" "rm -rf $DIR && mkdir -m 700 $DIR"
scp -q "$STAGE"/* "$SERVER:$DIR/"
ssh -t "$SERVER" "sudo sh $DIR/install.sh"
