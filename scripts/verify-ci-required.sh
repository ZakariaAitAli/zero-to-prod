#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 7 ]; then
  echo "Usage: $0 <changes-result> <backend-required> <backend-result> <frontend-required> <frontend-result> <workflow-required> <workflow-result>" >&2
  exit 2
fi

changes_result="$1"
backend_required="$2"
backend_result="$3"
frontend_required="$4"
frontend_result="$5"
workflow_required="$6"
workflow_result="$7"

failed=false

if [ "$changes_result" != "success" ]; then
  echo "Change detection result: ${changes_result}" >&2
  failed=true
fi

check_validation() {
  local name="$1"
  local required="$2"
  local result="$3"

  echo "${name}: required=${required:-unknown}, result=${result}"

  case "$required" in
    true)
      if [ "$result" != "success" ]; then
        echo "${name} was required but finished with ${result}." >&2
        failed=true
      fi
      ;;
    false)
      if [ "$result" != "skipped" ]; then
        echo "${name} was not required but finished with ${result}." >&2
        failed=true
      fi
      ;;
    *)
      echo "${name} has invalid required flag: ${required:-missing}." >&2
      failed=true
      ;;
  esac
}

check_validation "Backend validation" "$backend_required" "$backend_result"
check_validation "Frontend validation" "$frontend_required" "$frontend_result"
check_validation "Workflow validation" "$workflow_required" "$workflow_result"

if [ "$failed" = "true" ]; then
  exit 1
fi

echo "All required validations passed."
