#!/bin/sh
# Builds the user, admin and MCP binaries and sends them, the systemd units, the env files and install.sh to the
# server. Then on the server: sudo sh /tmp/tether-deploy/install.sh   (see README.md)
# Usage, from anywhere in the repo: deploy/systemd/deploy.sh [user@host]
set -eu

SERVER=${1:-devender@185.67.20.135}
ARCH=${GOARCH:-amd64}
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=$HERE/../../backend/dist

cd "$HERE/../../backend"
mkdir -p "$OUT"
for s in user admin mcp; do
  echo "building tether-$s ($ARCH)"
  CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH go build -trimpath -ldflags='-s -w' -o "$OUT/tether-$s" "./$s/cmd/$s"
done

echo "sending to $SERVER:/tmp/tether-deploy"
ssh "$SERVER" 'mkdir -p /tmp/tether-deploy && chmod 700 /tmp/tether-deploy'
scp "$OUT"/tether-user "$OUT"/tether-admin "$OUT"/tether-mcp \
  "$HERE"/tether-*.service "$HERE"/user.env "$HERE"/admin.env "$HERE"/mcp.env "$HERE"/install.sh \
  "$SERVER":/tmp/tether-deploy/

echo
echo "done. Now on the server:"
echo "  ssh $SERVER"
echo "  sudo sh /tmp/tether-deploy/install.sh"
