#!/usr/bin/env bash
set -uo pipefail

REGION="${AWS_REGION:-eu-west-3}"
ECS_CLUSTER="${ECS_CLUSTER:-zero-to-prod-dev}"
ECS_SERVICE="${ECS_SERVICE:-demo-api}"
LOG_GROUP="${LOG_GROUP:-/zero-to-prod/development/demo-api}"
LOG_STREAM_PREFIX="${LOG_STREAM_PREFIX:-ecs/demo-api}"

EXPECTED_SHA="${EXPECTED_SHA:-unknown}"
EXPECTED_TASK_DEFINITION="${EXPECTED_TASK_DEFINITION:-unknown}"
FAILURE_STAGE="${FAILURE_STAGE:-unknown}"

AWS_CALL_TIMEOUT_SECONDS="${AWS_CALL_TIMEOUT_SECONDS:-15}"
AWS_CONNECT_TIMEOUT_SECONDS="${AWS_CONNECT_TIMEOUT_SECONDS:-3}"
AWS_READ_TIMEOUT_SECONDS="${AWS_READ_TIMEOUT_SECONDS:-5}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

summary="${tmp_dir}/summary.md"

warn() {
  printf '::warning::%s\n' "$1"
}

summary_line() {
  printf '%s\n' "$*" >> "$summary"
}

aws_bounded() {
  local output_file="$1"
  local error_file="$2"
  shift 2

  timeout "${AWS_CALL_TIMEOUT_SECONDS}s" \
    aws "$@" \
      --region "$REGION" \
      --cli-connect-timeout "$AWS_CONNECT_TIMEOUT_SECONDS" \
      --cli-read-timeout "$AWS_READ_TIMEOUT_SECONDS" \
      >"$output_file" 2>"$error_file"
}

summary_line "## Deployment failure diagnostics"
summary_line
summary_line "- Failure stage: \`${FAILURE_STAGE}\`"
summary_line "- Expected Git SHA: \`${EXPECTED_SHA}\`"
summary_line "- Expected task definition: \`${EXPECTED_TASK_DEFINITION}\`"
summary_line "- ECS cluster: \`${ECS_CLUSTER}\`"
summary_line "- ECS service: \`${ECS_SERVICE}\`"
summary_line

service_json="${tmp_dir}/service.json"
service_error="${tmp_dir}/service.err"

if aws_bounded \
  "$service_json" \
  "$service_error" \
  ecs describe-services \
    --cluster "$ECS_CLUSTER" \
    --services "$ECS_SERVICE" \
    --output json
then
  summary_line "### ECS service"
  summary_line

  jq -r '
    .services[0] as $s
    | "- Task definition: `\($s.taskDefinition // "unknown")`\n"
      + "- Desired: `\($s.desiredCount // "unknown")`\n"
      + "- Running: `\($s.runningCount // "unknown")`\n"
      + "- Pending: `\($s.pendingCount // "unknown")`"
  ' "$service_json" >> "$summary"

  summary_line
  summary_line "#### Recent ECS service events"
  summary_line

  jq -r '
    .services[0].events[0:10][]
    | "- `\(.createdAt)`: \(.message)"
  ' "$service_json" >> "$summary"
else
  warn "Could not collect ECS service diagnostics."
  summary_line "### ECS service"
  summary_line
  summary_line "Service diagnostics unavailable."
fi

running_json="${tmp_dir}/running.json"
running_error="${tmp_dir}/running.err"

stopped_json="${tmp_dir}/stopped.json"
stopped_error="${tmp_dir}/stopped.err"

printf '{"taskArns":[]}\n' > "$running_json"
printf '{"taskArns":[]}\n' > "$stopped_json"

if ! aws_bounded \
  "$running_json" \
  "$running_error" \
  ecs list-tasks \
    --cluster "$ECS_CLUSTER" \
    --service-name "$ECS_SERVICE" \
    --desired-status RUNNING \
    --max-results 10 \
    --no-paginate \
    --output json
then
  warn "Could not list RUNNING ECS tasks."
  printf '{"taskArns":[]}\n' > "$running_json"
fi

if ! aws_bounded \
  "$stopped_json" \
  "$stopped_error" \
  ecs list-tasks \
    --cluster "$ECS_CLUSTER" \
    --service-name "$ECS_SERVICE" \
    --desired-status STOPPED \
    --max-results 10 \
    --no-paginate \
    --output json
then
  warn "Could not list STOPPED ECS tasks."
  printf '{"taskArns":[]}\n' > "$stopped_json"
fi

candidate_arns="${tmp_dir}/candidate-arns.txt"

{
  # Service events are checked first because they often contain the task that
  # most recently failed or stopped.
  if [ -s "$service_json" ]; then
    jq -r '.services[0].events[0:10][]?.message // empty' "$service_json" \
      | grep -oE 'task [0-9a-f]{32}' \
      | awk -v region="$REGION" \
            -v cluster="$ECS_CLUSTER" \
            -v account="333534066371" \
        '{
          sub(/^task /, "", $0)
          printf "arn:aws:ecs:%s:%s:task/%s/%s\n",
                 region, account, cluster, $0
        }' \
      || true
  fi

  jq -r '.taskArns[]?' "$running_json"
  jq -r '.taskArns[]?' "$stopped_json"
} | awk '
  NF && !seen[$0]++ {
    if (count < 5) {
      print
      count++
    }
  }
' > "$candidate_arns"

task_json="${tmp_dir}/tasks.json"
task_error="${tmp_dir}/tasks.err"

printf '{"tasks":[],"failures":[]}\n' > "$task_json"

if [ -s "$candidate_arns" ]; then
  mapfile -t task_arns < "$candidate_arns"

  if ! aws_bounded \
    "$task_json" \
    "$task_error" \
    ecs describe-tasks \
      --cluster "$ECS_CLUSTER" \
      --tasks "${task_arns[@]}" \
      --output json
  then
    warn "Could not describe candidate ECS tasks."
    printf '{"tasks":[],"failures":[]}\n' > "$task_json"
  fi
fi

matching_tasks="${tmp_dir}/matching-tasks.json"

jq \
  --arg expected "$EXPECTED_TASK_DEFINITION" \
  '[
    .tasks[]
    | select(
        $expected == "unknown"
        or .taskDefinitionArn == $expected
      )
  ]' \
  "$task_json" > "$matching_tasks"

summary_line
summary_line "### Correlated ECS tasks"
summary_line

matching_count="$(jq 'length' "$matching_tasks")"

if [ "$matching_count" -eq 0 ]; then
  summary_line "No currently describable task matched the expected task definition."
else
  jq -r '
    .[] |
    "- Task: `\(.taskArn)`\n"
    + "  - Last status: `\(.lastStatus // "unknown")`\n"
    + "  - Stop code: `\(.stopCode // "n/a")`\n"
    + "  - Stopped reason: \(.stoppedReason // "n/a")\n"
    + (
        [
          .containers[]?
          | "  - Container `\(.name)`:"
            + " status=`\(.lastStatus // "unknown")`"
            + " health=`\(.healthStatus // "unknown")`"
            + " exitCode=`\(.exitCode // "n/a")`"
            + " reason=\(.reason // "n/a")"
        ]
        | join("\n")
      )
  ' "$matching_tasks" >> "$summary"
fi

failure_count="$(jq '.failures | length' "$task_json")"

if [ "$failure_count" -gt 0 ]; then
  summary_line
  summary_line "#### ECS task lookup failures"
  summary_line

  jq -r '
    .failures[]
    | "- `\(.arn // "unknown")`: \(.reason // "unknown")"
  ' "$task_json" >> "$summary"
fi

summary_line
summary_line "### External verification"
summary_line

if [ -n "${VERIFICATION_DIAGNOSTICS_FILE:-}" ] &&
   [ -s "$VERIFICATION_DIAGNOSTICS_FILE" ]; then
  summary_line '```text'
  head -c 4096 "$VERIFICATION_DIAGNOSTICS_FILE" >> "$summary"
  summary_line
  summary_line '```'
else
  summary_line "No external verifier failure evidence was recorded."
fi

summary_line
summary_line "### Recent application logs"
summary_line

if [ "$matching_count" -eq 0 ]; then
  summary_line "No matching task was available for task-specific log correlation."
else
  while IFS= read -r task_arn; do
    task_id="${task_arn##*/}"
    log_stream="${LOG_STREAM_PREFIX}/${task_id}"

    logs_json="${tmp_dir}/logs-${task_id}.json"
    logs_error="${tmp_dir}/logs-${task_id}.err"

    summary_line
    summary_line "#### Task \`${task_id}\`"
    summary_line
    summary_line "- Log stream: \`${log_stream}\`"
    summary_line

    if aws_bounded \
      "$logs_json" \
      "$logs_error" \
      logs filter-log-events \
        --log-group-name "$LOG_GROUP" \
        --log-stream-names "$log_stream" \
        --limit 20 \
        --no-paginate \
        --output json
    then
      event_count="$(jq '.events | length' "$logs_json")"

      if [ "$event_count" -eq 0 ]; then
        summary_line "No application log events were returned."
      else
        jq -r '
          .events[]
          | (.message // "")
          | gsub("[\r\n]+"; " ")
          | "- " + .
        ' "$logs_json" >> "$summary"
      fi
    else
      warn "Could not retrieve application logs for task ${task_id}."
      summary_line "Application logs unavailable."
    fi
  done < <(jq -r '.[].taskArn' "$matching_tasks")
fi

cat "$summary"

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  cat "$summary" >> "$GITHUB_STEP_SUMMARY"
fi

# Diagnostics are deliberately best-effort.
# Failure to collect evidence must never prevent deployment cleanup.
exit 0
