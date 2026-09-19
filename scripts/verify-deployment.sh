#!/usr/bin/env bash
set -euo pipefail

: "${BASE_URL:?BASE_URL is required}"
: "${EXPECTED_VERSION:?EXPECTED_VERSION is required}"

printf 'Verification target: %s\n' "$BASE_URL"
printf 'Expected version:   %s\n' "$EXPECTED_VERSION"

health_url="${BASE_URL%/}/health"
version_url="${BASE_URL%/}/version"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

health_body="${tmp_dir}/health.body"
version_body="${tmp_dir}/version.body"

diagnostics_file="${VERIFICATION_DIAGNOSTICS_FILE:-}"
result_file="${VERIFICATION_RESULT_FILE:-}"

if [ -n "$result_file" ]; then
  rm -f "$result_file"
fi

record_diagnostic() {
  [ -n "$diagnostics_file" ] || return 0

  {
    printf 'stage=%s\n' "$1"
    printf 'detail=%s\n' "$2"
  } > "$diagnostics_file"
}

json_field() {
  local file="$1"
  local field="$2"

  jq -r \
    --arg field "$field" \
    '(.[$field] // "missing")
     | tostring
     | gsub("[\\r\\n\\t]"; " ")
     | .[0:128]' \
    "$file" 2>/dev/null \
    || printf 'unparseable'
}

echo "Checking health endpoint: ${health_url}"

if curl \
  --fail-with-body \
  --silent \
  --show-error \
  --connect-timeout 3 \
  --max-time 5 \
  --retry 4 \
  --retry-delay 2 \
  --retry-connrefused \
  --output "$health_body" \
  "$health_url"; then
  :
else
  curl_exit=$?
  record_diagnostic \
    "health-request" \
    "curl_exit=${curl_exit}"

  echo "::error::Health request failed with curl exit code ${curl_exit}"
  exit "$curl_exit"
fi

health_status="$(json_field "$health_body" status)"
printf 'Observed health status: %s\n' "$health_status"

if [ "$health_status" != "healthy" ]; then
  record_diagnostic \
    "health-content" \
    "observed_status=${health_status}"

  echo "::error::Health endpoint did not report status=healthy"
  exit 1
fi

echo "Health verification passed."

echo "Checking version endpoint: ${version_url}"

if curl \
  --fail-with-body \
  --silent \
  --show-error \
  --connect-timeout 3 \
  --max-time 5 \
  --retry 4 \
  --retry-delay 2 \
  --retry-connrefused \
  --output "$version_body" \
  "$version_url"; then
  :
else
  curl_exit=$?
  record_diagnostic \
    "version-request" \
    "curl_exit=${curl_exit}"

  echo "::error::Version request failed with curl exit code ${curl_exit}"
  exit "$curl_exit"
fi

observed_version="$(json_field "$version_body" version)"
printf 'Observed version:   %s\n' "$observed_version"

if [ "$observed_version" != "$EXPECTED_VERSION" ]; then
  record_diagnostic \
    "version-mismatch" \
    "expected=${EXPECTED_VERSION} observed=${observed_version}"

  echo "::error::Expected version ${EXPECTED_VERSION}, observed ${observed_version}"
  exit 1
fi

if [ -n "$result_file" ]; then
  verification_timestamp="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  result_tmp="${tmp_dir}/verification-result.json"

  jq -n \
    --arg timestamp "$verification_timestamp" \
    --arg health "$health_status" \
    --arg expected "$EXPECTED_VERSION" \
    --arg observed "$observed_version" \
    '{
      verification_timestamp: $timestamp,
      health: {
        status: $health
      },
      version: {
        expected: $expected,
        observed: $observed,
        matches: ($expected == $observed)
      }
    }' > "$result_tmp"

  mv "$result_tmp" "$result_file"
fi

echo "Version verification passed."
echo "Deployment verification passed."
