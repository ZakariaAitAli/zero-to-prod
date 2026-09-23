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

docs_expected=$'app=false\nterraform=false\nworkflow=false\ndeploy=false'
app_no_deploy_expected=$'app=true\nterraform=false\nworkflow=false\ndeploy=false'
app_expected=$'app=true\nterraform=false\nworkflow=false\ndeploy=true'
app_local_only_expected=$'app=true\nterraform=false\nworkflow=false\ndeploy=false'
deploy_sensitive_expected=$'app=true\nterraform=true\nworkflow=true\ndeploy=true'
deploy_sensitive_local_only_expected=$'app=true\nterraform=true\nworkflow=true\ndeploy=false'
deployment_mode_local_only_expected=$'app=true\nterraform=false\nworkflow=true\ndeploy=false'
deployment_mode_cloud_ready_expected=$'app=true\nterraform=false\nworkflow=true\ndeploy=true'
terraform_expected=$'app=false\nterraform=true\nworkflow=false\ndeploy=false'
workflow_expected=$'app=true\nterraform=true\nworkflow=true\ndeploy=false'
workflow_only_expected=$'app=false\nterraform=false\nworkflow=true\ndeploy=false'

assert_case \
  "docs-only" \
  "$docs_expected" \
  docs/sprint-02/change-aware-ci.md

assert_case \
  "database migration" \
  "$app_no_deploy_expected" \
  apps/demo-api/migrations/000001_create_work_items.up.sql

assert_case \
  "Go source blocked while local-only" \
  "$app_local_only_expected" \
  apps/demo-api/cmd/api/main.go

assert_case \
  "Dockerfile blocked while local-only" \
  "$app_local_only_expected" \
  apps/demo-api/Dockerfile

assert_case \
  "Terraform" \
  "$terraform_expected" \
  infra/terraform/development-verification/main.tf

assert_case \
  "workflow YAML" \
  "$workflow_expected" \
  .github/workflows/demo-api-ci.yml

assert_case \
  "CI required gate policy" \
  "$workflow_expected" \
  scripts/verify-ci-required.sh

assert_case \
  "rollback eligibility verifier" \
  "$workflow_only_expected" \
  scripts/verify-rollback-eligibility.sh

assert_case \
  "rollback eligibility tests" \
  "$workflow_only_expected" \
  scripts/test-rollback-eligibility.sh

assert_case \
  "runtime config digest helper" \
  "$workflow_only_expected" \
  scripts/runtime-config-digest.sh

assert_case \
  "runtime config digest tests" \
  "$workflow_only_expected" \
  scripts/test-runtime-config-digest.sh

assert_case \
  "deployment verifier blocked while local-only" \
  "$deploy_sensitive_local_only_expected" \
  scripts/verify-deployment.sh

assert_case \
  "deployment diagnostics collector blocked while local-only" \
  "$deploy_sensitive_local_only_expected" \
  scripts/collect-deployment-diagnostics.sh

assert_case \
  "verified deployment recorder blocked while local-only" \
  "$deploy_sensitive_local_only_expected" \
  scripts/record-verified-deployment.sh

assert_case \
  "ECS task definition blocked while local-only" \
  "$deploy_sensitive_local_only_expected" \
  infra/aws/ecs/demo-api-task-definition.json

assert_case \
  "deployment mode blocks deployment while local-only" \
  "$deployment_mode_local_only_expected" \
  apps/demo-api/deployment-mode

assert_case \
  "local developer tooling" \
  "$workflow_only_expected" \
  tools/demo-api-local

assert_case \
  "PostgreSQL backup local tooling" \
  "$workflow_only_expected" \
  tools/postgres-backup-local

assert_case \
  "unknown path remains conservative" \
  "$workflow_expected" \
  some/future/unclassified-file.txt

assert_case \
  "mixed docs + app blocked while local-only" \
  "$app_local_only_expected" \
  docs/sprint-02/change-aware-ci.md \
  apps/demo-api/cmd/api/main.go

assert_classifier_with_mode() {
  local name="$1"
  local mode="$2"
  local expected="$3"
  shift 3

  local tmpdir
  local actual

  tmpdir="$(mktemp -d)"

  mkdir -p \
    "${tmpdir}/scripts" \
    "${tmpdir}/apps/demo-api"

  cp "$classifier" "${tmpdir}/scripts/classify-ci-changes.sh"
  printf '%s\n' "$mode" > "${tmpdir}/apps/demo-api/deployment-mode"

  actual="$(
    "${tmpdir}/scripts/classify-ci-changes.sh" "$@"
  )"

  rm -rf "$tmpdir"

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

assert_classifier_failure_with_mode() {
  local name="$1"
  local mode="$2"
  shift 2

  local tmpdir
  local output
  local rc

  tmpdir="$(mktemp -d)"

  mkdir -p \
    "${tmpdir}/scripts" \
    "${tmpdir}/apps/demo-api"

  cp "$classifier" "${tmpdir}/scripts/classify-ci-changes.sh"
  printf '%s\n' "$mode" > "${tmpdir}/apps/demo-api/deployment-mode"

  set +e
  output="$(
    "${tmpdir}/scripts/classify-ci-changes.sh" "$@" 2>&1
  )"
  rc=$?
  set -e

  rm -rf "$tmpdir"

  if [[ "$rc" -eq 0 ]]; then
    echo "FAIL: ${name}"
    echo "classifier unexpectedly succeeded"
    printf '%s\n' "$output"
    exit 1
  fi

  echo "PASS: ${name}"
}

assert_classifier_with_mode \
  "cloud-ready Go source remains deploy eligible" \
  "cloud-ready" \
  "$app_expected" \
  apps/demo-api/cmd/api/main.go

assert_classifier_with_mode \
  "cloud-ready Dockerfile remains deploy eligible" \
  "cloud-ready" \
  "$app_expected" \
  apps/demo-api/Dockerfile

assert_classifier_with_mode \
  "cloud-ready ECS task definition remains deploy eligible" \
  "cloud-ready" \
  "$deploy_sensitive_expected" \
  infra/aws/ecs/demo-api-task-definition.json

assert_classifier_with_mode \
  "cloud-ready deployment mode change is deploy eligible" \
  "cloud-ready" \
  "$deployment_mode_cloud_ready_expected" \
  apps/demo-api/deployment-mode

assert_classifier_failure_with_mode \
  "invalid deployment mode fails closed" \
  "unsupported-mode" \
  apps/demo-api/cmd/api/main.go

echo
echo "All CI path-classification experiments passed."
