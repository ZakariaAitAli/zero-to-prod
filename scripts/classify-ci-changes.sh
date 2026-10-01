#!/usr/bin/env bash
set -euo pipefail

backend=false
frontend=false
workflow=false

if [ "${1:-}" = "--conservative" ]; then
  backend=true
  frontend=true
  workflow=true
  shift
elif [ "$#" -eq 0 ]; then
  echo "No changed paths supplied; using conservative validation." >&2
  backend=true
  frontend=true
  workflow=true
fi

for changed_file in "$@"; do
  case "$changed_file" in
    docs/*|README.md|LICENSE)
      ;;

    apps/work-items/web/*)
      frontend=true
      ;;

    apps/work-items/api/*|apps/work-items/worker/*|apps/work-items/migrations/*|apps/work-items/go.mod|apps/work-items/go.sum)
      backend=true
      ;;

    infra/local/*|tools/work-items-api-local|tools/postgres-local|tools/postgres-backup-local|tools/work-items-worker-local|tools/rabbitmq-local)
      backend=true
      workflow=true
      ;;

    .github/workflows/*|scripts/classify-ci-changes.sh|scripts/test-ci-change-classifier.sh|scripts/verify-ci-required.sh|scripts/test-ci-required-gate.sh)
      backend=true
      frontend=true
      workflow=true
      ;;

    apps/work-items/*)
      echo "Conservative validation for unclassified Work Items path: ${changed_file}" >&2
      backend=true
      frontend=true
      workflow=true
      ;;

    *)
      echo "Conservative validation for unclassified path: ${changed_file}" >&2
      backend=true
      frontend=true
      workflow=true
      ;;
  esac
done

printf 'backend=%s\n' "$backend"
printf 'frontend=%s\n' "$frontend"
printf 'workflow=%s\n' "$workflow"
