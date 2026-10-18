#!/bin/sh
set -eu
mkdir -p "$PGDATA" /wal-archive
chown postgres:postgres "$PGDATA" /wal-archive
chmod 700 "$PGDATA"
umask 077
printf 'postgres:5432:replication:ztp_replication:%s\n' "$(cat /run/secrets/replication-password)" > /var/lib/postgresql/.replication.pgpass
chown postgres:postgres /var/lib/postgresql/.replication.pgpass
if [ ! -s "$PGDATA/PG_VERSION" ]; then
 gosu postgres pg_basebackup -d 'host=postgres user=ztp_replication passfile=/var/lib/postgresql/.replication.pgpass application_name=ha-standby' -D "$PGDATA" -R -X stream -c fast
fi
exec /usr/local/bin/docker-entrypoint.sh "$@"
