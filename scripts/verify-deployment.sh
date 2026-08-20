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
  health_response="$(cat "$health_body" 2>/dev/null || true)"
  printf 'Health response: %s\n' "$health_response"
  echo "::error::Health request failed with curl exit code ${curl_exit}"
  exit "$curl_exit"
fi

health_response="$(cat "$health_body")"
printf 'Health response: %s\n' "$health_response"

if ! jq -e '.status == "healthy"' >/dev/null <<<"$health_response"; then
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
  version_response="$(cat "$version_body" 2>/dev/null || true)"
  printf 'Version response: %s\n' "$version_response"
  echo "::error::Version request failed with curl exit code ${curl_exit}"
  exit "$curl_exit"
fi

version_response="$(cat "$version_body")"
printf 'Version response: %s\n' "$version_response"

observed_version="$(jq -r '.version // empty' <<<"$version_response")"

printf 'Observed version:   %s\n' "$observed_version"

if [ "$observed_version" != "$EXPECTED_VERSION" ]; then
  echo "::error::Expected version ${EXPECTED_VERSION}, observed ${observed_version}"
  exit 1
fi

echo "Version verification passed."
echo "Deployment verification passed."
