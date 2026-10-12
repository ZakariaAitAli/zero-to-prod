#!/usr/bin/env bash
# Tests the security lab's startup permission check with simulated token
# responses. It needs no lab, no Docker, and no network, and never touches a
# lab's state: the tool runs with a throwaway project and state path.
set -euo pipefail

root_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tool="${root_dir}/tools/security-lab-local"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
failures=0

# A token response whose access token carries the given resource_access
# claim (JSON). The signature is a placeholder: the check reads claims only.
response() {
  python3 -I -c '
import base64, json, sys
enc = lambda o: base64.urlsafe_b64encode(json.dumps(o).encode()).rstrip(b"=").decode()
claims = {"iss": "https://auth.work-items.localhost:9443/realms/work-items", "azp": "work-items-automation"}
if sys.argv[1] != "none":
    claims["resource_access"] = json.loads(sys.argv[1])
print(json.dumps({"access_token": enc({"alg": "RS256"}) + "." + enc(claims) + ".signature", "token_type": "Bearer"}))
' "$1"
}

check() {
  local name="$1" input="$2" expected="$3" output
  output="$(printf '%s' "$input" | ZTP_SECURITY_LAB_PROJECT=zero-to-prod-security-lab-test \
    ZTP_SECURITY_LAB_STATE_DIR="${work}/state" "$tool" check-machine-permissions 2>&1)"
  local result=pass
  case "$expected" in
    silent) [[ -z "$output" ]] || result=fail ;;
    *) grep -qF -- "$expected" <<< "$output" || result=fail ;;
  esac
  # The warning reports observations only: no cause and no destructive advice.
  if grep -qiE 'predates|destroy|purge' <<< "$output"; then result=fail; fi
  if [[ "$expected" != silent ]] && ! grep -qF 'verify-realm' <<< "$output"; then result=fail; fi
  printf '%s %s\n' "$(tr '[:lower:]' '[:upper:]' <<< "$result")" "$name"
  if [[ "$result" == fail ]]; then
    printf '%s\n' "$output" | sed 's/^/    /'
    failures=$((failures + 1))
  fi
}

check "all expected permissions" \
  "$(response '{"work-items-api":{"roles":["items.read","items.process"]}}')" silent
check "expected permissions plus others" \
  "$(response '{"work-items-api":{"roles":["items.read","items.process","items.create"]},"account":{"roles":["view-profile"]}}')" silent
check "no resource_access claim" "$(response none)" "lacks: items.read, items.process"
check "no work-items-api entry" "$(response '{"account":{"roles":["view-profile"]}}')" "lacks: items.read, items.process"
check "one permission missing" "$(response '{"work-items-api":{"roles":["items.read"]}}')" "lacks: items.process."
check "permission names only in another client" \
  "$(response '{"work-items-gateway":{"roles":["items.read","items.process"]}}')" "lacks: items.read, items.process"
check "non-string role entries ignored" \
  "$(response '{"work-items-api":{"roles":["items.read",7]}}')" "lacks: items.process."
check "empty response (token request failed)" "" "could not be checked"
check "error response" '{"error":"unauthorized_client"}' "could not be checked"
check "not a JWT" '{"access_token":"opaque-value"}' "could not be checked"

if [[ "$failures" -ne 0 ]]; then
  echo "Startup permission check tests failed: ${failures}." >&2
  exit 1
fi
echo "All startup permission check tests passed."
