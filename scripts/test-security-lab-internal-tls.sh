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
permitted="$(sed -n '/Permitted:/,/Excluded:/p' <<< "$constraints" | grep 'DNS:' | tr -d ' ' | sort | paste -sd, -)"
excluded="$(sed -n '/Excluded:/,$p' <<< "$constraints" | grep 'DNS:' | tr -d ' ' | sort | paste -sd, -)"
check "internal CA permits the DNS subtrees postgres and rabbitmq" "$(
  [[ "$permitted" == "DNS:postgres,DNS:rabbitmq" ]] && echo pass || echo fail)"
check "internal CA excludes their subdomains (.postgres, .rabbitmq)" "$(
  [[ "$excluded" == "DNS:.postgres,DNS:.rabbitmq" ]] && echo pass || echo fail)"
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

# The CA's permitted names, checked with certificates signed by the CA's
# own key (as a stolen key could). Each client additionally verifies the one
# name it dialed; that is checked separately with -verify_hostname above.
# sign <ca-dir-prefix> <name> <san>: prints OpenSSL's verdict for the chain
# and, when given, the dialed name.
sign() {
  local ca="$1" name="$2" san="$3"
  openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
    -keyout "${work}/${name}.key" -out "${work}/${name}.csr" -subj "/CN=${name}" 2>/dev/null
  printf 'subjectAltName=%s\nextendedKeyUsage=serverAuth\n' "$san" > "${work}/${name}.ext"
  openssl x509 -req -in "${work}/${name}.csr" -CA "${ca}.crt" -CAkey "${ca}.key" \
    -days 1 -extfile "${work}/${name}.ext" -out "${work}/${name}.crt" 2>/dev/null
}
verdict() {
  local ca="$1" name="$2" host="${3:-}" args=()
  [[ -n "$host" ]] && args=(-verify_hostname "$host")
  if out="$(openssl verify -CAfile "${ca}.crt" "${args[@]}" "${work}/${name}.crt" 2>&1)"; then
    echo ok
  else
    grep -oE 'permitted subtree violation|excluded subtree violation|hostname mismatch' <<< "$out" | head -n 1
  fi
}
internal_ca="${pki}/internal-ca"

sign "$internal_ca" exact-postgres "DNS:postgres"
check "positive: exact name postgres is accepted for the dialed name postgres" "$(
  [[ "$(verdict "$internal_ca" exact-postgres postgres)" == ok ]] && echo pass || echo fail)"
sign "$internal_ca" exact-rabbitmq "DNS:rabbitmq"
check "positive: exact name rabbitmq is accepted for the dialed name rabbitmq" "$(
  [[ "$(verdict "$internal_ca" exact-rabbitmq rabbitmq)" == ok ]] && echo pass || echo fail)"
for name in extra.postgres extra.rabbitmq; do
  sign "$internal_ca" "$name" "DNS:${name}"
  check "negative: subdomain ${name} is outside the CA's names" "$(
    [[ "$(verdict "$internal_ca" "$name" "$name")" == "excluded subtree violation" ]] && echo pass || echo fail)"
done
sign "$internal_ca" both-postgres "DNS:postgres,DNS:extra.postgres"
check "negative: postgres plus extra.postgres is refused although the dialed name matches" "$(
  [[ "$(verdict "$internal_ca" both-postgres postgres)" == "excluded subtree violation" ]] && echo pass || echo fail)"
sign "$internal_ca" keycloak "DNS:keycloak"
check "negative: another name (keycloak) is outside the CA's names" "$(
  [[ "$(verdict "$internal_ca" keycloak)" == "permitted subtree violation" ]] && echo pass || echo fail)"
check "negative: a valid rabbitmq certificate is refused for the dialed name postgres" "$(
  [[ "$(verdict "$internal_ca" exact-rabbitmq postgres)" == "hostname mismatch" ]] && echo pass || echo fail)"

# Control: a CA with only the permitted subtrees (the previous policy) does
# accept the subdomain, so the cases above detect the exclusion.
openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
  -keyout "${work}/subtree-ca.key" -out "${work}/subtree-ca.crt" -days 1 -subj "/CN=subtree control" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign" \
  -addext "nameConstraints=critical,permitted;DNS:postgres,permitted;DNS:rabbitmq" 2>/dev/null
sign "${work}/subtree-ca" control-extra "DNS:extra.postgres"
check "control: a subtree-only CA accepts extra.postgres" "$(
  [[ "$(verdict "${work}/subtree-ca" control-extra extra.postgres)" == ok ]] && echo pass || echo fail)"
# An internal CA from before the exclusion (as created by the previous
# version of init) must be refused and replaced.
cp "${work}/subtree-ca.crt" "${pki}/internal-ca.crt"
cp "${work}/subtree-ca.key" "${pki}/internal-ca.key"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses an internal CA that permits subdomains" "$(
  grep -q "name constraints do not limit it to exactly" <<< "$output" && echo pass || echo fail)"
output="$(lab init 2>&1)"
check "init replaces that CA and reissues both certificates" "$(
  grep -q 'Replacing the internal CA' <<< "$output" &&
    [[ "$(grep -c 'Issuing internal certificate' <<< "$output")" == 2 ]] && echo pass || echo fail)"
check_cmd "check-secrets passes after the replacement" lab check-secrets

# The full ADR 0006 policy, checked on the DER encoding: exactly DNS:postgres
# and DNS:rabbitmq permitted; .postgres, .rabbitmq, all IPv4, and all IPv6
# excluded; critical; nothing else. Each invalid CA is refused by
# check-secrets, then replaced by init, which reissues both datastore
# certificates; both verify against the replacement CA.
make_internal_ca() {
  openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
    -keyout "${internal_ca}.key" -out "${internal_ca}.crt" -days 1 -subj "/CN=policy case" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -addext "nameConstraints=$1" 2>/dev/null
}
fingerprint() { openssl x509 -noout -fingerprint -sha256 -in "${internal_ca}.crt"; }
permitted_both="permitted;DNS:postgres,permitted;DNS:rabbitmq"
dns_exclusions="excluded;DNS:.postgres,excluded;DNS:.rabbitmq"
ip_exclusions="excluded;IP:0.0.0.0/0.0.0.0,excluded;IP:::/::"
policy_case() {
  local label="$1" constraints="$2" reason="$3" before output service
  lab init >/dev/null
  make_internal_ca "$constraints"
  before="$(fingerprint)"
  # Both certificates from the weak CA, as the previous init kept and issued
  # them, so the CA's policy is the only thing check-secrets can object to.
  for service in postgres rabbitmq; do
    sign "$internal_ca" "weak-${service}" "DNS:${service}"
    cat "${work}/weak-${service}.key" "${work}/weak-${service}.crt" > "${internal}/${service}/tls.pem"
  done
  output="$(lab check-secrets 2>&1 || true)"
  check "policy: check-secrets refuses an internal CA with ${label}" "$(
    grep -q "internal CA's name constraints do not limit it to exactly postgres rabbitmq" <<< "$output" &&
      grep -qF "$reason" <<< "$output" && echo pass || echo fail)"
  output="$(lab init 2>&1)"
  check "policy: init replaces the internal CA with ${label} and reissues both certificates" "$(
    grep -q 'Replacing the internal CA' <<< "$output" &&
      [[ "$(grep -c 'Issuing internal certificate' <<< "$output")" == 2 && "$(fingerprint)" != "$before" ]] &&
      echo pass || echo fail)"
  check "policy: after replacing the CA with ${label}, both certificates verify and check-secrets passes" "$(
    openssl verify -CAfile "${internal_ca}.crt" -verify_hostname postgres -purpose sslserver "${internal}/postgres/tls.pem" >/dev/null 2>&1 &&
      openssl verify -CAfile "${internal_ca}.crt" -verify_hostname rabbitmq -purpose sslserver "${internal}/rabbitmq/tls.pem" >/dev/null 2>&1 &&
      lab check-secrets >/dev/null 2>&1 && echo pass || echo fail)"
}
policy_case "an additional permitted DNS name" \
  "critical,${permitted_both},permitted;DNS:attacker.example,${dns_exclusions},${ip_exclusions}" "attacker.example"
policy_case "an additional permitted IP range" \
  "critical,${permitted_both},permitted;IP:10.0.0.0/255.0.0.0,${dns_exclusions},${ip_exclusions}" "permitted subtrees are"
policy_case "only one of the two service names permitted" \
  "critical,permitted;DNS:postgres,${dns_exclusions},${ip_exclusions}" "permitted subtrees are"
policy_case "no IPv4 exclusion" \
  "critical,${permitted_both},${dns_exclusions},excluded;IP:::/::" "missing [('ipv4', 'all')]"
policy_case "no IPv6 exclusion" \
  "critical,${permitted_both},${dns_exclusions},excluded;IP:0.0.0.0/0.0.0.0" "missing [('ipv6', 'all')]"
policy_case "no IP exclusions and an extra permitted name" \
  "critical,${permitted_both},permitted;DNS:attacker.example,${dns_exclusions}" "permitted subtrees are"
policy_case "a non-critical extension" \
  "${permitted_both},${dns_exclusions},${ip_exclusions}" "not critical"

# The valid policy, written in another order, is kept by init.
lab init >/dev/null
make_internal_ca "critical,excluded;IP:::/::,excluded;DNS:.rabbitmq,permitted;DNS:rabbitmq,${ip_exclusions%%,*},excluded;DNS:.postgres,permitted;DNS:postgres"
before="$(fingerprint)"
output="$(lab init 2>&1)"
check "policy: init keeps a valid internal CA whose subtrees are in another order" "$(
  grep -q 'Internal CA exists' <<< "$output" && [[ "$(fingerprint)" == "$before" ]] && echo pass || echo fail)"
check_cmd "policy: check-secrets accepts that valid internal CA" lab check-secrets
check "policy: the internal CA init creates passes the same check" "$(
  rm -f "${internal_ca}.key" "${internal_ca}.crt"; lab init >/dev/null
  lab check-secrets >/dev/null 2>&1 && echo pass || echo fail)"

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

# Renewal confirmation: five consecutive equal observations within the
# attempt limit, never fewer (the rule issue-internal-certificate and
# verify-internal-tls use). streak <name> <exit> <output prefix> <attempts>
# <observations…>
streak() {
  local name="$1" want_exit="$2" want_output="$3" attempts="$4" output rc=0
  shift 4
  output="$(printf '%s\n' "$@" | lab check-serial-streak "0 NEW" 5 "$attempts" 2>&1)" || rc=$?
  check "streak: ${name}" "$(
    [[ "$rc" == "$want_exit" && "$output" == "$want_output"* ]] && echo pass || echo fail)"
  [[ "$rc" == "$want_exit" && "$output" == "$want_output"* ]] || printf '    exit=%s output=%s\n' "$rc" "$output"
}
repeat() { local i; for (( i = 0; i < $1; i++ )); do echo "$2"; done; }
mapfile -t old59 < <(repeat 59 "0 OLD")
mapfile -t old60 < <(repeat 60 "0 OLD")

streak "five consecutive matches succeed" 0 "0 NEW" 60 "0 NEW" "0 NEW" "0 NEW" "0 NEW" "0 NEW"
streak "old serial, then five matches succeed" 0 "0 NEW" 60 "0 OLD" "0 OLD" "0 NEW" "0 NEW" "0 NEW" "0 NEW" "0 NEW"
streak "intermittent matches reset the streak (4, old, 4)" 1 "timeout: 4 of 5" 9 \
  "0 NEW" "0 NEW" "0 NEW" "0 NEW" "0 OLD" "0 NEW" "0 NEW" "0 NEW" "0 NEW"
streak "a failed verification resets the streak" 1 "timeout: 1 of 5" 6 \
  "0 NEW" "0 NEW" "0 NEW" "0 NEW" "21 NEW" "0 NEW"
streak "a single match on the final attempt fails (reviewer reproduction)" 1 "timeout: 1 of 5" 60 \
  "${old59[@]}" "0 NEW"
streak "no match before timeout fails" 1 "timeout: 0 of 5" 60 "${old60[@]}"
streak "a timeout never prints the expected observation" 1 "timeout:" 3 "0 NEW" "0 NEW" "0 NEW"
# Failure experiments wait for their own nonzero code with the bad serial.
output="$(printf '21 BAD\n%.0s' 1 2 3 4 5 | lab check-serial-streak "21 BAD" 2>&1)" && rc=0 || rc=$?
check "streak: five consecutive nonzero-code observations succeed for a failure case" "$(
  [[ "$rc" == 0 && "$output" == "21 BAD" ]] && echo pass || echo fail)"

if [[ "$failures" -ne 0 ]]; then
  echo "Internal TLS tests failed: ${failures}." >&2
  exit 1
fi
echo "All internal TLS tests passed."
