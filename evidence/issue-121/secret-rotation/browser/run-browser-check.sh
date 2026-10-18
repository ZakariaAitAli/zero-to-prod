#!/usr/bin/env bash
# Reproduces the browser check of the application TLS experiment: headless
# Chromium (Playwright 1.49.1) drives the real Web UI of a running security
# lab, trusting only that lab's CA (NSS database), with no certificate error
# ignored. Results and screenshots are written to this directory.
#
# Chromium resolves *.localhost to its own loopback and ignores /etc/hosts,
# so the browser container shares the ingress container's network namespace:
# 127.0.0.1:9443 is then the ingress's own listener, as 127.0.0.1:9443 is on
# Windows. Only the two test users' password files are mounted (read-only);
# no value is printed or recorded.
#
# Preparation downloads the playwright npm package and Ubuntu's
# libnss3-tools (for certutil) in a throwaway container.
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
project="${ZTP_SECURITY_LAB_PROJECT:?set ZTP_SECURITY_LAB_PROJECT to the lab project}"
state="${ZTP_SECURITY_LAB_STATE_DIR:-${XDG_STATE_HOME:-${HOME}/.local/state}/zero-to-prod/${project}}"
image="mcr.microsoft.com/playwright:v1.49.1-noble@sha256:70e367e0cbf60340a5b5fd562f6247a34eb3196efab9f88a3dd56482d9fe09d2"
work="$(mktemp -d)"
trap 'docker run --rm -v "${work}:/w" --entrypoint rm "$image" -rf /w/app /w/nssdb >/dev/null 2>&1 || true; rm -rf -- "$work"' EXIT

mkdir -p "${work}/app" "${work}/nssdb"
chmod 777 "${work}/app" "${work}/nssdb"
cp "${here}/browser-check.js" "${work}/app/"
install -m 644 "${state}/pki/ca.crt" "${work}/lab-ca.crt"

docker run --rm -v "${work}:/w" --entrypoint bash "$image" -c '
  set -e
  cd /w/app && npm init -y >/dev/null && npm install --no-audit --no-fund playwright@1.49.1 >/dev/null 2>&1
  apt-get update -qq >/dev/null && apt-get install -y -qq --no-install-recommends libnss3-tools >/dev/null 2>&1
  certutil -N --empty-password -d sql:/w/nssdb
  certutil -A -n security-lab-ca -t "C,," -i /w/lab-ca.crt -d sql:/w/nssdb
  chmod -R a+rwX /w/nssdb /w/app'

docker run --rm --network "container:${project}-ingress-1" \
  --cap-drop ALL --security-opt no-new-privileges:true --ipc host --user "$(id -u):$(id -g)" \
  --tmpfs "/tmp/home:uid=$(id -u),gid=$(id -g)" -e OUT_DIR=/out -e HOME=/tmp/home \
  -v "${work}/app:/app:ro" -v "${work}/nssdb:/tmp/home/.pki/nssdb" \
  -v "${state}/secrets/lab-user-password:/secrets/lab-user-password:ro" \
  -v "${state}/secrets/lab-viewer-password:/secrets/lab-viewer-password:ro" \
  -v "${here}:/out" -w /app "$image" node browser-check.js
