#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 <task-definition.json>" >&2
  exit 2
fi

task_definition_file="$1"

if [ ! -f "$task_definition_file" ]; then
  echo "::error::Task definition file not found: ${task_definition_file}" >&2
  exit 1
fi

if ! jq -e '
  type == "object" and
  (.containerDefinitions | type == "array")
' "$task_definition_file" >/dev/null 2>&1; then
  echo "::error::Task definition must be a JSON object with containerDefinitions" >&2
  exit 1
fi

demo_api_count="$(
  jq '
    [
      .containerDefinitions[]
      | select(.name == "demo-api")
    ]
    | length
  ' "$task_definition_file"
)"

if [ "$demo_api_count" -ne 1 ]; then
  echo "::error::Expected exactly one demo-api container, found ${demo_api_count}" >&2
  exit 1
fi

if ! jq -e '
  .containerDefinitions[]
  | select(.name == "demo-api")
  | (.image | type == "string")
' "$task_definition_file" >/dev/null 2>&1; then
  echo "::error::demo-api container image must be a string" >&2
  exit 1
fi

jq -S -c '
  (
    .containerDefinitions[]
    | select(.name == "demo-api")
    | .image
  ) = "__RELEASE_IMAGE_EXCLUDED__"
' "$task_definition_file" \
  | sha256sum \
  | awk '{print "sha256:" $1}'
