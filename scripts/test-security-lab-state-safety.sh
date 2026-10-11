#!/usr/bin/env bash
# Regression tests for security lab state-directory validation and cleanup.
#
# Every test runs with a fake HOME and TMPDIR inside a temporary fixture.
# Destructive commands only ever target fixture directories; unsafe real
# locations (/, the repository) are exercised only through the read-only
# state-dir command.
set -euo pipefail

root_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
tool="${root_dir}/tools/security-lab-local"

fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT

fake_home="${fixture}/home"
fake_tmp="${fixture}/tmp"
state_base="${fake_home}/.local/state"
mkdir -p "$state_base" "$fake_tmp" "${fixture}/elsewhere"

failures=0

run_tool() {
  local state_dir="$1"
  shift
  env -u XDG_STATE_HOME HOME="$fake_home" TMPDIR="$fake_tmp" \
    ZTP_SECURITY_LAB_STATE_DIR="$state_dir" "$tool" "$@"
}

pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; failures=$((failures + 1)); }

expect_rejected() {
  local name="$1" state_dir="$2"
  shift 2
  if run_tool "$state_dir" "$@" >/dev/null 2>&1; then
    fail "$name (accepted ${state_dir})"
  else
    pass "$name"
  fi
}

expect_accepted() {
  local name="$1" state_dir="$2"
  shift 2
  local output
  if output="$(run_tool "$state_dir" "$@" 2>&1)"; then
    pass "$name"
  else
    fail "$name: ${output}"
  fi
}

assert_exists() {
  if [[ -e "$2" ]]; then pass "$1"; else fail "$1 (${2} missing)"; fi
}

assert_missing() {
  if [[ ! -e "$2" ]]; then pass "$1"; else fail "$1 (${2} still exists)"; fi
}

# --- Location validation (read-only) ----------------------------------------
expect_rejected "relative path" "relative/state" state-dir
expect_rejected "filesystem root" "/" state-dir
expect_rejected "home directory" "$fake_home" state-dir
expect_rejected "ancestor of home" "$fixture" state-dir
expect_rejected "state base itself" "$state_base" state-dir
expect_rejected "outside allowed bases" "${fixture}/elsewhere/lab" state-dir
expect_rejected "repository root" "$root_dir" state-dir
expect_rejected "inside repository" "${root_dir}/.security-lab-state" state-dir

ln -s "$root_dir" "${fake_tmp}/repo-alias"
expect_rejected "symlink alias to repository" "${fake_tmp}/repo-alias/state" state-dir
ln -s "$fake_home" "${state_base}/home-alias"
expect_rejected "symlink alias to home" "${state_base}/home-alias" state-dir
expect_rejected "dot-dot escape to home" "${state_base}/../../" state-dir

expect_accepted "below state base" "${state_base}/zero-to-prod/lab" state-dir
expect_accepted "below temp base" "${fake_tmp}/lab" state-dir

canonical="$(run_tool "${fake_tmp}/./x/../lab" state-dir)"
if [[ "$canonical" == "$(realpath -m -- "${fake_tmp}/lab")" ]]; then
  pass "path is canonicalized"
else
  fail "path is canonicalized (got ${canonical})"
fi

# --- Destructive commands refuse unrelated directories -----------------------
expect_rejected "purge refuses fake home" "$fake_home" purge-state --yes
assert_exists "fake home survives" "${fake_home}/.local"

unrelated="${state_base}/unrelated"
mkdir -p "$unrelated"
echo keep > "${unrelated}/keep"
expect_rejected "purge refuses unmarked directory" "$unrelated" purge-state --yes
expect_rejected "init refuses unrelated directory" "$unrelated" init
assert_exists "unrelated content survives" "${unrelated}/keep"
assert_missing "init did not mark unrelated directory" "${unrelated}/.zero-to-prod-security-lab"

other="${state_base}/other-lab"
mkdir -p "$other"
echo 'project=zero-to-prod-security-lab-other' > "${other}/.zero-to-prod-security-lab"
expect_rejected "purge refuses another lab's marker" "$other" purge-state --yes
assert_exists "other lab survives" "${other}/.zero-to-prod-security-lab"

# --- Lab-owned state can be created and purged --------------------------------
lab="${state_base}/zero-to-prod/lab"
expect_accepted "init creates lab state" "$lab" init
assert_exists "init writes ownership marker" "${lab}/.zero-to-prod-security-lab"
touch "${lab}/unexpected"
expect_rejected "purge refuses lab state with unexpected entries" "$lab" purge-state --yes
assert_exists "lab state survives refused purge" "${lab}/pki/ca.key"
rm -f "${lab}/unexpected"
expect_accepted "purge removes lab state" "$lab" purge-state --yes
assert_missing "lab state removed" "$lab"
assert_exists "parent of lab state kept" "${state_base}/zero-to-prod"

# State created before the marker existed is adopted only if it holds lab
# entries alone.
legacy="${state_base}/zero-to-prod/legacy"
mkdir -p "${legacy}/pki" "${legacy}/secrets"
expect_accepted "init adopts unmarked lab-only state" "$legacy" init
assert_exists "adopted state is marked" "${legacy}/.zero-to-prod-security-lab"

expect_rejected "purge requires --yes" "$legacy" purge-state
assert_exists "state survives purge without --yes" "$legacy"

if [[ "$failures" -ne 0 ]]; then
  echo "Security lab state-safety tests failed: ${failures}." >&2
  exit 1
fi

echo
echo "All security lab state-safety tests passed."
