#!/bin/sh
# Sends a build from deploy/build.sh to the server, installs it and restarts the service.
# Usage, from anywhere in the repo: deploy/send.sh ENV APP PART [user@host]
#   same ENV APP PART as build.sh; e.g. deploy/build.sh prod user all && deploy/send.sh prod user all
# On the server: binary in /opt/taskwell, frontend in /var/www/taskwell/<app>. Units, env files and the Caddyfile
# come from deploy/config.sh, run it first. Asks for the sudo password once.
set -eu

usage() { echo "usage: $0 dev|prod user|admin|mcp frontend|backend|all [user@host]"; exit 1; }
[ $# -eq 3 ] || [ $# -eq 4 ] || usage
ENV=$1 APP=$2 PART=$3 SERVER=${4:-devender@ftp.fotospace.co.in}
case $ENV in dev|prod) ;; *) usage ;; esac
case $APP in user|admin|mcp) ;; *) usage ;; esac
[ "$APP" != mcp ] || [ "$PART" = backend ] || { echo "mcp has no frontend: use PART backend"; exit 1; }
case $PART in frontend|backend|all) ;; *) usage ;; esac

HERE=$(cd "$(dirname "$0")" && pwd)
BUILD=$HERE/../dist/$ENV/$APP
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
missing() { echo "no $1, run: deploy/build.sh $ENV $APP $PART"; exit 1; }

if [ "$PART" != backend ]; then
  [ -f "$BUILD/web/index.html" ] || missing "$BUILD/web"
  tar -czf "$STAGE/web.tar.gz" -C "$BUILD/web" .
fi
if [ "$PART" != frontend ]; then
  [ -x "$BUILD/taskwell-$APP" ] || missing "$BUILD/taskwell-$APP"
  cp "$BUILD/taskwell-$APP" "$STAGE/"
fi

# Runs on the server as root from the upload dir. New files go in beside the old and are swapped in with mv.
cat >"$STAGE/install.sh" <<'EOF'
set -eu
APP=$1 SRC=$(cd "$(dirname "$0")" && pwd)
if [ -f "$SRC/web.tar.gz" ]; then
  WEB=/var/www/taskwell/$APP
  rm -rf "$WEB.new" "$WEB.old" && mkdir -p "$WEB.new"
  tar -xzf "$SRC/web.tar.gz" -C "$WEB.new"
  [ -d "$WEB" ] && mv "$WEB" "$WEB.old"
  mv "$WEB.new" "$WEB" && rm -rf "$WEB.old"
  echo "frontend installed in $WEB"
fi
if [ -f "$SRC/taskwell-$APP" ]; then
  [ -f "/etc/systemd/system/taskwell-$APP.service" ] || { echo "no taskwell-$APP unit, run deploy/config.sh first"; exit 1; }
  mkdir -p /opt/taskwell
  install -m 755 "$SRC/taskwell-$APP" "/opt/taskwell/taskwell-$APP.new"
  mv "/opt/taskwell/taskwell-$APP.new" "/opt/taskwell/taskwell-$APP"
  systemctl enable "taskwell-$APP" >/dev/null
  systemctl restart "taskwell-$APP"
  sleep 2
  systemctl --no-pager --lines=5 status "taskwell-$APP" | grep -E '●|Active:'
fi
rm -rf "$SRC"
EOF

DIR=/tmp/taskwell-deploy-$APP
echo "sending $ENV $APP $PART to $SERVER"
ssh "$SERVER" "rm -rf $DIR && mkdir -m 700 $DIR"
scp -q "$STAGE"/* "$SERVER:$DIR/"
ssh -t "$SERVER" "sudo sh $DIR/install.sh $APP"
