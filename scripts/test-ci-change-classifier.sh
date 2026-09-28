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

docs_expected=$'app=false\nworkflow=false'
app_expected=$'app=true\nworkflow=false'
full_expected=$'app=true\nworkflow=true'

assert_case \
  "docs-only" \
  "$docs_expected" \
  docs/sprint-03/reliable-async-processing.md

assert_case \
  "API Go source" \
  "$app_expected" \
  apps/work-items/cmd/api/main.go

assert_case \
  "worker Go source" \
  "$app_expected" \
  apps/work-items/cmd/worker/main.go

assert_case \
  "database migration" \
  "$app_expected" \
  apps/work-items/migrations/000001_create_work_items.up.sql

assert_case \
  "Dockerfile" \
  "$app_expected" \
  apps/work-items/Dockerfile

assert_case \
  "local Compose runtime" \
  "$full_expected" \
  infra/local/compose.yaml

assert_case \
  "local RabbitMQ configuration" \
  "$full_expected" \
  infra/local/rabbitmq/rabbitmq.conf

assert_case \
  "local PostgreSQL bootstrap" \
  "$full_expected" \
  infra/local/postgres/init/001-roles.sh

assert_case \
  "API local lifecycle tooling" \
  "$full_expected" \
  tools/work-items-api-local

assert_case \
  "worker local lifecycle tooling" \
  "$full_expected" \
  tools/work-items-worker-local

assert_case \
  "PostgreSQL lifecycle tooling" \
  "$full_expected" \
  tools/postgres-local

assert_case \
  "PostgreSQL backup tooling" \
  "$full_expected" \
  tools/postgres-backup-local

assert_case \
  "RabbitMQ lifecycle tooling" \
  "$full_expected" \
  tools/rabbitmq-local

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
  "unknown path remains conservative" \
  "$full_expected" \
  some/future/unclassified-file.txt

assert_case \
  "mixed docs and application" \
  "$app_expected" \
  docs/sprint-03/reliable-async-processing.md \
  apps/work-items/cmd/api/main.go

assert_case \
  "mixed application and local infrastructure" \
  "$full_expected" \
  apps/work-items/cmd/api/main.go \
  infra/local/rabbitmq/rabbitmq.conf

assert_case \
  "conservative execution" \
  "$full_expected" \
  --conservative

echo
echo "All CI path-classification experiments passed."
