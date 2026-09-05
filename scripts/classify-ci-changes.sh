#!/usr/bin/env bash
set -euo pipefail

app=false
terraform=false
workflow=false
deploy=false

if [ "${1:-}" = "--conservative" ]; then
  app=true
  terraform=true
  workflow=true
  shift
elif [ "$#" -eq 0 ]; then
  echo "No changed paths supplied; using conservative validation." >&2
  app=true
  terraform=true
  workflow=true
fi

for changed_file in "$@"; do
  case "$changed_file" in
    docs/*|README.md|LICENSE)
      ;;

    apps/demo-api/*)
      app=true
      deploy=true
      ;;

    infra/terraform/*)
      terraform=true
      ;;

    .github/workflows/*|scripts/classify-ci-changes.sh|scripts/test-ci-change-classifier.sh)
      app=true
      terraform=true
      workflow=true
      ;;

    infra/aws/ecs/demo-api-task-definition.json|scripts/verify-deployment.sh)
      app=true
      terraform=true
      deploy=true
      ;;

    *)
      echo "Conservative validation for unclassified path: ${changed_file}" >&2
      app=true
      terraform=true
      workflow=true
      ;;
  esac
done

printf 'app=%s\n' "$app"
printf 'terraform=%s\n' "$terraform"
printf 'workflow=%s\n' "$workflow"
printf 'deploy=%s\n' "$deploy"
