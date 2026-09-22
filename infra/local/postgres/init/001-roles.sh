#!/usr/bin/env bash
set -euo pipefail

: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${ZTP_POSTGRES_MIGRATOR_USER:?ZTP_POSTGRES_MIGRATOR_USER is required}"
: "${ZTP_POSTGRES_MIGRATOR_PASSWORD:?ZTP_POSTGRES_MIGRATOR_PASSWORD is required}"
: "${ZTP_POSTGRES_APP_USER:?ZTP_POSTGRES_APP_USER is required}"
: "${ZTP_POSTGRES_APP_PASSWORD:?ZTP_POSTGRES_APP_PASSWORD is required}"

psql \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --set=migrator_user="$ZTP_POSTGRES_MIGRATOR_USER" \
  --set=migrator_password="$ZTP_POSTGRES_MIGRATOR_PASSWORD" \
  --set=app_user="$ZTP_POSTGRES_APP_USER" \
  --set=app_password="$ZTP_POSTGRES_APP_PASSWORD" \
  --set=db_name="$POSTGRES_DB" \
  <<'EOSQL'
CREATE ROLE :"migrator_user"
  LOGIN
  PASSWORD :'migrator_password'
  NOSUPERUSER
  NOCREATEDB
  NOCREATEROLE
  NOINHERIT;

CREATE ROLE :"app_user"
  LOGIN
  PASSWORD :'app_password'
  NOSUPERUSER
  NOCREATEDB
  NOCREATEROLE
  NOINHERIT;

GRANT CONNECT ON DATABASE :"db_name"
  TO :"migrator_user", :"app_user";

GRANT USAGE, CREATE ON SCHEMA public
  TO :"migrator_user";

GRANT USAGE ON SCHEMA public
  TO :"app_user";
EOSQL
