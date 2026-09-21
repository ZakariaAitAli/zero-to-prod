#!/usr/bin/env bash
set -euo pipefail

digest_script="./scripts/runtime-config-digest.sh"
task_definition="infra/aws/ecs/demo-api-task-definition.json"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

image_only="${tmp_dir}/image-only.json"
cpu_changed="${tmp_dir}/cpu-changed.json"
reformatted="${tmp_dir}/reformatted.json"
missing_demo_api="${tmp_dir}/missing-demo-api.json"

jq '
  (
    .containerDefinitions[]
    | select(.name == "demo-api")
    | .image
  ) = "example.invalid/demo-api:different-image"
' "$task_definition" > "$image_only"

jq '
  .cpu = "512"
' "$task_definition" > "$cpu_changed"

jq -S . "$task_definition" > "$reformatted"

jq '
  .containerDefinitions |=
    map(select(.name != "demo-api"))
' "$task_definition" > "$missing_demo_api"

current_digest="$("$digest_script" "$task_definition")"
image_only_digest="$("$digest_script" "$image_only")"
cpu_changed_digest="$("$digest_script" "$cpu_changed")"
reformatted_digest="$("$digest_script" "$reformatted")"

if [ "$current_digest" != "$image_only_digest" ]; then
  echo "FAIL: image-only change altered runtime config digest"
  exit 1
fi

echo "PASS: application image is excluded"

if [ "$current_digest" = "$cpu_changed_digest" ]; then
  echo "FAIL: runtime configuration change was not detected"
  exit 1
fi

echo "PASS: CPU runtime configuration change is detected"

if [ "$current_digest" != "$reformatted_digest" ]; then
  echo "FAIL: JSON formatting/key order altered runtime config digest"
  exit 1
fi

echo "PASS: JSON formatting/key order is canonicalized"

set +e
output="$("$digest_script" "$missing_demo_api" 2>&1)"
status=$?
set -e

if [ "$status" -eq 0 ]; then
  echo "FAIL: missing demo-api container was accepted"
  exit 1
fi

if ! grep -Fq "Expected exactly one demo-api container" <<<"$output"; then
  echo "FAIL: missing demo-api error was not explicit"
  printf '%s\n' "$output"
  exit 1
fi

echo "PASS: missing demo-api container is rejected"

echo
echo "Current runtime config digest: ${current_digest}"
echo "CPU-changed config digest:      ${cpu_changed_digest}"
echo
echo "All runtime config digest tests passed."
