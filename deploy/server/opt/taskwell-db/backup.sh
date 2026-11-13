#!/bin/sh
# Nightly dump of the taskwell database. Run from /etc/cron.d/taskwell-backup.
# With R2_BUCKET set in /etc/taskwell-backup.env (root only), each dump is encrypted with BACKUP_PASSPHRASE (rclone
# crypt) and moved to that Cloudflare R2 bucket, kept there for R2_KEEP_DAYS days; nothing stays on the server once it
# uploaded (a dump whose upload failed stays and goes up with the next run). Without R2_BUCKET dumps stay here for
# KEEP_DAYS days, and a lost server takes them with it.
#   backup.sh                 dump now (and upload)
#   backup.sh list            the dumps in R2
#   backup.sh get FILE [DIR]  download and decrypt one from R2 into DIR (default .)
set -eu
[ -f /etc/taskwell-backup.env ] && . /etc/taskwell-backup.env
umask 077 # the dumps hold every workspace's data: root only

# The "backup:" rclone remote, from env vars only (no rclone.conf): crypt over the R2 bucket. no_head: rclone 1.60
# checks an upload with a versioned HEAD that R2 answers 501; the PUT is still checked by its Content-MD5.
r2() {
  [ -n "${R2_BUCKET:-}" ] || { echo "no R2_BUCKET in /etc/taskwell-backup.env"; exit 1; }
  : "${R2_ACCOUNT_ID:?set in /etc/taskwell-backup.env}" "${R2_ACCESS_KEY_ID:?}" "${R2_SECRET_ACCESS_KEY:?}" "${BACKUP_PASSPHRASE:?}"
  export RCLONE_CONFIG_R2_TYPE=s3 RCLONE_CONFIG_R2_PROVIDER=Cloudflare RCLONE_CONFIG_R2_NO_CHECK_BUCKET=true RCLONE_CONFIG_R2_NO_HEAD=true \
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
  SIZE=$(du -h "$FILE" | cut -f1)
  r2
  rclone move "$DIR" backup: --include 'taskwell-*.dump' # deletes each local dump once it's uploaded
  rclone delete --min-age "${R2_KEEP_DAYS:-30}d" backup:
  echo "backup ok: $(basename "$FILE") ($SIZE) in r2:$R2_BUCKET"
else
  echo "backup ok: $FILE ($(du -h "$FILE" | cut -f1))"
fi
