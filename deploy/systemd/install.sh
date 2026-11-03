#!/bin/sh
# Runs on the server as root, after deploy.sh sent the files to /tmp/tether-deploy: installs the binaries and units,
# the env files on the first install (later runs keep /etc/tether as it is; pass --env to replace them), then restarts
# the services, user first so it applies the migrations.
set -eu

SRC=/tmp/tether-deploy
[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }

id tether >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin tether
mkdir -p /opt/tether /etc/tether
chmod 700 /etc/tether

install -m 755 "$SRC"/tether-user "$SRC"/tether-admin "$SRC"/tether-mcp /opt/tether/
install -m 644 "$SRC"/tether-user.service "$SRC"/tether-admin.service "$SRC"/tether-mcp.service /etc/systemd/system/

for s in user admin mcp; do
  if [ ! -f /etc/tether/$s.env ] || [ "${1:-}" = --env ]; then
    install -m 600 "$SRC"/$s.env /etc/tether/$s.env
    echo "installed /etc/tether/$s.env"
  fi
done
if grep -q '<POSTGRES_PASSWORD>' /etc/tether/user.env /etc/tether/admin.env; then
  echo "set the database password in /etc/tether/user.env and admin.env (DATABASE_URL), then run this again"
  exit 1
fi

systemctl daemon-reload
systemctl enable tether-user tether-admin tether-mcp >/dev/null
systemctl restart tether-user # applies migrations first
systemctl restart tether-admin tether-mcp
rm -rf "$SRC"

sleep 2
systemctl --no-pager --lines=0 status tether-user tether-admin tether-mcp | grep -E '●|Active:'
code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/healthz || true)
echo "user API /healthz: $code"
