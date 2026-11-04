#!/bin/sh
# Builds the user and admin frontends, packs them into backend/dist/taskwell-web.tar.gz and sends it to the server.
# Then on the server: sudo mkdir -p /var/www/taskwell && sudo tar -xzf /tmp/taskwell-web.tar.gz -C /var/www/taskwell
# Usage, from anywhere in the repo: deploy/systemd/deploy-web.sh [user@host]
set -eu

SERVER=${1:-devender@185.67.20.135}
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=$HERE/../../backend/dist
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

cd "$HERE/../../frontend"
npm run build
cp -r user/dist "$STAGE/user"
cp -r admin/dist "$STAGE/admin"
mkdir -p "$OUT"
tar -czf "$OUT/taskwell-web.tar.gz" -C "$STAGE" user admin

scp "$OUT/taskwell-web.tar.gz" "$SERVER":/tmp/
echo
echo "sent. Now on the server:"
echo "  sudo rm -rf /var/www/taskwell/user /var/www/taskwell/admin"
echo "  sudo mkdir -p /var/www/taskwell && sudo tar -xzf /tmp/taskwell-web.tar.gz -C /var/www/taskwell && rm /tmp/taskwell-web.tar.gz"
