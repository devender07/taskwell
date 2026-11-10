#!/bin/sh
# Nightly dump of the taskwell database, kept for KEEP_DAYS days. Run from /etc/cron.d/taskwell-backup (deploy/server-setup.sh).
# Set RCLONE_REMOTE (e.g. r2:taskwell-backups, ideally an rclone crypt remote) in /etc/taskwell-backup.env (root only) to
# also copy each dump off the server; without it a lost server takes its backups with it.
set -eu
[ -f /etc/taskwell-backup.env ] && . /etc/taskwell-backup.env
umask 077 # the dumps hold every workspace's data: root only

DIR=${BACKUP_DIR:-/backups/taskwell}
KEEP_DAYS=${KEEP_DAYS:-14}
FILE="$DIR/taskwell-$(date +%F-%H%M).dump"

mkdir -p "$DIR"
docker exec taskwell-postgres pg_dump -U taskwell -Fc taskwell > "$FILE.tmp"
mv "$FILE.tmp" "$FILE" # a failed dump never replaces a good file name
find "$DIR" -name 'taskwell-*.dump' -mtime +"$KEEP_DAYS" -delete

if [ -n "${RCLONE_REMOTE:-}" ]; then
  rclone copy "$FILE" "$RCLONE_REMOTE"
fi
echo "backup ok: $FILE ($(du -h "$FILE" | cut -f1))"
