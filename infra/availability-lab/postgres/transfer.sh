#!/bin/sh
set -eu
# Local transfer/access boundary demonstration; same host/disk is NOT off-site.
if [ -e /offsite/latest ]; then echo 'transfer target already populated' >&2; exit 1; fi
backup="$(cat /source-backups/latest)"
case "$backup" in 20[0-9]*-*) ;; *) exit 1;; esac
mkdir -p /offsite/wal
cp -a "/source-backups/$backup" "/offsite/$backup"
for wal in /source-wal/*; do
 [ -f "$wal" ] || continue
 case "$wal" in *.partial.*) continue;; esac
 cp -p "$wal" /offsite/wal/
done
printf '%s\n' "$backup" > /offsite/latest
(cd /offsite && find "$backup" wal -type f -exec sha256sum '{}' \; > SHA256SUMS)
(cd /offsite && sha256sum -c SHA256SUMS >/dev/null)
pg_verifybackup "/offsite/$backup"
sync
echo 'Transferred and independently verified backup/WAL copy; physical host remains shared.'
