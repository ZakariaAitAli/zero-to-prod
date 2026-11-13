#!/bin/sh
set -eu
mkdir -p /backups
chown postgres:postgres /backups
umask 077
printf 'postgres:5432:replication:ztp_replication:%s\n' "$(cat /run/secrets/replication-password)" > /tmp/backup.pgpass
chmod 600 /tmp/backup.pgpass
chown postgres:postgres /tmp/backup.pgpass
# One schedule actor. A crash leaves a visible stale lock; never guess it is
# safe to remove a different process's lock or delete the last good backup.
backup_once() {
 mkdir /backups/.backup-lock || { echo 'backup busy or stale lock' >&2; return 1; }
 name="$(date -u +%Y%m%dT%H%M%S)-$$"
 mkdir "/backups/$name.partial"
 chown postgres:postgres "/backups/$name.partial"
 if gosu postgres pg_basebackup -d 'host=postgres user=ztp_replication passfile=/tmp/backup.pgpass' -D "/backups/$name.partial" -X stream -c fast --manifest-checksums=SHA256 && gosu postgres pg_verifybackup "/backups/$name.partial"; then
  mv "/backups/$name.partial" "/backups/$name"
  printf '%s\n' "$name" > /backups/latest.tmp
  mv /backups/latest.tmp /backups/latest
  date +%s > /backups/last-verified.tmp
  mv /backups/last-verified.tmp /backups/last-verified
  sync
  # Retain the newest three verified directories; never delete partials as
  # though they were successful backups. WAL retained conservatively here.
  ls -1d /backups/20* 2>/dev/null | grep -v '\.partial$' | sort -r | tail -n +4 | while read -r old; do
   case "$old" in /backups/20[0-9]*-*) rm -rf -- "$old" ;; *) exit 1;; esac
  done
  rmdir /backups/.backup-lock
  echo "verified=$name"
 else
  rmdir /backups/.backup-lock
  return 1
 fi
}
if [ "${1:-}" = once ]; then backup_once; exit $?; fi
while :; do backup_once || echo 'scheduled backup failed' >&2; sleep "${BACKUP_INTERVAL_SECONDS:-300}"; done
