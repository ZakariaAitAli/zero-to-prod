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
terraform_expected=$'app=false\nterraform=true\nworkflow=false\ndeploy=false'
workflow_expected=$'app=true\nterraform=true\nworkflow=true\ndeploy=false'

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
  "mixed docs + app" \
  "$app_expected" \
  docs/sprint-02/change-aware-ci.md \
  apps/demo-api/cmd/api/main.go

echo
echo "All Issue #43 path-classification experiments passed."
