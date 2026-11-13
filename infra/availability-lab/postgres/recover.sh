#!/bin/sh
set -eu
# Only a freshly allocated recovery volume is accepted; never overwrite data.
if [ -e "$PGDATA/PG_VERSION" ]; then echo 'recovery target already initialized' >&2; exit 1; fi
backup="$(cat /restore-source/latest)"
case "$backup" in 20[0-9]*-*) ;; *) echo 'invalid verified backup selector' >&2; exit 1;; esac
(cd /restore-source && sha256sum -c SHA256SUMS >/dev/null)
pg_verifybackup "/restore-source/$backup"
mkdir -p "$PGDATA"
if [ -n "$(ls -A "$PGDATA")" ]; then echo 'recovery target not empty' >&2; exit 1; fi
cp -a "/restore-source/$backup/." "$PGDATA/"
chown -R postgres:postgres "$PGDATA"
chmod 700 "$PGDATA"
cat >> "$PGDATA/postgresql.auto.conf" <<'CONF'
restore_command = 'cp /restore-source/wal/%f %p'
recovery_target_name = 'ha_before_delete'
recovery_target_timeline = '1'
recovery_target_action = 'promote'
CONF
# The selected base backup comes directly from the original primary (-R off).
touch "$PGDATA/recovery.signal"
chown postgres:postgres "$PGDATA/recovery.signal" "$PGDATA/postgresql.auto.conf"
exec /usr/local/bin/docker-entrypoint.sh postgres -c archive_mode=off
