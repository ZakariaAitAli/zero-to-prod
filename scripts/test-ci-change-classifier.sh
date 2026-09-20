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
app_expected=$'app=true\nterraform=false\nworkflow=false\ndeploy=true'
deploy_sensitive_expected=$'app=true\nterraform=true\nworkflow=true\ndeploy=true'
terraform_expected=$'app=false\nterraform=true\nworkflow=false\ndeploy=false'
workflow_expected=$'app=true\nterraform=true\nworkflow=true\ndeploy=false'
workflow_only_expected=$'app=false\nterraform=false\nworkflow=true\ndeploy=false'

assert_case \
  "docs-only" \
  "$docs_expected" \
  docs/sprint-02/change-aware-ci.md

assert_case \
  "Go source" \
  "$app_expected" \
  apps/demo-api/cmd/api/main.go

assert_case \
  "Dockerfile" \
  "$app_expected" \
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
  "deployment verifier" \
  "$deploy_sensitive_expected" \
  scripts/verify-deployment.sh

assert_case \
  "deployment diagnostics collector" \
  "$deploy_sensitive_expected" \
  scripts/collect-deployment-diagnostics.sh

assert_case \
  "verified deployment recorder" \
  "$deploy_sensitive_expected" \
  scripts/record-verified-deployment.sh

assert_case \
  "ECS task definition" \
  "$deploy_sensitive_expected" \
  infra/aws/ecs/demo-api-task-definition.json

assert_case \
  "local developer tooling" \
  "$workflow_only_expected" \
  tools/demo-api-local

assert_case \
  "unknown path remains conservative" \
  "$workflow_expected" \
  some/future/unclassified-file.txt

assert_case \
  "mixed docs + app" \
  "$app_expected" \
  docs/sprint-02/change-aware-ci.md \
  apps/demo-api/cmd/api/main.go

echo
echo "All CI path-classification experiments passed."
