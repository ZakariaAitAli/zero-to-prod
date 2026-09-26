#!/usr/bin/env bash
set -euo pipefail

app=false
workflow=false

if [ "${1:-}" = "--conservative" ]; then
  app=true
  workflow=true
  shift
elif [ "$#" -eq 0 ]; then
  echo "No changed paths supplied; using conservative validation." >&2
  app=true
  workflow=true
fi

for changed_file in "$@"; do
  case "$changed_file" in
    docs/*|README.md|LICENSE)
      ;;

    apps/demo-api/*)
      app=true
      ;;

    infra/local/*|tools/demo-api-local|tools/postgres-local|tools/postgres-backup-local|tools/worker-local|tools/rabbitmq-local)
      app=true
      workflow=true
      ;;

    .github/workflows/*|scripts/classify-ci-changes.sh|scripts/test-ci-change-classifier.sh|scripts/verify-ci-required.sh|scripts/test-ci-required-gate.sh)
      app=true
      workflow=true
      ;;

    *)
      echo "Conservative validation for unclassified path: ${changed_file}" >&2
      app=true
      workflow=true
      ;;
  esac
done

printf 'app=%s\n' "$app"
printf 'workflow=%s\n' "$workflow"
