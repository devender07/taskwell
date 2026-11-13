#!/bin/sh
# Nightly dump of the taskwell database, kept here for KEEP_DAYS days. Run from /etc/cron.d/taskwell-backup.
# With R2_BUCKET set in /etc/taskwell-backup.env (root only), each dump is also encrypted with BACKUP_PASSPHRASE
# (rclone crypt) and uploaded to that Cloudflare R2 bucket, kept there for R2_KEEP_DAYS days. Without it a lost server
# takes its backups with it.
#   backup.sh                 dump now (and upload)
#   backup.sh list            the dumps in R2
#   backup.sh get FILE [DIR]  download and decrypt one from R2 into DIR (default .)
set -eu
[ -f /etc/taskwell-backup.env ] && . /etc/taskwell-backup.env
umask 077 # the dumps hold every workspace's data: root only

# The "backup:" rclone remote, from env vars only (no rclone.conf): crypt over the R2 bucket.
r2() {
  [ -n "${R2_BUCKET:-}" ] || { echo "no R2_BUCKET in /etc/taskwell-backup.env"; exit 1; }
  : "${R2_ACCOUNT_ID:?set in /etc/taskwell-backup.env}" "${R2_ACCESS_KEY_ID:?}" "${R2_SECRET_ACCESS_KEY:?}" "${BACKUP_PASSPHRASE:?}"
  export RCLONE_CONFIG_R2_TYPE=s3 RCLONE_CONFIG_R2_PROVIDER=Cloudflare RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true \
    RCLONE_CONFIG_R2_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" RCLONE_CONFIG_R2_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
    RCLONE_CONFIG_R2_ENDPOINT="https://$R2_ACCOUNT_ID.r2.cloudflarestorage.com" \
    RCLONE_CONFIG_BACKUP_TYPE=crypt RCLONE_CONFIG_BACKUP_REMOTE="r2:$R2_BUCKET"
  RCLONE_CONFIG_BACKUP_PASSWORD=$(printf '%s' "$BACKUP_PASSPHRASE" | rclone obscure -)
  export RCLONE_CONFIG_BACKUP_PASSWORD
}

case ${1:-} in
  list) r2 && exec rclone lsl backup: ;;
  get) [ -n "${2:-}" ] || { echo "usage: $0 get FILE [DIR]"; exit 1; }; r2 && exec rclone copy "backup:$2" "${3:-.}" ;;
esac

DIR=${BACKUP_DIR:-/backups/taskwell}
KEEP_DAYS=${KEEP_DAYS:-14}
FILE="$DIR/taskwell-$(date +%F-%H%M).dump"

mkdir -p "$DIR"
docker exec taskwell-postgres pg_dump -U taskwell -Fc taskwell > "$FILE.tmp"
mv "$FILE.tmp" "$FILE" # a failed dump never replaces a good file name
find "$DIR" -name 'taskwell-*.dump' -mtime +"$KEEP_DAYS" -delete

if [ -n "${R2_BUCKET:-}" ]; then
  r2
  rclone copy "$FILE" backup:
  rclone delete --min-age "${R2_KEEP_DAYS:-30}d" backup:
  echo "uploaded to r2:$R2_BUCKET"
fi
echo "backup ok: $FILE ($(du -h "$FILE" | cut -f1))"
