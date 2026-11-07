#!/bin/sh
# Sends the server config to its places and restarts only what changed. Asks for the sudo password once.
#   systemd/*.service  -> /etc/systemd/system/
#   systemd/*.env      -> /etc/taskwell/          (secrets, mode 600)
#   config/Caddyfile   -> /etc/caddy/Caddyfile    (validated before it replaces the old one)
# Usage, from anywhere in the repo: deploy/config.sh [user@host]
set -eu

SERVER=${1:-devender@ftp.fotospace.co.in}
HERE=$(cd "$(dirname "$0")" && pwd)
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
cp "$HERE"/systemd/*.service "$HERE"/systemd/*.env "$HERE/config/Caddyfile" "$STAGE/"

# Runs on the server as root from the upload dir.
cat >"$STAGE/install.sh" <<'EOF'
set -eu
SRC=$(cd "$(dirname "$0")" && pwd)
trap 'rm -rf "$SRC"' EXIT
same() { cmp -s "$1" "$2"; }
id taskwell >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin taskwell
mkdir -p /etc/taskwell && chmod 700 /etc/taskwell

restart=""
for unit in "$SRC"/taskwell-*.service; do
  name=$(basename "$unit" .service) app=${name#taskwell-}
  changed=""
  same "$unit" "/etc/systemd/system/$name.service" || { install -m 644 "$unit" /etc/systemd/system/; changed=1; }
  if [ -f "$SRC/$app.env" ] && ! same "$SRC/$app.env" "/etc/taskwell/$app.env"; then
    install -m 600 "$SRC/$app.env" "/etc/taskwell/$app.env"; changed=1
  fi
  if [ -n "$changed" ]; then
    echo "updated $name"
    if [ -x "/opt/taskwell/$name" ]; then restart="$restart $name"; fi
  fi
done
systemctl daemon-reload
# One restart call: the units' After= start user first, so it applies migrations before admin and mcp.
if [ -n "$restart" ]; then
  systemctl enable $restart >/dev/null
  systemctl restart $restart
  echo "restarted$restart"
fi

if ! same "$SRC/Caddyfile" /etc/caddy/Caddyfile; then
  caddy validate --config "$SRC/Caddyfile" --adapter caddyfile >/dev/null
  install -m 644 "$SRC/Caddyfile" /etc/caddy/Caddyfile
  systemctl reload caddy
  echo "reloaded caddy"
fi
echo "config up to date"
EOF

DIR=/tmp/taskwell-config
echo "sending config to $SERVER"
ssh "$SERVER" "rm -rf $DIR && mkdir -m 700 $DIR"
scp -q "$STAGE"/* "$SERVER:$DIR/"
ssh -t "$SERVER" "sudo sh $DIR/install.sh"
