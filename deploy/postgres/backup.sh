#!/bin/sh
# Nightly dump of the tether database, kept for KEEP_DAYS days. Run from cron (see README.md).
# Set RCLONE_REMOTE (e.g. r2:tether-backups) to also copy each dump off the server with rclone.
set -eu

DIR=${BACKUP_DIR:-/backups/tether}
KEEP_DAYS=${KEEP_DAYS:-14}
FILE="$DIR/tether-$(date +%F-%H%M).dump"

mkdir -p "$DIR"
docker exec tether-postgres pg_dump -U tether -Fc tether > "$FILE.tmp"
mv "$FILE.tmp" "$FILE" # a failed dump never replaces a good file name
find "$DIR" -name 'tether-*.dump' -mtime +"$KEEP_DAYS" -delete

if [ -n "${RCLONE_REMOTE:-}" ]; then
  rclone copy "$FILE" "$RCLONE_REMOTE"
fi
echo "backup ok: $FILE ($(du -h "$FILE" | cut -f1))"
