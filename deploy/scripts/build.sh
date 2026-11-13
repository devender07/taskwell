#!/bin/sh
# Builds one app for one environment into dist/<env>/<app>/ at the repo root.
# Usage, from anywhere in the repo: deploy/scripts/build.sh ENV APP PART
#   ENV   dev | prod     picks frontend/<app>/.env.development or .env.production
#   APP   user | admin | mcp (mcp: backend only)
#   PART  frontend | backend | all
# e.g. deploy/scripts/build.sh dev user frontend, deploy/scripts/build.sh prod admin all
set -eu

usage() { echo "usage: $0 dev|prod user|admin|mcp frontend|backend|all"; exit 1; }
[ $# -eq 3 ] || usage
ENV=$1 APP=$2 PART=$3
case $ENV in dev) MODE=development ;; prod) MODE=production ;; *) usage ;; esac
case $APP in user|admin|mcp) ;; *) usage ;; esac
[ "$APP" != mcp ] || [ "$PART" = backend ] || { echo "mcp has no frontend: use PART backend"; exit 1; }
case $PART in frontend|backend|all) ;; *) usage ;; esac

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=$ROOT/dist/$ENV/$APP
mkdir -p "$OUT"

if [ "$PART" != backend ]; then
  echo "building $APP frontend ($MODE)"
  cd "$ROOT/frontend/$APP"
  npx vite build --mode "$MODE" --outDir "$OUT/web" --emptyOutDir
  npx tsc --noEmit
fi

if [ "$PART" != frontend ]; then
  ARCH=${GOARCH:-amd64}
  echo "building taskwell-$APP backend (linux/$ARCH)"
  cd "$ROOT/backend"
  CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH go build -trimpath -ldflags='-s -w' -o "$OUT/taskwell-$APP" "./$APP/cmd/$APP"
fi

echo "done: $OUT"
