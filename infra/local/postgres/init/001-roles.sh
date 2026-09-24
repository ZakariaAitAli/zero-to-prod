#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${ZTP_POSTGRES_MIGRATOR_USER:?ZTP_POSTGRES_MIGRATOR_USER is required}"
: "${ZTP_POSTGRES_MIGRATOR_PASSWORD:?ZTP_POSTGRES_MIGRATOR_PASSWORD is required}"
: "${ZTP_POSTGRES_APP_USER:?ZTP_POSTGRES_APP_USER is required}"
: "${ZTP_POSTGRES_APP_PASSWORD:?ZTP_POSTGRES_APP_PASSWORD is required}"
: "${ZTP_POSTGRES_WORKER_USER:?ZTP_POSTGRES_WORKER_USER is required}"
: "${ZTP_POSTGRES_WORKER_PASSWORD:?ZTP_POSTGRES_WORKER_PASSWORD is required}"

psql \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --set=migrator_user="$ZTP_POSTGRES_MIGRATOR_USER" \
  --set=migrator_password="$ZTP_POSTGRES_MIGRATOR_PASSWORD" \
  --set=app_user="$ZTP_POSTGRES_APP_USER" \
  --set=app_password="$ZTP_POSTGRES_APP_PASSWORD" \
  --set=worker_user="$ZTP_POSTGRES_WORKER_USER" \
  --set=worker_password="$ZTP_POSTGRES_WORKER_PASSWORD" \
  --set=db_name="$POSTGRES_DB" \
  <<'EOSQL'
SELECT format(
  'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT',
  :'migrator_user',
  :'migrator_password'
)
WHERE NOT EXISTS (
  SELECT 1
  FROM pg_roles
  WHERE rolname = :'migrator_user'
)
\gexec

SELECT format(
  'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT',
  :'app_user',
  :'app_password'
)
WHERE NOT EXISTS (
  SELECT 1
  FROM pg_roles
  WHERE rolname = :'app_user'
)
\gexec

SELECT format(
  'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT',
  :'worker_user',
  :'worker_password'
)
WHERE NOT EXISTS (
  SELECT 1
  FROM pg_roles
  WHERE rolname = :'worker_user'
)
\gexec

ALTER ROLE :"migrator_user"
  LOGIN
  PASSWORD :'migrator_password'
  NOSUPERUSER
  NOCREATEDB
  NOCREATEROLE
  NOINHERIT;

ALTER ROLE :"app_user"
  LOGIN
  PASSWORD :'app_password'
  NOSUPERUSER
  NOCREATEDB
  NOCREATEROLE
  NOINHERIT;

ALTER ROLE :"worker_user"
  LOGIN
  PASSWORD :'worker_password'
  NOSUPERUSER
  NOCREATEDB
  NOCREATEROLE
  NOINHERIT;

GRANT CONNECT ON DATABASE :"db_name"
  TO :"migrator_user", :"app_user", :"worker_user";

GRANT USAGE, CREATE ON SCHEMA public
  TO :"migrator_user";

GRANT USAGE ON SCHEMA public
  TO :"app_user", :"worker_user";
EOSQL
