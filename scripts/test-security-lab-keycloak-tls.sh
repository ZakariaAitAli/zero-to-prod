#!/usr/bin/env bash
# Tests the security lab's identity-provider hop material (ADR 0007): the
# identity-provider CA, Keycloak's certificate, the trust separation from the
# lab CA and the internal CA, Keycloak's HTTPS-only configuration, the
# ingress's upstream verification directives, and the preflight refusals. It
# needs no lab, no Docker, and no network; the tool runs with a throwaway
# project and state path.
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

check_cmd() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then check "$name" pass; else check "$name" fail; fi
}

check_not() {
  local name="$1"; shift
  if "$@" >/dev/null 2>&1; then check "$name" fail; else check "$name" pass; fi
}

lab init >/dev/null

pki="${state}/pki"
idp_ca="${pki}/idp-ca"
keycloak="${state}/idp-tls/tls.pem"
conf="${state}/secrets/keycloak.conf"

check_cmd "check-secrets passes after init" lab check-secrets

# --- The identity-provider CA ------------------------------------------------
constraints="$(openssl x509 -in "${idp_ca}.crt" -noout -ext nameConstraints,basicConstraints)"
check "identity-provider CA permits only the name keycloak" "$(
  [[ "$(sed -n '/Permitted:/,/Excluded:/p' <<< "$constraints" | grep 'DNS:' | tr -d ' ' | paste -sd, -)" == "DNS:keycloak" ]] &&
    echo pass || echo fail)"
check "identity-provider CA excludes its subdomains (.keycloak) and IP addresses" "$(
  excluded="$(sed -n '/Excluded:/,$p' <<< "$constraints" | tr -d ' ' | grep -E '^(DNS|IP):' | paste -sd, -)"
  [[ "$excluded" == "DNS:.keycloak,IP:0.0.0.0/0.0.0.0,IP:0:0:0:0:0:0:0:0/0:0:0:0:0:0:0:0" ]] && echo pass || echo fail)"
check "identity-provider CA cannot issue intermediate CAs" "$(grep -q 'pathlen:0' <<< "$constraints" && echo pass || echo fail)"
check "identity-provider CA differs from the lab CA and the internal CA" "$(
  key="$(openssl x509 -in "${idp_ca}.crt" -noout -pubkey)"
  [[ "$key" != "$(openssl x509 -in "${pki}/ca.crt" -noout -pubkey)" &&
     "$key" != "$(openssl x509 -in "${pki}/internal-ca.crt" -noout -pubkey)" ]] && echo pass || echo fail)"
check "identity-provider CA key is mode 600; idp-tls is mode 711 and holds only tls.pem (644)" "$(
  [[ "$(stat -c '%a' "${idp_ca}.key")" == 600 && "$(stat -c '%a' "${state}/idp-tls")" == 711 &&
     "$(stat -c '%a' "$keycloak")" == 644 && "$(find "${state}/idp-tls" -type f -printf '%f\n')" == tls.pem ]] &&
    echo pass || echo fail)"

# --- Keycloak's certificate and trust separation ---------------------------------
check_cmd "keycloak certificate verifies for the name keycloak against the identity-provider CA" \
  openssl verify -CAfile "${idp_ca}.crt" -verify_hostname keycloak -purpose sslserver "$keycloak"
check_not "keycloak certificate does not verify against the lab CA (browser-trusted)" \
  openssl verify -CAfile "${pki}/ca.crt" "$keycloak"
check_not "keycloak certificate does not verify against the internal CA (datastores)" \
  openssl verify -CAfile "${pki}/internal-ca.crt" "$keycloak"
check_not "the postgres certificate does not verify against the identity-provider CA" \
  openssl verify -CAfile "${idp_ca}.crt" "${state}/internal-tls/postgres/tls.pem"
check_not "the ingress certificate does not verify against the identity-provider CA" \
  openssl verify -CAfile "${idp_ca}.crt" "${state}/ingress-tls/tls.pem"

# Certificates signed with each CA's own key (as a stolen key could), checked
# against the CA that signed them.
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

sign "$idp_ca" exact "DNS:keycloak"
check "positive: the identity-provider CA's keycloak certificate is accepted for keycloak" "$(
  [[ "$(verdict "$idp_ca" exact keycloak)" == ok ]] && echo pass || echo fail)"
sign "$idp_ca" sub "DNS:extra.keycloak"
check "negative: the identity-provider CA cannot vouch for extra.keycloak" "$(
  [[ "$(verdict "$idp_ca" sub extra.keycloak)" == "excluded subtree violation" ]] && echo pass || echo fail)"
sign "$idp_ca" both "DNS:keycloak,DNS:extra.keycloak"
check "negative: keycloak plus extra.keycloak is refused although the dialed name matches" "$(
  [[ "$(verdict "$idp_ca" both keycloak)" == "excluded subtree violation" ]] && echo pass || echo fail)"
for ip in IP:172.18.0.5 IP:127.0.0.1 IP:::1; do
  sign "$idp_ca" "ip-${ip//[:.]/-}" "DNS:keycloak,${ip}"
  check "negative: keycloak plus ${ip#IP:} is refused although the dialed name matches" "$(
    [[ "$(verdict "$idp_ca" "ip-${ip//[:.]/-}" keycloak)" == "excluded subtree violation" ]] && echo pass || echo fail)"
done

# Positive controls for the refusals above: the same subject alternative names
# signed by a CA without name constraints verify for keycloak, so the
# identity-provider CA's constraints, not the certificates, cause the refusals.
openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
  -keyout "${work}/open-ca.key" -out "${work}/open-ca.crt" -days 1 -subj "/CN=unconstrained control" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign" 2>/dev/null
for san in DNS:keycloak,DNS:extra.keycloak DNS:keycloak,IP:172.18.0.5 DNS:keycloak,IP:127.0.0.1 DNS:keycloak,IP:::1; do
  sign "${work}/open-ca" "open-${san//[:.,]/-}" "$san"
  check "positive control: ${san} from an unconstrained CA verifies for keycloak" "$(
    [[ "$(verdict "${work}/open-ca" "open-${san//[:.,]/-}" keycloak)" == ok ]] && echo pass || echo fail)"
done

for name in postgres auth.work-items.localhost; do
  sign "$idp_ca" "other-${name}" "DNS:${name}"
  check "negative: the identity-provider CA cannot vouch for ${name}" "$(
    [[ "$(verdict "$idp_ca" "other-${name}")" == "permitted subtree violation" ]] && echo pass || echo fail)"
done
sign "${pki}/internal-ca" internal-keycloak "DNS:keycloak"
check "negative: the internal CA cannot vouch for keycloak" "$(
  [[ "$(verdict "${pki}/internal-ca" internal-keycloak)" == "permitted subtree violation" ]] && echo pass || echo fail)"
sign "${pki}/ca" lab-keycloak "DNS:keycloak"
check "negative: the lab CA cannot vouch for keycloak" "$(
  [[ "$(verdict "${pki}/ca" lab-keycloak)" == "permitted subtree violation" ]] && echo pass || echo fail)"
check "negative: a valid keycloak certificate is refused for another dialed name" "$(
  [[ "$(verdict "$idp_ca" exact postgres)" == "hostname mismatch" ]] && echo pass || echo fail)"

# --- Keycloak serves only HTTPS ---------------------------------------------------
check "keycloak.conf disables the HTTP listener" "$(
  grep -qx 'http-enabled=false' "$conf" && ! grep -q '^http-enabled=true' "$conf" && echo pass || echo fail)"
check "keycloak.conf serves HTTPS on 8443 from the mounted tls.pem, reread periodically" "$(
  grep -qx 'https-port=8443' "$conf" &&
    grep -qx 'https-certificate-file=/run/keycloak-tls/tls.pem' "$conf" &&
    grep -qx 'https-certificate-key-file=/run/keycloak-tls/tls.pem' "$conf" &&
    grep -qE '^https-certificates-reload-period=[0-9]+s$' "$conf" && echo pass || echo fail)"
check "keycloak.conf keeps the public issuer and forwarded-header handling" "$(
  grep -qx 'hostname=https://auth.work-items.localhost:9443' "$conf" &&
    grep -qx 'proxy-headers=xforwarded' "$conf" && echo pass || echo fail)"
check "keycloak.conf binds the plain-HTTP management interface to loopback" "$(
  grep -qx 'http-management-scheme=http' "$conf" && grep -qx 'http-management-host=127.0.0.1' "$conf" &&
    echo pass || echo fail)"

# --- The ingress verifies Keycloak ------------------------------------------------
include="${root_dir}/infra/security-lab/ingress/keycloak-upstream.conf"
nginx_conf="${root_dir}/infra/security-lab/ingress/nginx.conf"
check "ingress upstream directives verify chain and name, without session reuse" "$(
  grep -qx 'proxy_ssl_verify on;' "$include" &&
    grep -qx 'proxy_ssl_trusted_certificate /run/idp-ca/ca.crt;' "$include" &&
    grep -qx 'proxy_ssl_name keycloak;' "$include" &&
    grep -qx 'proxy_ssl_server_name on;' "$include" &&
    grep -qx 'proxy_ssl_session_reuse off;' "$include" && echo pass || echo fail)"
check "every Keycloak upstream in nginx.conf is https://keycloak:8443 with the include" "$(
  [[ "$(grep -c 'set \$keycloak https://keycloak:8443;' "$nginx_conf")" == 2 &&
     "$(grep -c 'include /etc/nginx/keycloak-upstream.conf;' "$nginx_conf")" == 2 ]] &&
    ! grep -q 'http://keycloak' "$nginx_conf" && echo pass || echo fail)"
# The tool's only http://keycloak URL is the deliberate plaintext probe of the
# TLS port in verify-keycloak-tls; its admin-API client uses HTTPS.
check "the tool's admin-API client uses https://keycloak:8443; no URL targets port 8080" "$(
  grep -qx '        kc=https://keycloak:8443' "$tool" && ! grep -q 'http://keycloak:8080' "$tool" &&
    [[ "$(grep -c 'http://keycloak' "$tool")" == 1 ]] && echo pass || echo fail)"
check "the identity-provider CA is mounted into the ingress only" "$(
  [[ "$(grep -c 'ZTP_SECURITY_LAB_IDP_CA_FILE' "${root_dir}/infra/security-lab/compose.yaml")" == 1 ]] &&
    awk '/^  [a-z0-9-]+:$/ {service = $1} /ZTP_SECURITY_LAB_IDP_CA_FILE/ {print service}' \
      "${root_dir}/infra/security-lab/compose.yaml" | grep -qx 'ingress:' && echo pass || echo fail)"

# --- Preflight refusals, each restored afterwards -----------------------------------
cp "$conf" "${work}/keycloak.conf"
sed -i 's/^http-enabled=false$/http-enabled=true/' "$conf"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses a Keycloak configuration with the HTTP listener" "$(
  grep -q 'keycloak.conf does not restrict Keycloak to HTTPS' <<< "$output" && echo pass || echo fail)"
check "the refusal does not print the configuration's database password" "$(
  grep -qF "$(tr -d '\n' < "${state}/secrets/keycloak-db-password")" <<< "$output" && echo fail || echo pass)"
cp "${work}/keycloak.conf" "$conf"

serial="$(openssl x509 -noout -serial -in "$keycloak")"
cp "${state}/internal-tls/postgres/tls.pem" "$keycloak"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses a Keycloak certificate from another CA or for another name" "$(
  grep -q 'the Keycloak certificate does not verify for the name keycloak' <<< "$output" && echo pass || echo fail)"
lab init >/dev/null
check_cmd "init reissues the wrong Keycloak certificate" lab check-secrets
check "the reissued certificate is new" "$(
  [[ "$(openssl x509 -noout -serial -in "$keycloak")" != "$serial" ]] && echo pass || echo fail)"

# An identity-provider CA without the subdomain exclusion is refused and
# replaced.
openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
  -keyout "${idp_ca}.key" -out "${idp_ca}.crt" -days 1 -subj "/CN=subtree control" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign" \
  -addext "nameConstraints=critical,permitted;DNS:keycloak" 2>/dev/null
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses an identity-provider CA that permits subdomains" "$(
  grep -q "identity-provider CA's name constraints do not limit it to exactly keycloak" <<< "$output" && echo pass || echo fail)"
output="$(lab init 2>&1)"
check "init replaces that CA and reissues the Keycloak certificate" "$(
  grep -q 'Replacing the identity-provider CA' <<< "$output" &&
    grep -q 'Issuing Keycloak certificate' <<< "$output" && echo pass || echo fail)"
check_cmd "check-secrets passes after the replacement" lab check-secrets

rm -f "$keycloak"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses a missing Keycloak TLS file" "$(
  grep -q 'required identity-provider TLS file missing or empty' <<< "$output" && echo pass || echo fail)"
lab init >/dev/null
check_cmd "check-secrets passes after init restores it" lab check-secrets

if [[ "$failures" -ne 0 ]]; then
  echo "Keycloak TLS tests failed: ${failures}." >&2
  exit 1
fi
echo "All Keycloak TLS tests passed."
