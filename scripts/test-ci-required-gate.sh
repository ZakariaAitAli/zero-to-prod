#!/usr/bin/env bash
set -euo pipefail

gate="./scripts/verify-ci-required.sh"

expect_pass() {
  local name="$1"
  shift

  if "$gate" "$@" >/dev/null 2>&1; then
    echo "PASS: ${name}"
  else
    echo "FAIL: ${name}"
    exit 1
  fi
}

expect_fail() {
  local name="$1"
  shift

  if "$gate" "$@" >/dev/null 2>&1; then
    echo "FAIL: ${name}"
    exit 1
  else
    echo "PASS: ${name}"
  fi
}

expect_pass \
  "docs-only skips all optional validation" \
  success false skipped false skipped false skipped

expect_pass \
  "application validation succeeds" \
  success true success false skipped false skipped

expect_pass \
  "all validations succeed" \
  success true success true success true success

expect_fail \
  "required validation failure blocks gate" \
  success true failure false skipped false skipped

expect_fail \
  "required validation cannot be skipped" \
  success true skipped false skipped false skipped

expect_fail \
  "optional validation cannot unexpectedly run" \
  success false success false skipped false skipped

expect_fail \
  "missing classifier output fails closed" \
  success "" skipped false skipped false skipped

expect_fail \
  "change detection failure blocks gate" \
  failure false skipped false skipped false skipped

echo
echo "All CI required gate regression tests passed."
