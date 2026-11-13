#!/usr/bin/env bash
# Monitoring identity for postgres-exporter: pg_monitor only (statistics
# views and settings), no access to application tables.
set -euo pipefail

password_file=/run/secrets/postgres-monitor-password
if [[ ! -s "$password_file" ]]; then
  echo "error: required secret file missing or empty: ${password_file}" >&2
  exit 1
fi

psql \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --set=monitor_password="$(tr -d '\r\n' < "$password_file")" \
  <<'EOSQL'
SELECT format(
  'CREATE ROLE zero_to_prod_monitor LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE',
  :'monitor_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zero_to_prod_monitor')
\gexec

GRANT pg_monitor TO zero_to_prod_monitor;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
EOSQL
