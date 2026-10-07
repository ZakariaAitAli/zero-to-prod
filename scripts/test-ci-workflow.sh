#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
WORKFLOW="${ROOT_DIR}/.github/workflows/work-items-ci.yml"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
mkdir -p "$test_dir/scripts" "$test_dir/bin"
real_tee="$(command -v tee)"

# Execute the actual workflow run blocks, not a second implementation.
extract_run() {
  awk -v name="$1" '
    $0 == "      - name: " name { found = 1; next }
    found && $0 == "        run: |" { body = 1; next }
    body && /^          / { print substr($0, 11); next }
    body && /^$/ { print; next }
    body { exit }
  ' "$WORKFLOW"
}

extract_run "Detect changed paths" > "$test_dir/detect.sh"
[[ -s "$test_dir/detect.sh" ]]

cat > "$test_dir/scripts/classify-ci-changes.sh" <<'SH'
#!/usr/bin/env bash
printf '%s' "$CLASSIFIER_OUTPUT"
exit "$CLASSIFIER_RC"
SH
cat > "$test_dir/bin/git" <<'SH'
#!/usr/bin/env bash
printf '%s' "$DIFF_OUTPUT"
exit "$DIFF_RC"
SH
cat > "$test_dir/bin/go" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$TEST_OUTPUT"
exit "$TEST_RC"
SH
cat > "$test_dir/bin/tee" <<'SH'
#!/usr/bin/env bash
if [ "$TEE_RC" -ne 0 ]; then
  cat >/dev/null
  exit "$TEE_RC"
fi
exec "$REAL_TEE" "$@"
SH
chmod +x "$test_dir/scripts/classify-ci-changes.sh" "$test_dir/bin/"*

full=$'backend=true\nfrontend=true\nworkflow=true\n'
docs=$'backend=false\nfrontend=false\nworkflow=false\n'

check_detection() {
  local name="$1" expected="$2" event="$3" classifier_rc="$4"
  local classifier_output="$5" diff_rc="$6" diff_output="$7"
  local expected_output="${8:-}" rc=0
  : > "$test_dir/outputs"
  (
    cd "$test_dir"
    env PATH="$test_dir/bin:$PATH" \
      EVENT_NAME="$event" PR_BASE_SHA=base PR_HEAD_SHA=head \
      PUSH_BASE_SHA=base PUSH_HEAD_SHA=head \
      CLASSIFIER_RC="$classifier_rc" CLASSIFIER_OUTPUT="$classifier_output" \
      DIFF_RC="$diff_rc" DIFF_OUTPUT="$diff_output" \
      GITHUB_OUTPUT="$test_dir/outputs" GITHUB_STEP_SUMMARY="$test_dir/summary" \
      bash -e detect.sh
  ) > "$test_dir/log" 2>&1 || rc=$?
  if { [ "$expected" = success ] && [ "$rc" -ne 0 ]; } ||
     { [ "$expected" = failure ] && [ "$rc" -eq 0 ]; }; then
    cat "$test_dir/log"
    echo "FAIL: $name (exit $rc)" >&2
    exit 1
  fi
  if [ "$expected" = failure ] && [ -s "$test_dir/outputs" ]; then
    echo "FAIL: $name emitted classification despite failure" >&2
    exit 1
  fi
  if [ "$expected" = success ] &&
     [ "$(cat "$test_dir/outputs")" != "$expected_output" ]; then
    echo "FAIL: $name emitted unexpected flags" >&2
    exit 1
  fi
  echo "PASS: $name"
}

check_detection "valid docs classification" success push 0 "$docs" 0 README.md "${docs%$'\n'}"
check_detection "manual conservative classification" success workflow_dispatch 0 "$full" 0 '' "${full%$'\n'}"
check_detection "CI control guard" success pull_request 0 "$docs" 0 .github/workflows/work-items-ci.yml "${full%$'\n'}"
check_detection "workflow regression test guard" success push 0 "$docs" 0 scripts/test-ci-workflow.sh "${full%$'\n'}"
check_detection "backup regression test guard" success push 0 "$docs" 0 scripts/test-postgres-backup-validation.sh "${full%$'\n'}"
check_detection "failed git diff with partial output" failure push 0 "$full" 42 README.md
check_detection "failed classifier with complete output" failure push 42 "$docs" 0 README.md
check_detection "failed manual classifier" failure workflow_dispatch 42 "$full" 0 ''
check_detection "empty classifier output" failure push 0 '' 0 README.md
check_detection "missing classifier flag" failure push 0 $'backend=false\nfrontend=false\n' 0 README.md
check_detection "duplicate classifier flag" failure push 0 "${docs}backend=true" 0 README.md
check_detection "invalid boolean" failure push 0 $'backend=maybe\nfrontend=false\nworkflow=false\n' 0 README.md
check_detection "unknown key" failure push 0 "${docs}unknown=false" 0 README.md
check_detection "missing equals" failure push 0 $'backend\nfrontend=false\nworkflow=false\n' 0 README.md
check_detection "embedded blank line" failure push 0 $'backend=false\n\nfrontend=false\nworkflow=false\n' 0 README.md
check_detection "guard cannot rescue malformed output" failure push 0 '' 0 .github/workflows/work-items-ci.yml

for step in "Run API integration tests" "Run worker integration tests"; do
  extract_run "$step" > "$test_dir/integration.sh"
  [[ -s "$test_dir/integration.sh" ]]
  shell_name="$(awk -v name="$step" '
    $0 == "      - name: " name { found = 1; next }
    found && /^        shell:/ { print $2; exit }
    found && /^      - name:/ { exit }
  ' "$WORKFLOW")"
  shell_options=(-e)
  if [ "$shell_name" = bash ]; then
    shell_options=(--noprofile --norc -eo pipefail)
  fi
  for scenario in success test_failure tee_failure skipped_test; do
    test_rc=0 tee_rc=0 test_output=PASS expected=0
    case "$scenario" in
      test_failure) test_rc=17; test_output=FAIL; expected=17 ;;
      tee_failure) tee_rc=23; expected=23 ;;
      skipped_test) test_output='--- SKIP: integration'; expected=1 ;;
    esac
    rc=0
    env PATH="$test_dir/bin:$PATH" RUNNER_TEMP="$test_dir" \
      TEST_RC="$test_rc" TEST_OUTPUT="$test_output" TEE_RC="$tee_rc" \
      REAL_TEE="$real_tee" \
      bash "${shell_options[@]}" "$test_dir/integration.sh" \
      > "$test_dir/log" 2>&1 || rc=$?
    if [ "$rc" -ne "$expected" ]; then
      cat "$test_dir/log"
      echo "FAIL: $step / $scenario expected $expected, got $rc" >&2
      exit 1
    fi
    echo "PASS: $step / $scenario"
  done
done
