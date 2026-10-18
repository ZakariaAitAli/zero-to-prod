#!/usr/bin/env bash
# Tests the security lab's application-hop material (ADR 0008): the
# application CA and its policy, the API's and oauth2-proxy's certificates,
# trust separation from the other CAs, oauth2-proxy's HTTPS-only
# configuration, the ingress's upstream verification directives, the
# Compose mounts, and the preflight refusals. It needs no lab, no Docker, and
# no network; the tool runs with a throwaway project and state path.
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
app_ca="${pki}/app-ca"
app_tls="${state}/app-tls"
o2p_cfg="${state}/secrets/oauth2-proxy.cfg"

check_cmd "check-secrets passes after init" lab check-secrets

# --- The application CA and its certificates --------------------------------
check "application CA is CA:TRUE, pathlen:0" "$(
  openssl x509 -in "${app_ca}.crt" -noout -ext basicConstraints | grep -q 'CA:TRUE, pathlen:0' && echo pass || echo fail)"
check "application CA differs from the lab, internal, and identity-provider CAs" "$(
  key="$(openssl x509 -in "${app_ca}.crt" -noout -pubkey)"
  for other in ca internal-ca idp-ca; do
    [[ "$key" != "$(openssl x509 -in "${pki}/${other}.crt" -noout -pubkey)" ]] || { echo fail; exit 0; }
  done
  echo pass)"
check "application CA key is mode 600; app-tls and its directories are 711 and hold only tls.pem (644)" "$(
  [[ "$(stat -c '%a' "${app_ca}.key")" == 600 && "$(stat -c '%a' "$app_tls")" == 711 &&
     "$(stat -c '%a' "${app_tls}/api")" == 711 && "$(stat -c '%a' "${app_tls}/oauth2-proxy")" == 711 &&
     "$(stat -c '%a' "${app_tls}/api/tls.pem")" == 644 &&
     "$(find "$app_tls" -type f -printf '%f\n' | sort -u)" == tls.pem ]] && echo pass || echo fail)"

for service in api oauth2-proxy; do
  file="${app_tls}/${service}/tls.pem"
  check_cmd "${service}: certificate verifies for its name against the application CA" \
    openssl verify -CAfile "${app_ca}.crt" -verify_hostname "$service" -purpose sslserver "$file"
  for other in ca internal-ca idp-ca; do
    check_not "${service}: certificate does not verify against ${other}" openssl verify -CAfile "${pki}/${other}.crt" "$file"
  done
done
check_not "the api certificate does not verify for the name oauth2-proxy" \
  openssl verify -CAfile "${app_ca}.crt" -verify_hostname oauth2-proxy "${app_tls}/api/tls.pem"
for file in "${state}/idp-tls/tls.pem" "${state}/internal-tls/postgres/tls.pem" "${state}/ingress-tls/tls.pem"; do
  check_not "$(basename "$(dirname "$file")") certificate does not verify against the application CA" \
    openssl verify -CAfile "${app_ca}.crt" "$file"
done

# Certificates signed with the application CA's own key (as a stolen key
# could), and controls from a CA without constraints.
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
openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
  -keyout "${work}/open-ca.key" -out "${work}/open-ca.crt" -days 1 -subj "/CN=unconstrained control" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign" 2>/dev/null

for name in api oauth2-proxy; do
  sign "$app_ca" "exact-${name}" "DNS:${name}"
  check "positive: the application CA's ${name} certificate is accepted for ${name}" "$(
    [[ "$(verdict "$app_ca" "exact-${name}" "$name")" == ok ]] && echo pass || echo fail)"
done
# The dialed name: the first subject alternative name without "DNS:".
host_of() { local s="${1%%,*}"; echo "${s#DNS:}"; }
for san in "DNS:api,DNS:extra.api" "DNS:oauth2-proxy,DNS:extra.oauth2-proxy" "DNS:api,IP:127.0.0.1" "DNS:api,IP:::1"; do
  sign "$app_ca" "excluded-${san//[:.,]/-}" "$san"
  check "negative: ${san} is refused although the dialed name matches" "$(
    [[ "$(verdict "$app_ca" "excluded-${san//[:.,]/-}" "$(host_of "$san")")" == "excluded subtree violation" ]] &&
      echo pass || echo fail)"
  sign "${work}/open-ca" "open-${san//[:.,]/-}" "$san"
  check "positive control: ${san} from an unconstrained CA verifies" "$(
    [[ "$(verdict "${work}/open-ca" "open-${san//[:.,]/-}" "$(host_of "$san")")" == ok ]] && echo pass || echo fail)"
done
for name in keycloak postgres work-items.localhost; do
  sign "$app_ca" "other-${name}" "DNS:${name}"
  check "negative: the application CA cannot vouch for ${name}" "$(
    [[ "$(verdict "$app_ca" "other-${name}")" == "permitted subtree violation" ]] && echo pass || echo fail)"
done
for ca in ca internal-ca idp-ca; do
  sign "${pki}/${ca}" "${ca}-api" "DNS:api"
  check "negative: ${ca} cannot vouch for api" "$(
    [[ "$(verdict "${pki}/${ca}" "${ca}-api")" == "permitted subtree violation" ]] && echo pass || echo fail)"
done

# --- oauth2-proxy serves only HTTPS ------------------------------------------
check "oauth2-proxy.cfg disables the plaintext listener and serves HTTPS on 4443 from the mounted file" "$(
  grep -qx 'http_address = "-"' "$o2p_cfg" && grep -qx 'https_address = "0.0.0.0:4443"' "$o2p_cfg" &&
    grep -qx 'tls_cert_file = "/run/oauth2-proxy-tls/tls.pem"' "$o2p_cfg" &&
    grep -qx 'tls_key_file = "/run/oauth2-proxy-tls/tls.pem"' "$o2p_cfg" &&
    grep -qx 'tls_min_version = "TLS1.2"' "$o2p_cfg" && echo pass || echo fail)"

# --- The ingress verifies both hops ------------------------------------------
ingress="${root_dir}/infra/security-lab/ingress"
for service in api oauth2-proxy; do
  include="${ingress}/${service}-upstream.conf"
  check "${service}-upstream.conf verifies chain and name ${service} against the application CA, without session reuse" "$(
    grep -qx 'proxy_ssl_verify on;' "$include" &&
      grep -qx 'proxy_ssl_trusted_certificate /run/app-ca/ca.crt;' "$include" &&
      grep -qx "proxy_ssl_name ${service};" "$include" &&
      grep -qx 'proxy_ssl_server_name on;' "$include" &&
      grep -qx 'proxy_ssl_session_reuse off;' "$include" && echo pass || echo fail)"
done
nginx_conf="${ingress}/nginx.conf"
check "nginx.conf has no plaintext upstream to the API or oauth2-proxy" "$(
  ! grep -qE 'http://(api|oauth2-proxy|work_items_api)' "$nginx_conf" && ! grep -qE 'api:8080|oauth2-proxy:4180' "$nginx_conf" &&
    echo pass || echo fail)"
check "every proxy_pass to the API or oauth2-proxy is in a location with its include" "$(
  python3 -I - "$nginx_conf" <<'PY'
import re, sys
text = open(sys.argv[1]).read()
bad = 0
for m in re.finditer(r"location[^{]*\{", text):
    depth, i = 1, m.end()
    while depth and i < len(text):
        depth += {"{": 1, "}": -1}.get(text[i], 0)
        i += 1
    body = text[m.end():i]
    if re.search(r"proxy_pass\s+(https://work_items_api|\$oauth2_proxy|\$api_diag)", body):
        want = "api-upstream.conf" if ("work_items_api" in body or "$api_diag" in body) else "oauth2-proxy-upstream.conf"
        bad += want not in body
    if re.search(r"proxy_pass\s+\$oauth2_proxy_diag", body):
        bad += "oauth2-proxy-upstream.conf" not in body
print("pass" if bad == 0 else "fail")
PY
)"
check "the API upstream re-resolves at run time and bounds kept-alive connections" "$(
  sed -n '/upstream work_items_api {/,/}/p' "$nginx_conf" | grep -q 'server api:8443 resolve;' &&
    sed -n '/upstream work_items_api {/,/}/p' "$nginx_conf" | grep -q 'keepalive_time 5m;' && echo pass || echo fail)"
check "the ingress image contains both includes" "$(
  grep -q 'api-upstream.conf /etc/nginx/api-upstream.conf' "${ingress}/Dockerfile" &&
    grep -q 'oauth2-proxy-upstream.conf /etc/nginx/oauth2-proxy-upstream.conf' "${ingress}/Dockerfile" &&
    grep -qx '!infra/security-lab/ingress/api-upstream.conf' "${ingress}/Dockerfile.dockerignore" &&
    grep -qx '!infra/security-lab/ingress/oauth2-proxy-upstream.conf' "${ingress}/Dockerfile.dockerignore" &&
    echo pass || echo fail)"

compose_file="${root_dir}/infra/security-lab/compose.yaml"
service_of() {
  awk -v var="$1" '/^  [a-z0-9-]+:$/ {service = $1} index($0, var) {print service}' "$compose_file" | sort -u | paste -sd, -
}
check "the application CA is mounted into the ingress only" "$([[ "$(service_of ZTP_SECURITY_LAB_APP_CA_FILE)" == "ingress:" ]] && echo pass || echo fail)"
check "app-tls is mounted into the API and oauth2-proxy only, each its own directory" "$(
  [[ "$(service_of 'ZTP_SECURITY_LAB_APP_TLS_DIR:?set by tools/security-lab-local}/api:')" == "api:" &&
     "$(service_of 'ZTP_SECURITY_LAB_APP_TLS_DIR:?set by tools/security-lab-local}/oauth2-proxy:')" == "oauth2-proxy:" ]] &&
    echo pass || echo fail)"
check "the API listens with TLS on 8443 (PORT, TLS_CERT_FILE)" "$(
  sed -n '/^  api:$/,/^  [a-z]/p' "$compose_file" | grep -q 'PORT: "8443"' &&
    sed -n '/^  api:$/,/^  [a-z]/p' "$compose_file" | grep -q 'TLS_CERT_FILE: /run/api-tls/tls.pem' && echo pass || echo fail)"

# --- Policy validation (shared validator) and preflight refusals ------------------
make_app_ca() {
  openssl req -x509 -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc \
    -keyout "${app_ca}.key" -out "${app_ca}.crt" -days 1 -subj "/CN=policy case" \
    -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -addext "nameConstraints=$1" 2>/dev/null
}
fingerprint() { openssl x509 -noout -fingerprint -sha256 -in "${app_ca}.crt"; }
permitted="permitted;DNS:api,permitted;DNS:oauth2-proxy"
dns_x="excluded;DNS:.api,excluded;DNS:.oauth2-proxy"
ip_x="excluded;IP:0.0.0.0/0.0.0.0,excluded;IP:::/::"
policy_case() {
  local label="$1" constraints="$2" reason="$3" before output service
  lab init >/dev/null
  make_app_ca "$constraints"
  before="$(fingerprint)"
  for service in api oauth2-proxy; do
    sign "$app_ca" "weak-${service}" "DNS:${service}"
    cat "${work}/weak-${service}.key" "${work}/weak-${service}.crt" > "${app_tls}/${service}/tls.pem"
  done
  output="$(lab check-secrets 2>&1 || true)"
  check "policy: check-secrets refuses an application CA with ${label}" "$(
    grep -q "application CA's name constraints do not limit it to exactly api oauth2-proxy" <<< "$output" &&
      grep -qF "$reason" <<< "$output" && echo pass || echo fail)"
  output="$(lab init 2>&1)"
  check "policy: init replaces it and reissues both certificates (${label})" "$(
    grep -q 'Replacing the application CA' <<< "$output" &&
      [[ "$(grep -c 'Issuing application certificate' <<< "$output")" == 2 && "$(fingerprint)" != "$before" ]] &&
      openssl verify -CAfile "${app_ca}.crt" -verify_hostname api "${app_tls}/api/tls.pem" >/dev/null 2>&1 &&
      openssl verify -CAfile "${app_ca}.crt" -verify_hostname oauth2-proxy "${app_tls}/oauth2-proxy/tls.pem" >/dev/null 2>&1 &&
      lab check-secrets >/dev/null 2>&1 && echo pass || echo fail)"
}
policy_case "an additional permitted DNS name" "critical,${permitted},permitted;DNS:keycloak,${dns_x},${ip_x}" "keycloak"
policy_case "only one service name" "critical,permitted;DNS:api,${dns_x},${ip_x}" "permitted subtrees are"
policy_case "no IP exclusions" "critical,${permitted},${dns_x}" "missing [('ipv4', 'all'), ('ipv6', 'all')]"
policy_case "a non-critical extension" "${permitted},${dns_x},${ip_x}" "not critical"
lab init >/dev/null
make_app_ca "critical,${ip_x},excluded;DNS:.oauth2-proxy,permitted;DNS:oauth2-proxy,excluded;DNS:.api,permitted;DNS:api"
before="$(fingerprint)"
output="$(lab init 2>&1)"
check "policy: init keeps a valid application CA in another order" "$(
  grep -q 'Application CA exists' <<< "$output" && [[ "$(fingerprint)" == "$before" ]] && echo pass || echo fail)"
check_cmd "policy: check-secrets accepts it" lab check-secrets

cp "$o2p_cfg" "${work}/o2p.cfg"
sed -i 's/^http_address = "-"$/http_address = "0.0.0.0:4180"/' "$o2p_cfg"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses an oauth2-proxy configuration with a plaintext listener" "$(
  grep -q 'oauth2-proxy.cfg does not restrict oauth2-proxy to HTTPS' <<< "$output" && echo pass || echo fail)"
check "the refusal does not print the configuration's secrets" "$(
  for name in gateway-client-secret oauth2-proxy-cookie-secret valkey-password; do
    grep -qF "$(tr -d '\n' < "${state}/secrets/${name}")" <<< "$output" && { echo fail; exit 0; }
  done
  echo pass)"
cp "${work}/o2p.cfg" "$o2p_cfg"

cp "${state}/internal-tls/postgres/tls.pem" "${app_tls}/api/tls.pem"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses an API certificate from another CA" "$(
  grep -q 'the api certificate does not verify for the name api against the application CA' <<< "$output" && echo pass || echo fail)"
cp "${app_tls}/api/tls.pem" "${work}/keep.pem"
rm -f "${app_tls}/oauth2-proxy/tls.pem"
output="$(lab check-secrets 2>&1 || true)"
check "check-secrets refuses a missing oauth2-proxy TLS file" "$(
  grep -q 'required application TLS file missing or empty' <<< "$output" && echo pass || echo fail)"
lab init >/dev/null
check_cmd "init reissues both and check-secrets passes" lab check-secrets

if [[ "$failures" -ne 0 ]]; then
  echo "Application TLS tests failed: ${failures}." >&2
  exit 1
fi
echo "All application TLS tests passed."
