#!/usr/bin/env bash
# Provision the repository-defined PostgreSQL identities from runtime secret
# files, then apply the shared role definition used by the development lab.
set -euo pipefail

read_secret() {
  local path="/run/secrets/$1"

  if [[ ! -f "$path" || ! -s "$path" ]]; then
    echo "error: required secret file missing or empty: ${path}" >&2
    return 1
  fi

  tr -d '\r\n' < "$path"
}

migrator_password="$(read_secret postgres-migrator-password)"
app_password="$(read_secret postgres-app-password)"
worker_password="$(read_secret postgres-worker-password)"

export ZTP_POSTGRES_MIGRATOR_PASSWORD="$migrator_password"
export ZTP_POSTGRES_APP_PASSWORD="$app_password"
export ZTP_POSTGRES_WORKER_PASSWORD="$worker_password"

exec /opt/zero-to-prod/001-roles.sh
