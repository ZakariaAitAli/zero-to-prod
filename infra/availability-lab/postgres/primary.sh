#!/bin/sh
set -eu
mkdir -p /wal-archive
chown postgres:postgres /wal-archive
exec /usr/local/bin/docker-entrypoint.sh "$@"
