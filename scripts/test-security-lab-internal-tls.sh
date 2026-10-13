#!/usr/bin/env bash
# Tests the security lab's internal TLS material (ADR 0006): the internal CA,
# the PostgreSQL and RabbitMQ certificates, the connection URLs, and the
# preflight that refuses plaintext or unverifiable configurations. It needs
# no lab, no Docker, and no network; the tool runs with a throwaway project
# and state path.
set -euo pipefail

root_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tool="${root_dir}/tools/security-lab-local"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
state="${work}/state"
failures=0

lab() {
  ZTP_SECURITY_LAB_PROJECT=zero-to-prod-security-lab-test ZTP_SECURITY_LAB_STATE_DIR="$state" "$tool" "$@"
}

check() {
  local name="$1" result="$2"
  printf '%s %s\n' "$(tr '[:lower:]' '[:upper:]' <<< "$result")" "$name"
  [[ "$result" == pass ]] || failures=$((failures + 1))
}

# check_cmd <name> <command...>: passes when the command succeeds.
check_cmd() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then check "$name" pass; else check "$name" fail; fi
}

# check_not <name> <command...>: passes when the command fails.
check_not() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then check "$name" fail; else check "$name" pass; fi
}

lab init >/dev/null

pki="${state}/pki"
internal="${state}/internal-tls"

check_cmd "check-secrets passes after init" lab check-secrets

constraints="$(openssl x509 -in "${pki}/internal-ca.crt" -noout -ext nameConstraints,basicConstraints)"
check "internal CA permits only postgres and rabbitmq" "$(
  [[ "$(grep -c 'DNS:' <<< "$constraints")" == 2 ]] &&
    grep -q 'DNS:postgres' <<< "$constraints" && grep -q 'DNS:rabbitmq' <<< "$constraints" &&
    echo pass || echo fail)"
check "internal CA cannot issue intermediate CAs" "$(grep -q 'pathlen:0' <<< "$constraints" && echo pass || echo fail)"
check "internal CA differs from the lab CA" "$(
  [[ "$(openssl x509 -in "${pki}/internal-ca.crt" -noout -pubkey)" != "$(openssl x509 -in "${pki}/ca.crt" -noout -pubkey)" ]] &&
    echo pass || echo fail)"

for service in postgres rabbitmq; do
  file="${internal}/${service}/tls.pem"
  check_cmd "${service}: verifies for its name against the internal CA" \
    openssl verify -CAfile "${pki}/internal-ca.crt" -verify_hostname "$service" -purpose sslserver "$file"
  check_not "${service}: does not verify against the lab CA" \
    openssl verify -CAfile "${pki}/ca.crt" "$file"
  check "${service}: directory mode 711, file mode 644" "$(
    [[ "$(stat -c '%a' "${internal}/${service}")" == 711 && "$(stat -c '%a' "$file")" == 644 ]] && echo pass || echo fail)"
done

check_not "postgres certificate does not verify for the name rabbitmq" \
  openssl verify -CAfile "${pki}/internal-ca.crt" -verify_hostname rabbitmq "${internal}/postgres/tls.pem"
check_not "ingress certificate does not verify against the internal CA" \
  openssl verify -CAfile "${pki}/internal-ca.crt" "${state}/ingress-tls/tls.pem"
check "internal CA key is mode 600; mounted directories hold only tls.pem" "$(
  [[ "$(stat -c '%a' "${pki}/internal-ca.key")" == 600 &&
     "$(find "$internal" -type f -printf '%f\n' | sort -u)" == tls.pem ]] &&
    echo pass || echo fail)"

# The name constraint: a certificate for another name, signed with the
# internal CA's own key, is still rejected.
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
  -keyout "${work}/other.key" -out "${work}/other.csr" -subj "/CN=keycloak" 2>/dev/null
printf 'subjectAltName=DNS:keycloak\nextendedKeyUsage=serverAuth\n' > "${work}/other.ext"
openssl x509 -req -in "${work}/other.csr" -CA "${pki}/internal-ca.crt" -CAkey "${pki}/internal-ca.key" \
  -days 1 -extfile "${work}/other.ext" -out "${work}/other.crt" 2>/dev/null
other_result="$(openssl verify -CAfile "${pki}/internal-ca.crt" "${work}/other.crt" 2>&1 || true)"
check "internal CA cannot vouch for another name (keycloak)" "$(
  grep -q 'permitted subtree violation' <<< "$other_result" && echo pass || echo fail)"

secrets="${state}/secrets"
check "database URLs require verify-full with the internal CA" "$(
  grep -L 'sslmode=verify-full&sslrootcert=/internal-ca/ca.crt$' \
    "${secrets}/postgres-migrator-url" "${secrets}/api-database-url" "${secrets}/worker-database-url" | grep -q . &&
    echo fail || echo pass)"
check "AMQP URLs use amqps on 5671 with the internal CA" "$(
  grep -L '^amqps://.*@rabbitmq:5671/.*?cacertfile=/internal-ca/ca.crt$' \
    "${secrets}/api-rabbitmq-url" "${secrets}/worker-rabbitmq-url" | grep -q . &&
    echo fail || echo pass)"

# Preflight refusals, each restored afterwards.
rabbitmq_serial="$(openssl x509 -noout -serial -in "${internal}/rabbitmq/tls.pem")"
cp "${internal}/rabbitmq/tls.pem" "${internal}/postgres/tls.pem"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses a postgres certificate for another name" "$(
  grep -q 'the postgres certificate does not verify for the name postgres' <<< "$output" && echo pass || echo fail)"
lab init >/dev/null
check_cmd "init reissues the wrong postgres certificate" lab check-secrets
check "init keeps the valid rabbitmq certificate" "$(
  [[ "$(openssl x509 -noout -serial -in "${internal}/rabbitmq/tls.pem")" == "$rabbitmq_serial" ]] && echo pass || echo fail)"

cp "${secrets}/api-database-url" "${work}/url"
sed -i 's/sslmode=verify-full&sslrootcert=.*$/sslmode=require/' "${secrets}/api-database-url"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses sslmode=require (no server verification)" "$(
  grep -q 'api-database-url does not require verified TLS' <<< "$output" && echo pass || echo fail)"
check "the refusal does not print the URL" "$(
  grep -q 'postgres://' <<< "$output" && echo fail || echo pass)"
cp "${work}/url" "${secrets}/api-database-url"

cp "${secrets}/worker-rabbitmq-url" "${work}/url"
sed -i 's#^amqps://\(.*\)@rabbitmq:5671/\([^?]*\)?.*$#amqp://\1@rabbitmq:5672/\2#' "${secrets}/worker-rabbitmq-url"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses plaintext AMQP" "$(
  grep -q 'worker-rabbitmq-url does not require verified TLS' <<< "$output" && echo pass || echo fail)"
cp "${work}/url" "${secrets}/worker-rabbitmq-url"

check_cmd "check-secrets passes after restoring" lab check-secrets

if [[ "$failures" -ne 0 ]]; then
  echo "Internal TLS tests failed: ${failures}." >&2
  exit 1
fi
echo "All internal TLS tests passed."
