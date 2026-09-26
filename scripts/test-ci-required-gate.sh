#!/usr/bin/env bash
set -euo pipefail

gate="./scripts/verify-ci-required.sh"

expect_success() {
  local name="$1"
  shift

  if "$gate" "$@" >/dev/null 2>&1; then
    echo "PASS: ${name}"
  else
    echo "FAIL: ${name} unexpectedly failed"
    exit 1
  fi
}

expect_failure() {
  local name="$1"
  shift

  if "$gate" "$@" >/dev/null 2>&1; then
    echo "FAIL: ${name} unexpectedly succeeded"
    exit 1
  else
    echo "PASS: ${name}"
  fi
}

expect_exit_code() {
  local name="$1"
  local expected_rc="$2"
  shift 2

  set +e
  "$gate" "$@" >/dev/null 2>&1
  actual_rc=$?
  set -e

  if [ "$actual_rc" -ne "$expected_rc" ]; then
    echo "FAIL: ${name}: expected exit ${expected_rc}, got ${actual_rc}"
    exit 1
  fi

  echo "PASS: ${name}"
}

expect_success \
  "docs-only skips both validations" \
  success \
  false skipped \
  false skipped

expect_success \
  "application-only validation succeeds" \
  success \
  true success \
  false skipped

expect_success \
  "full validation succeeds" \
  success \
  true success \
  true success

expect_failure \
  "change detection failure fails closed" \
  failure \
  true success \
  true success

expect_failure \
  "required application validation cannot fail" \
  success \
  true failure \
  false skipped

expect_failure \
  "required application validation cannot be skipped" \
  success \
  true skipped \
  false skipped

expect_failure \
  "required workflow validation cannot fail" \
  success \
  true success \
  true failure

expect_failure \
  "required workflow validation cannot be skipped" \
  success \
  true success \
  true skipped

expect_failure \
  "unexpected application execution is rejected" \
  success \
  false success \
  false skipped

expect_failure \
  "unexpected workflow execution is rejected" \
  success \
  false skipped \
  false success

expect_failure \
  "invalid application required flag is rejected" \
  success \
  maybe success \
  false skipped

expect_failure \
  "invalid workflow required flag is rejected" \
  success \
  false skipped \
  maybe success

expect_exit_code \
  "wrong argument count is usage error" \
  2 \
  success \
  false skipped

echo
echo "All required-CI gate experiments passed."
