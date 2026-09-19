#!/usr/bin/env bash
set -euo pipefail

export AWS_PAGER=""

required=(
  DEPLOYMENT_RECORD_BUCKET
  DEPLOYMENT_ENVIRONMENT
  GIT_SHA
  IMAGE_URI
  IMAGE_DIGEST
  TASK_DEFINITION_ARN
  WORKFLOW_RUN_ID
  VERIFICATION_RESULT_FILE
)

for name in "${required[@]}"; do
  if [ -z "${!name:-}" ]; then
    echo "::error::${name} is required"
    exit 1
  fi
done

if [[ ! "$GIT_SHA" =~ ^[0-9a-f]{40}$ ]]; then
  echo "::error::GIT_SHA must be a full lowercase 40-character SHA"
  exit 1
fi

if [[ ! "$IMAGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo "::error::IMAGE_DIGEST must be a sha256 digest"
  exit 1
fi

if [[ ! "$DEPLOYMENT_ENVIRONMENT" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
  echo "::error::Invalid deployment environment"
  exit 1
fi

health_status="$(jq -r '.health.status // empty' "$VERIFICATION_RESULT_FILE")"
version_expected="$(jq -r '.version.expected // empty' "$VERIFICATION_RESULT_FILE")"
version_observed="$(jq -r '.version.observed // empty' "$VERIFICATION_RESULT_FILE")"
version_matches="$(jq -r '.version.matches // false' "$VERIFICATION_RESULT_FILE")"
verified_at="$(jq -r '.verification_timestamp // empty' "$VERIFICATION_RESULT_FILE")"

if [ "$health_status" != "healthy" ] ||
   [ "$version_matches" != "true" ] ||
   [ "$version_expected" != "$GIT_SHA" ] ||
   [ "$version_observed" != "$GIT_SHA" ] ||
   [ -z "$verified_at" ]; then
  echo "::error::Verification result does not prove this exact Git SHA"
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

record_file="${tmp_dir}/deployment-record.json"
retrieved_file="${tmp_dir}/retrieved-record.json"
key="${DEPLOYMENT_ENVIRONMENT}/${GIT_SHA}.json"

jq -n \
  --arg environment "$DEPLOYMENT_ENVIRONMENT" \
  --arg git_sha "$GIT_SHA" \
  --arg image_uri "$IMAGE_URI" \
  --arg image_digest "$IMAGE_DIGEST" \
  --arg task_definition_arn "$TASK_DEFINITION_ARN" \
  --arg workflow_run_id "$WORKFLOW_RUN_ID" \
  --arg verified_at "$verified_at" \
  --arg health_status "$health_status" \
  --arg version_expected "$version_expected" \
  --arg version_observed "$version_observed" \
  '{
    schema_version: 1,
    verification_status: "verified",
    environment: $environment,
    git_sha: $git_sha,
    image_uri: $image_uri,
    image_digest: $image_digest,
    task_definition_arn: $task_definition_arn,
    workflow_run_id: $workflow_run_id,
    verification_timestamp: $verified_at,
    health: {
      status: $health_status
    },
    version: {
      expected: $version_expected,
      observed: $version_observed,
      matches: true
    }
  }' > "$record_file"

aws s3api put-object \
  --bucket "$DEPLOYMENT_RECORD_BUCKET" \
  --key "$key" \
  --body "$record_file" \
  --content-type application/json \
  --if-none-match "*" \
  --no-cli-pager >/dev/null

aws s3api get-object \
  --bucket "$DEPLOYMENT_RECORD_BUCKET" \
  --key "$key" \
  "$retrieved_file" \
  --no-cli-pager >/dev/null

if ! cmp -s "$record_file" "$retrieved_file"; then
  echo "::error::Retrieved deployment record differs from written record"
  exit 1
fi

echo "Verified deployment record stored and retrieved: s3://${DEPLOYMENT_RECORD_BUCKET}/${key}"
