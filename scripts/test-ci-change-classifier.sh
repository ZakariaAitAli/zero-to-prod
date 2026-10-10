#!/usr/bin/env bash
set -euo pipefail

classifier="./scripts/classify-ci-changes.sh"

assert_case() {
  local name="$1"
  local expected="$2"
  shift 2

  local actual
  actual="$("$classifier" "$@")"

  if [[ "$actual" != "$expected" ]]; then
    echo "FAIL: ${name}"
    echo "--- expected ---"
    printf '%s\n' "$expected"
    echo "--- actual ---"
    printf '%s\n' "$actual"
    exit 1
  fi

  echo "PASS: ${name}"
}

docs_expected=$'backend=false\nfrontend=false\nworkflow=false'
backend_expected=$'backend=true\nfrontend=false\nworkflow=false'
frontend_expected=$'backend=false\nfrontend=true\nworkflow=false'
backend_workflow_expected=$'backend=true\nfrontend=false\nworkflow=true'
full_expected=$'backend=true\nfrontend=true\nworkflow=true'

assert_case \
  "docs-only" \
  "$docs_expected" \
  docs/sprint-03/reliable-async-processing.md

assert_case \
  "API Go source" \
  "$backend_expected" \
  apps/work-items/api/main.go

assert_case \
  "worker Go source" \
  "$backend_expected" \
  apps/work-items/worker/main.go

assert_case \
  "database migration" \
  "$backend_expected" \
  apps/work-items/migrations/000001_create_work_items.up.sql

assert_case \
  "API Dockerfile" \
  "$backend_expected" \
  apps/work-items/api/Dockerfile

assert_case \
  "frontend source" \
  "$frontend_expected" \
  apps/work-items/web/src/App.tsx

assert_case \
  "frontend package manifest" \
  "$frontend_expected" \
  apps/work-items/web/package.json

assert_case \
  "frontend lockfile" \
  "$frontend_expected" \
  apps/work-items/web/pnpm-lock.yaml

assert_case \
  "local Compose runtime" \
  "$backend_workflow_expected" \
  infra/local/compose.yaml

assert_case \
  "local RabbitMQ configuration" \
  "$backend_workflow_expected" \
  infra/local/rabbitmq/rabbitmq.conf

assert_case \
  "local PostgreSQL bootstrap" \
  "$backend_workflow_expected" \
  infra/local/postgres/init/001-roles.sh

assert_case \
  "API local lifecycle tooling" \
  "$backend_workflow_expected" \
  tools/work-items-api-local

assert_case \
  "worker local lifecycle tooling" \
  "$backend_workflow_expected" \
  tools/work-items-worker-local

assert_case \
  "PostgreSQL lifecycle tooling" \
  "$backend_workflow_expected" \
  tools/postgres-local

assert_case \
  "PostgreSQL backup tooling" \
  "$backend_workflow_expected" \
  tools/postgres-backup-local

assert_case \
  "RabbitMQ lifecycle tooling" \
  "$backend_workflow_expected" \
  tools/rabbitmq-local

assert_case \
  "security lab Compose runtime" \
  "$backend_workflow_expected" \
  infra/security-lab/compose.yaml

assert_case \
  "security lab ingress configuration" \
  "$backend_workflow_expected" \
  infra/security-lab/ingress/nginx.conf

assert_case \
  "security lab tooling" \
  "$backend_workflow_expected" \
  tools/security-lab-local

assert_case \
  "worker Dockerfile" \
  "$backend_expected" \
  apps/work-items/worker/Dockerfile

assert_case \
  "workflow YAML" \
  "$full_expected" \
  .github/workflows/work-items-ci.yml

assert_case \
  "classifier policy" \
  "$full_expected" \
  scripts/classify-ci-changes.sh

assert_case \
  "classifier regression tests" \
  "$full_expected" \
  scripts/test-ci-change-classifier.sh

assert_case \
  "required gate policy" \
  "$full_expected" \
  scripts/verify-ci-required.sh

assert_case \
  "required gate regression tests" \
  "$full_expected" \
  scripts/test-ci-required-gate.sh

assert_case \
  "unknown Work Items path remains conservative" \
  "$full_expected" \
  apps/work-items/some-future-shared-file.txt

assert_case \
  "workflow failure regression tests" \
  "$full_expected" \
  scripts/test-ci-workflow.sh

assert_case \
  "backup validation regression tests" \
  "$full_expected" \
  scripts/test-postgres-backup-validation.sh

assert_case \
  "security lab state-safety regression tests" \
  "$full_expected" \
  scripts/test-security-lab-state-safety.sh

assert_case \
  "unknown repository path remains conservative" \
  "$full_expected" \
  some/future/unclassified-file.txt

assert_case \
  "mixed docs and backend" \
  "$backend_expected" \
  docs/sprint-03/reliable-async-processing.md \
  apps/work-items/api/main.go

assert_case \
  "mixed backend and frontend" \
  $'backend=true\nfrontend=true\nworkflow=false' \
  apps/work-items/api/main.go \
  apps/work-items/web/src/App.tsx

assert_case \
  "mixed frontend and local infrastructure" \
  "$full_expected" \
  apps/work-items/web/src/App.tsx \
  infra/local/rabbitmq/rabbitmq.conf

assert_case \
  "conservative execution" \
  "$full_expected" \
  --conservative

echo
echo "All CI path-classification experiments passed."
