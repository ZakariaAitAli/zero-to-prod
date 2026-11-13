#!/bin/sh
set -eu
password="$(cat /run/secrets/replication-password)"
case "$password" in ''|*[!0-9a-f]*) echo 'invalid generated replication credential' >&2; exit 1;; esac
# Password only on stdin; session suppresses SQL/error statement logging.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<SQL
SET log_statement = 'none';
SET log_min_error_statement = 'panic';
CREATE ROLE ztp_replication LOGIN REPLICATION PASSWORD '$password';
SQL
printf '\nhost replication ztp_replication 10.213.22.0/24 scram-sha-256\n' >> "$PGDATA/pg_hba.conf"
