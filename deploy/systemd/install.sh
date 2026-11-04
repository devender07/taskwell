#!/bin/sh
# Runs on the server as root, after deploy.sh sent the files to /tmp/taskwell-deploy: installs the binaries and units,
# the env files on the first install (later runs keep /etc/taskwell as it is; pass --env to replace them), then restarts
# the services, user first so it applies the migrations.
set -eu

SRC=/tmp/taskwell-deploy
[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }

id taskwell >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin taskwell
mkdir -p /opt/taskwell /etc/taskwell
chmod 700 /etc/taskwell

install -m 755 "$SRC"/taskwell-user "$SRC"/taskwell-admin "$SRC"/taskwell-mcp /opt/taskwell/
install -m 644 "$SRC"/taskwell-user.service "$SRC"/taskwell-admin.service "$SRC"/taskwell-mcp.service /etc/systemd/system/

for s in user admin mcp; do
  if [ ! -f /etc/taskwell/$s.env ] || [ "${1:-}" = --env ]; then
    install -m 600 "$SRC"/$s.env /etc/taskwell/$s.env
    echo "installed /etc/taskwell/$s.env"
  fi
done
if grep -q '<POSTGRES_PASSWORD>' /etc/taskwell/user.env /etc/taskwell/admin.env; then
  echo "set the database password in /etc/taskwell/user.env and admin.env (DATABASE_URL), then run this again"
  exit 1
fi

systemctl daemon-reload
systemctl enable taskwell-user taskwell-admin taskwell-mcp >/dev/null
systemctl restart taskwell-user # applies migrations first
systemctl restart taskwell-admin taskwell-mcp
rm -rf "$SRC"

sleep 2
systemctl --no-pager --lines=0 status taskwell-user taskwell-admin taskwell-mcp | grep -E '●|Active:'
code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8090/healthz || true)
echo "user API /healthz: $code"
