#!/usr/bin/env bash
set -euo pipefail

validator="./scripts/verify-rollback-eligibility.sh"

target_sha="236147faca2751ed69bfa54e463ed5b63281e081"
target_environment="development"
image_uri="333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:${target_sha}"
image_digest="sha256:2e704c3ef7aabe82eb4632aa5bae2f3da82a70a5f42a04402c6769187f102d4d"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

mock_bin="${tmp_dir}/bin"
mkdir -p "$mock_bin"

cat > "${mock_bin}/aws" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail

if [ "${MOCK_AWS_MODE:-}" = "missing" ]; then
  echo "An error occurred (NoSuchKey) when calling the GetObject operation: The specified key does not exist." >&2
  exit 254
fi

if [ "${MOCK_AWS_MODE:-}" = "access-denied" ]; then
  echo "An error occurred (AccessDenied) when calling the GetObject operation: not authorized to perform: s3:ListBucket" >&2
  exit 254
fi

if [ "${1:-}" != "s3api" ] || [ "${2:-}" != "get-object" ]; then
  echo "Unexpected fake AWS invocation: $*" >&2
  exit 99
fi

output_file="${@: -2:1}"
cp "$MOCK_RECORD_FILE" "$output_file"
MOCK

chmod +x "${mock_bin}/aws"

make_record() {
  local output="$1"
  local environment="$2"
  local digest="$3"
  local status="$4"

  jq -n \
    --arg environment "$environment" \
    --arg sha "$target_sha" \
    --arg image_uri "$image_uri" \
    --arg digest "$digest" \
    --arg status "$status" \
    '{
      schema_version: 1,
      verification_status: $status,
      environment: $environment,
      git_sha: $sha,
      image_uri: $image_uri,
      image_digest: $digest,
      task_definition_arn: "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:20",
      workflow_run_id: "35454383940",
      verification_timestamp: "2026-09-19T16:21:51Z",
      health: {
        status: "healthy"
      },
      version: {
        expected: $sha,
        observed: $sha,
        matches: true
      }
    }' > "$output"
}

run_case() {
  local name="$1"
  local expected_status="$2"
  local expected_text="$3"
  local mode="$4"
  local record_file="${5:-}"

  local output
  local status

  set +e
  output="$(
    PATH="${mock_bin}:$PATH" \
    MOCK_AWS_MODE="$mode" \
    MOCK_RECORD_FILE="$record_file" \
    DEPLOYMENT_RECORD_BUCKET="test-deployment-records" \
    TARGET_ENVIRONMENT="$target_environment" \
    TARGET_SHA="$target_sha" \
    EXPECTED_IMAGE_URI="$image_uri" \
    EXPECTED_IMAGE_DIGEST="$image_digest" \
    "$validator" 2>&1
  )"
  status=$?
  set -e

  if [ "$status" -ne "$expected_status" ]; then
    echo "FAIL: ${name}"
    echo "Expected exit ${expected_status}, got ${status}"
    printf '%s\n' "$output"
    exit 1
  fi

  if ! grep -Fq "$expected_text" <<<"$output"; then
    echo "FAIL: ${name}"
    echo "Expected output containing: ${expected_text}"
    printf '%s\n' "$output"
    exit 1
  fi

  echo "PASS: ${name}"
}

valid_record="${tmp_dir}/valid.json"
wrong_environment_record="${tmp_dir}/wrong-environment.json"
wrong_digest_record="${tmp_dir}/wrong-digest.json"
unverified_record="${tmp_dir}/unverified.json"
malformed_record="${tmp_dir}/malformed.json"
wrong_sha_record="${tmp_dir}/wrong-sha.json"
wrong_image_uri_record="${tmp_dir}/wrong-image-uri.json"
unhealthy_record="${tmp_dir}/unhealthy.json"
version_mismatch_record="${tmp_dir}/version-mismatch.json"
wrong_schema_record="${tmp_dir}/wrong-schema.json"
missing_provenance_record="${tmp_dir}/missing-provenance.json"
missing_task_definition_record="${tmp_dir}/missing-task-definition.json"
missing_workflow_run_record="${tmp_dir}/missing-workflow-run.json"

make_record "$valid_record" \
  "development" \
  "$image_digest" \
  "verified"

make_record "$wrong_environment_record" \
  "staging" \
  "$image_digest" \
  "verified"

make_record "$wrong_digest_record" \
  "development" \
  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "verified"

make_record "$unverified_record" \
  "development" \
  "$image_digest" \
  "failed"

printf '{not-json\n' > "$malformed_record"

jq '.git_sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
  "$valid_record" > "$wrong_sha_record"

jq '.image_uri = "333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' \
  "$valid_record" > "$wrong_image_uri_record"

jq '.health.status = "unhealthy"' \
  "$valid_record" > "$unhealthy_record"

jq '.version.observed = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    | .version.matches = false' \
  "$valid_record" > "$version_mismatch_record"

jq '.schema_version = 2' \
  "$valid_record" > "$wrong_schema_record"

jq 'del(.verification_timestamp)' \
  "$valid_record" > "$missing_provenance_record"

jq 'del(.task_definition_arn)' \
  "$valid_record" > "$missing_task_definition_record"

jq 'del(.workflow_run_id)' \
  "$valid_record" > "$missing_workflow_run_record"

run_case \
  "missing deployment record" \
  1 \
  "No verified deployment record exists" \
  "missing"

run_case \
  "deployment record inaccessible without ListBucket" \
  1 \
  "Deployment record unavailable or inaccessible" \
  "access-denied"

run_case \
  "malformed deployment record" \
  1 \
  "Deployment record is malformed JSON" \
  "record" \
  "$malformed_record"

run_case \
  "different environment" \
  1 \
  "Deployment record .environment mismatch" \
  "record" \
  "$wrong_environment_record"

run_case \
  "different image digest" \
  1 \
  "Deployment record .image_digest mismatch" \
  "record" \
  "$wrong_digest_record"

run_case \
  "record not verified" \
  1 \
  "Deployment record .verification_status mismatch" \
  "record" \
  "$unverified_record"

run_case \
  "different Git SHA" \
  1 \
  "Deployment record .git_sha mismatch" \
  "record" \
  "$wrong_sha_record"

run_case \
  "different image URI" \
  1 \
  "Deployment record .image_uri mismatch" \
  "record" \
  "$wrong_image_uri_record"

run_case \
  "unhealthy historical verification" \
  1 \
  "Deployment record .health.status mismatch" \
  "record" \
  "$unhealthy_record"

run_case \
  "version verification mismatch" \
  1 \
  "Deployment record .version.observed mismatch" \
  "record" \
  "$version_mismatch_record"

run_case \
  "unsupported schema version" \
  1 \
  "Deployment record .schema_version mismatch" \
  "record" \
  "$wrong_schema_record"

run_case \
  "missing verification timestamp" \
  1 \
  "Deployment record .verification_timestamp is required" \
  "record" \
  "$missing_provenance_record"

run_case \
  "missing task definition provenance" \
  1 \
  "Deployment record .task_definition_arn is required" \
  "record" \
  "$missing_task_definition_record"

run_case \
  "missing workflow run provenance" \
  1 \
  "Deployment record .workflow_run_id is required" \
  "record" \
  "$missing_workflow_run_record"

run_case \
  "known-good verified record" \
  0 \
  "Rollback eligibility: ELIGIBLE" \
  "record" \
  "$valid_record"

echo
echo "All rollback eligibility tests passed."
