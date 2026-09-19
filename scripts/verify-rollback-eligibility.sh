#!/usr/bin/env bash
set -euo pipefail

export AWS_PAGER=""

required=(
  DEPLOYMENT_RECORD_BUCKET
  TARGET_ENVIRONMENT
  TARGET_SHA
  EXPECTED_IMAGE_URI
  EXPECTED_IMAGE_DIGEST
)

for name in "${required[@]}"; do
  if [ -z "${!name:-}" ]; then
    echo "::error::${name} is required"
    exit 1
  fi
done

if [[ ! "$TARGET_SHA" =~ ^[0-9a-f]{40}$ ]]; then
  echo "::error::TARGET_SHA must be a full lowercase 40-character SHA"
  exit 1
fi

if [[ ! "$TARGET_ENVIRONMENT" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
  echo "::error::Invalid target environment"
  exit 1
fi

if [[ ! "$EXPECTED_IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo "::error::EXPECTED_IMAGE_DIGEST must be a sha256 digest"
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

record_file="${tmp_dir}/deployment-record.json"
get_error_file="${tmp_dir}/get-object.stderr"
record_key="${TARGET_ENVIRONMENT}/${TARGET_SHA}.json"

echo "Checking verified deployment record: s3://${DEPLOYMENT_RECORD_BUCKET}/${record_key}"

if ! aws s3api get-object \
  --bucket "$DEPLOYMENT_RECORD_BUCKET" \
  --key "$record_key" \
  "$record_file" \
  --no-cli-pager \
  >/dev/null 2>"$get_error_file"; then

  echo "Rollback eligibility: REJECTED"

  if grep -Eq 'NoSuchKey|404|Not Found' "$get_error_file"; then
    echo "::error::No verified deployment record exists for ${TARGET_SHA} in ${TARGET_ENVIRONMENT}"
  elif grep -Eq 'AccessDenied|403|Forbidden' "$get_error_file"; then
    echo "::error::Deployment record unavailable or inaccessible for ${TARGET_SHA} in ${TARGET_ENVIRONMENT}"
  else
    echo "::error::Could not retrieve deployment record for ${TARGET_SHA}"
    cat "$get_error_file" >&2
  fi

  exit 1
fi

if ! jq -e 'type == "object"' "$record_file" >/dev/null 2>&1; then
  echo "::error::Deployment record is malformed JSON or is not a JSON object"
  exit 1
fi

assert_equal() {
  local field="$1"
  local expected="$2"
  local actual

  actual="$(jq -r "${field} // empty" "$record_file")"

  if [ "$actual" != "$expected" ]; then
    echo "::error::Deployment record ${field} mismatch: expected '${expected}', got '${actual}'"
    exit 1
  fi
}

assert_nonempty() {
  local field="$1"
  local actual

  actual="$(jq -r "${field} // empty" "$record_file")"

  if [ -z "$actual" ]; then
    echo "::error::Deployment record ${field} is required"
    exit 1
  fi
}

assert_equal '.schema_version' '1'
assert_equal '.verification_status' 'verified'
assert_equal '.environment' "$TARGET_ENVIRONMENT"
assert_equal '.git_sha' "$TARGET_SHA"
assert_equal '.image_uri' "$EXPECTED_IMAGE_URI"
assert_equal '.image_digest' "$EXPECTED_IMAGE_DIGEST"
assert_equal '.health.status' 'healthy'
assert_equal '.version.expected' "$TARGET_SHA"
assert_equal '.version.observed' "$TARGET_SHA"
assert_equal '.version.matches' 'true'

assert_nonempty '.task_definition_arn'
assert_nonempty '.workflow_run_id'
assert_nonempty '.verification_timestamp'

verified_at="$(jq -r '.verification_timestamp' "$record_file")"

echo "Rollback eligibility: ELIGIBLE"
echo "Environment: ${TARGET_ENVIRONMENT}"
echo "Target SHA: ${TARGET_SHA}"
echo "Image digest: ${EXPECTED_IMAGE_DIGEST}"
echo "Deployment record: ${record_key}"
echo "Previously verified at: ${verified_at}"
