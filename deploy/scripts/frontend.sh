#!/bin/sh
# Builds a frontend for production and deploys it to its Cloudflare Worker (frontend/<app>/wrangler.toml).
# Usage, from anywhere in the repo: deploy/scripts/frontend.sh user|admin   (needs `npx wrangler@4 login` once)
set -eu

case ${1:-} in user|admin) APP=$1 ;; *) echo "usage: $0 user|admin"; exit 1 ;; esac
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
"$ROOT/deploy/scripts/build.sh" prod "$APP" frontend
cd "$ROOT/frontend/$APP"
npx -y wrangler@4 deploy
