# Issue #121 — TLS from the ingress to Keycloak

Recorded 2026-10-13; rerun and extended on 2026-10-17 (see
[rerun](#rerun-on-2026-10-17)). This record covers the identity-provider hop decided in
[ADR 0007](../adr/0007-security-lab-identity-provider-transport.md)
(Proposed): Keycloak serves only HTTPS with a certificate from a separate
identity-provider CA, and the ingress verifies it. It builds on the merged
internal TLS slice (#136, [ADR 0006](../adr/0006-security-lab-internal-transport.md)).
It does **not** complete #121: four internal hops stay plaintext, and rate
limiting and secret rotation are open.

## Question

The ingress → Keycloak hop carries person passwords, client secrets,
authorization codes, and tokens. Can it be encrypted and verified without
changing the public issuer, metadata, redirects, or any authentication path?
What happens to new and established connections, and to resumed TLS sessions,
when Keycloak's certificate is untrusted, names the wrong host, or has
expired? Does the lab recover without restarts?

Environment classification: **LOCAL-FIRST**, in a separate security lab
project on one workstation. TLS verification and nginx's upstream behavior are
portable; network membership, Keycloak's reload behavior, and the capture are
specific to these versions on Docker Desktop.

## Environment

| Component | Version or mode |
| --- | --- |
| Host | Windows 11 Pro (build 26300), WSL 2 in NAT mode, Docker Desktop (Engine 29.6.2, Compose 5.3.1) |
| Keycloak | 26.8.0 (Quarkus 3.40.1), digest-pinned, production mode |
| Ingress | nginx 1.30.5 (Alpine, digest-pinned), OpenSSL as shipped in the image |
| Probes | OpenSSL 3.5.8 `s_client` and Perl (RabbitMQ 4.3.6 image); curl 8.22.0; WSL OpenSSL 3.5.5 for issuing |

## Setup

- **Separate lab.** Every run used the new project
  `zero-to-prod-security-lab-kctls`, with its own state directory, CAs,
  realm, and volumes. Before it was created, a check confirmed that no
  container, volume, or state directory of that name existed. The original,
  `-authz`, and `-tls` labs and the development volumes were not started,
  changed, or deleted (see [cleanup](#cleanup-and-final-state)).
- **Before and after in the same lab.** The project was first created and
  started with the tool and Compose files of `main` at `4f8b299` (extracted
  with `git archive`), to measure the hop as it was. The same project was then
  upgraded with this branch's `init` and `start`, which exercised the upgrade
  path of a lab created before this change.
- **Method.** Scripted clients only: curl with cookie jars, OpenSSL and curl
  probe containers on `idp`, a second nginx from the ingress image as a
  control, a raw-socket capture in Keycloak's network namespace, and container
  logs.
- **Browser check: blocked.** On 2026-10-17 a sign-in was attempted in the
  Claude desktop app's built-in browser. It did not load
  `https://work-items.localhost:9443/`, because this separate lab has its own
  lab CA, which Windows does not trust (`verify.json`: "lab CA in Windows
  trust stores: not trusted"). The CA was not added to the Windows trust
  store. **Browser sign-in is unverified in this slice**; every
  authentication result below comes from scripted clients.
- **Evidence provenance.** See [provenance](#evidence-provenance).

## Problem and decision

The [threat model](../architecture/security-lab-threat-model.md) listed this
hop as plaintext on a two-member network. Inspection of the actual connection
paths found more:

| Path | Before |
| --- | --- |
| Ingress → `http://keycloak:8080` (public routes `/realms/work-items/`, `/resources/`) | Plaintext |
| Lab tool admin probe on `idp` → `http://keycloak:8080` (role-management client secret, admin tokens) | Plaintext |
| `verify` reachability probes on `idp` | Plain TCP connects |
| Keycloak management interface `:9000` (health), cluster ports `:7800` and `:57800` | Listening on Keycloak's `idp` address |
| API and oauth2-proxy → issuer | Through the ingress on `app`, TLS with the lab CA (unchanged) |

ADR 0007 compares the lab CA, an extended internal CA, a separate
identity-provider CA, a pinned self-signed certificate, and public or ACME
issuance for the trust arrangement, and server-authenticated TLS, mTLS, TLS
passthrough, and tighter membership for the protection. It chooses a separate
CA limited to exactly `keycloak`, trusted only by the ingress, with
server-authenticated TLS and no upstream session reuse. mTLS was not adopted.
Its benefit here would be access control on `idp`, not transport protection.

## What was built

- **Identity-provider CA** (`init`): ECDSA P-256, 90 days, `pathlen:0`,
  permitted `DNS:keycloak`, excluded `DNS:.keycloak` and all IP addresses.
  Separate from the lab CA and the internal CA; its key stays in `pki/`.
- **Keycloak certificate**: 14 days, SAN `DNS:keycloak`, `idp-tls/tls.pem`
  (key and certificate) mounted read-only; `init` reissues it near expiry or
  when it does not verify; `issue-keycloak-certificate` renews it.
- **Keycloak**: `http-enabled=false`; HTTPS on 8443 from that file, reread
  every 35 s; TLS 1.2/1.3; management (health) plain HTTP on `127.0.0.1:9000`;
  the per-reload INFO logger set to `warn`. `hostname` and `proxy-headers`
  unchanged.
- **Ingress**: `proxy_pass https://keycloak:8443` with
  `keycloak-upstream.conf` (verify on, depth 1, identity-provider CA only,
  name `keycloak` and SNI, TLS 1.2/1.3, session reuse off); a loopback
  diagnostic route that logs the TLS verdict at `error` level.
- **Lab tool**: the admin probe uses HTTPS with the identity-provider CA;
  `check-secrets` refuses a missing, invalid, or wrong-CA certificate, a CA
  with other constraints, and a `keycloak.conf` that enables HTTP;
  `check-keycloak-upstream`, `capture-idp-hop`, and `verify-keycloak-tls`.
- **CI**: `scripts/test-security-lab-keycloak-tls.sh` (CA policy including
  the subdomain and IP-address exclusions with unconstrained-CA positive
  controls, trust separation, Keycloak and nginx configuration, preflight
  refusals; no Docker).

No application code changed.

## Results

Evidence: [`evidence/issue-121/keycloak-tls/`](../../evidence/issue-121/keycloak-tls).
This section reports the first run, on 2026-10-13 (UTC), with files directly
in that directory. The 2026-10-17 rerun, which supersedes most of them, is
reported [below](#rerun-on-2026-10-17).

| Check | Result |
| --- | --- |
| `capture-idp-hop`, configuration of `main` (`before-capture-idp-hop.json`, 22:24) | Credentials and plaintext HTTP found (table below) |
| `verify-keycloak-tls` (`verify-keycloak-tls.json`, 22:25–22:34) | 143 pass, 53 observations, 0 fail |
| `capture-idp-hop` after the change (`after-capture-idp-hop.json`) | No credential, no plaintext HTTP |
| `verify` (`verify.json`; Windows LAN address redacted) | 84 pass, 4 observations, 0 fail: the slice-4 checks plus `keycloak:8080` and `keycloak:9000` refused from `idp`; Keycloak probes now target 8443 |
| `verify-failures` (`missing-secret-failures.json`) | 18 / 18, including four new cases: missing Keycloak TLS file and plaintext `keycloak.conf` (preflight), Keycloak without its TLS file, the ingress without the identity-provider CA. No secret value in any output |
| `verify-realm` (`verify-realm.json`) | 11 / 11 (admin API over verified TLS) |
| `verify-authentication` (`verify-authentication.json`, with the slow expiry checks) | 77 pass, 10 observations, 0 fail |
| `verify-log-hygiene` (`log-hygiene.json`) | 17 pass, 21 observations, 0 fail |
| `verify-authorization` (`verify-authorization.json`, with role removal) | 79 pass, 14 observations, 0 fail; role-mapping changes through the admin API over verified TLS |
| `verify-internal-tls` (`verify-internal-tls.json`) | 94 pass, 35 observations, 0 fail (exercises the shared certificate-issuing code, refactored here) |
| `verify-renewal`, ingress (`ingress-certificate-renewal.json`) | pass |
| `scripts/test-security-lab-keycloak-tls.sh` (local; also in CI) | 37 / 37. Five mutations (verification off, session reuse on, a plaintext upstream in `nginx.conf`, `http-enabled=true`, no subdomain exclusion) each failed it |
| Other security-lab and CI-policy test scripts; Compose model; `git diff --check` | pass (local) |

The authentication, authorization, log-hygiene, realm, and datastore-TLS
counts equal those of the internal TLS slice (#136), so no regression was
detected by them.

### Before and after: what crosses the hop

`capture-idp-hop` joined Keycloak's network namespace with `CAP_NET_RAW`, as a
member of `idp` with raw sockets could, and kept the TCP payload between the
ingress's and Keycloak's `idp` addresses for 20 s. Meanwhile it drove a
sign-in up to the callback, the callback (oauth2-proxy redeems the code
through the ingress), and a client-credentials request. It counted fixed
strings in the capture, which was then deleted; no value was recorded.

| Occurrences in the captured payload | Before (`main`) | After (ADR 0007) |
| --- | --- | --- |
| lab-user password | 1 | 0 |
| Authorization code | 2 | 0 |
| Client secrets in HTTP Basic form (gateway, automation) | 2 | 0 |
| Issued access token | 1 | 0 |
| Plaintext HTTP messages (`HTTP/1.`) | 12 | 0 |
| TLS server name `keycloak` (positive control: the ClientHello is in clear) | 9 (in plaintext pages) | 5 |
| Payload bytes | 26,113 | 39,579 |

Before the change the access log showed the ingress's Keycloak upstream on
port 8080, and Keycloak listened on `:::8080` and `:::9000`. After it, port
8443 and `:::8443` with `127.0.0.1:9000`. The before capture was repeated in
a development run with the same counts.

The raw client secrets (not Basic-encoded) did not appear in either capture:
both clients authenticate with HTTP Basic.

### Baseline after the change

| Hypothesis | Observed |
| --- | --- |
| New connections verify against the identity-provider CA for the name `keycloak` | OpenSSL verify code 0 and the file's serial |
| Neither the lab CA nor the internal CA verifies Keycloak | Code 21 with each |
| No plaintext listener | `keycloak:8080` from `idp`: `refused connects=0`; plain HTTP to 8443: no response (curl exit 52) |
| Health interface not reachable from `idp` | `keycloak:9000`: refused; Keycloak's listeners: `:::8443`, `127.0.0.1:9000`, plus the cluster ports below |
| Public contract unchanged | Discovery digest `8b48b730131d9c24` before and after; issuer `https://auth.work-items.localhost:9443/realms/work-items` with every endpoint below it; the same digest with spoofed `X-Forwarded-*` and `Forwarded` headers |
| Redirects unchanged | The gateway redirects to the public authorization endpoint; the login form posts to `…/login-actions/authenticate`; Keycloak redirects to `https://work-items.localhost:9443/oauth2/callback` |
| Flows work | Callback 302 (code redeemed through the ingress); session 200; create and process `succeeded`; session and machine tokens carry the public issuer; machine token 200; sign-out 302 and the Keycloak session ended (login form) |
| The ingress uses TLS for Keycloak | Every identity-provider upstream in the access log on port 8443 (13 × 200, 1 × 302) |
| The ingress keeps no connection to Keycloak | 0 established connections to 8443 after the requests |

### Failure cases

For each kind the tool installed the certificate, waited until five
consecutive new connections were served it (Keycloak rereads the file every
35 s), then observed:

| Kind | OpenSSL code | Public route (discovery, token request with secret, new sign-in) | Access log | Reason (diagnostic route) | Recovery |
| --- | --- | --- | --- | --- | --- |
| Untrusted CA | 21 | 503 `identity_provider_unavailable` ×3 | `503 upstream=502` on 8443 | `(21:unable to verify the first certificate)` | Renewal; discovery, token, sign-in 200 |
| Lab CA (browser-trusted) | 21 | 503 ×3 | same | `(21:unable to verify the first certificate)` | same |
| Another name (`postgres`) | 47 | 503 ×3 | same | `(47:permitted subtree violation)` | same |
| Expired | 10 | 503 ×3 | same | `(10:certificate has expired)` | same |
| `keycloak` plus excluded `extra.keycloak` | 48 | 503 ×3 | same | `(48:excluded subtree violation)` | same |
| `keycloak` plus excluded `IP:127.0.0.1` (2026-10-17 only) | 48 | 503 ×3 | same | `(48:excluded subtree violation)` | same |

In every case:

- the public server logged no TLS reason (suppressed below `crit`); the
  diagnostic route logged exactly the reason above, with the request line
  `GET /keycloak-upstream` and no credentials;
- a TLS connection opened before the change answered a second request (200,
  one handshake, verify code 0 from before);
- an access token issued before the change was accepted by the API (local
  validation with cached keys);
- the control nginx with the production directives answered 502, and with
  verification off answered 200 (negative control: the test detects missing
  verification);
- neither Keycloak nor the ingress restarted (container start times unchanged
  over the run).

The hostname check by itself was shown with the control nginx dialing the
name `postgres` against the valid certificate: 502,
`upstream SSL certificate does not match "postgres"`. A wrong-name certificate
from the identity-provider CA always also violates its constraints, so in the
"another name" case OpenSSL and nginx report the constraint error (47), which
OpenSSL checks after the name.

### Session reuse and established connections

| Observation | Result |
| --- | --- |
| Saved TLS session offered while the certificate is unchanged | `Reused`: the session was resumed, no certificate was sent, and the verify code 0 is the one stored from the original handshake |
| Same, after two unchanged reload periods (control, 75 s; the file's digest unchanged) | `Reused`, stored verify code 0 (also 8 of 8 attempts over about 80 s in a manual check) |
| Saved session offered after each bad certificate took effect | `New`: Keycloak did not resume it; a full handshake sent the new certificate, and verification failed with its code (21, 21, 47, 10, 48; and 48 for the IP case on 2026-10-17). The probe's `status=200` in these lines is `s_client`, which completes a request despite a failed verification by default; the verify code is the verdict. nginx answered 502 in every such case |
| Saved session offered after a valid renewal | `New`: full handshake, verify code 0 |
| Control nginx, `proxy_ssl_session_reuse on`, variable upstream | 502 for every bad certificate. Whether nginx offered a saved session on these connections was not observed |
| Control nginx, kept-alive upstream connection opened before the change | **200** for every bad certificate |
| Production ingress | No session reuse; 0 connections kept |

What this shows, and what it does not:

- **Observed:** Keycloak resumed a saved session while its certificate was
  unchanged, including across reloads of an unchanged file. After every
  certificate change it declined the saved session and performed a full
  handshake, in which the client received and verified the new certificate.
- **Not observed:** a session actually resumed across a certificate change.
  A resumed session carries no certificate, so a client verifies nothing on
  it. This experiment does **not** show that a resumed session is
  revalidated. It shows only that this server, in these runs, did not
  resume across a change. Why Keycloak declined resumption (for example a
  new TLS context on reload) was not investigated.
- **An established connection did mask the change:** it kept working with
  the certificate it had negotiated.

The production ingress depends on neither server behavior. It keeps no
upstream connections and has `proxy_ssl_session_reuse off`, so each request
performs a full handshake and verifies the certificate served at that moment.
A kept-alive upstream, a reasonable performance change, would hide a bad or
revoked certificate until the connection closes. Enabling session reuse
would make the ingress depend on Keycloak continuing to decline resumption
after a change.

### Renewal

A valid certificate was renewed on the running lab: new connections verified
the new serial, a connection opened before renewal kept working, a new
sign-in and create-and-process `succeeded`, and nothing restarted.

### Logs

The run searched the ingress, oauth2-proxy, control nginx, Keycloak, and API
logs written during it for 32 strings: the long-lived secrets, the codes,
state, session cookies, and access tokens it used, including those sent while
Keycloak's certificate was refused, and their percent-encoded forms. None was
found in any of them. A synthetic log with every string was matched 32 of 32
times (positive control).

## Rerun on 2026-10-17

The first run's evidence came from tool blobs that differ from the final tool
(see [provenance](#evidence-provenance)). The whole suite was therefore
rerun with the final files, in the same `zero-to-prod-security-lab-kctls`
project, state directory, and volumes. A review of the criteria then found
one gap: nothing exercised the identity-provider CA's **IP-address
exclusion** at runtime. CI only checked that the constraint was listed. A
targeted run added it.

### Runs and their file hashes

| Run | Directory | When (UTC) | `tools/security-lab-local` blob | Other tested files |
| --- | --- | --- | --- | --- |
| Full rerun | [`rerun/`](../../evidence/issue-121/keycloak-tls/rerun) | 13:03–13:33 | `b4b2dc455bd38ede6cef3def33f18ffd139f7591` | [`rerun/provenance.json`](../../evidence/issue-121/keycloak-tls/rerun/provenance.json) |
| Targeted `verify-keycloak-tls` | [`rerun-ip/`](../../evidence/issue-121/keycloak-tls/rerun-ip) | 13:36–13:46 | `2b445a3374d607d60ba725da1b624d6eb28077bb` | [`rerun-ip/provenance.json`](../../evidence/issue-121/keycloak-tls/rerun-ip/provenance.json); identical to the full rerun's |

The infrastructure blobs (`compose.yaml` `bbbd6c5`, `nginx.conf` `8d7a968`,
`keycloak-upstream.conf` `b3db075`, `Dockerfile` `b7874b6`) are the same in
both runs and the same as in the first run.

### Results

Counts are from the evidence files. Pass and fail are expectations;
"observations" are recorded values with no expectation. No check failed or
was skipped in either run.

| Check | Evidence | Pass | Fail | Skip | Observations |
| --- | --- | --- | --- | --- | --- |
| `verify-keycloak-tls` (full rerun; superseded, see below) | `rerun/verify-keycloak-tls.json` | 143 | 0 | 0 | 53 |
| `verify-keycloak-tls` with the IP case and isolated control | `rerun-ip/verify-keycloak-tls.json` | 167 | 0 | 0 | 60 |
| `capture-idp-hop` (after the change; observation-only command) | `rerun/after-capture-idp-hop.json` | 0 | 0 | 0 | 7 (every credential and plaintext-HTTP count 0; TLS server name seen) |
| `check-keycloak-upstream` (console only; not counted) | — | | | | HTTP 200, certificate verified |
| `issue-keycloak-certificate` (console only; not counted) | — | | | | five consecutive verified new connections with the new serial |
| `verify` (Windows LAN address redacted) | `rerun/verify.json` | 84 | 0 | 0 | 4 |
| `verify-failures` | `rerun/missing-secret-failures.json` | 18 | 0 | 0 | 0 |
| `verify-realm` | `rerun/verify-realm.json` | 11 | 0 | 0 | 0 |
| `verify-authentication` (with the slow expiry checks) | `rerun/verify-authentication.json` | 77 | 0 | 0 | 10 |
| `verify-log-hygiene` | `rerun/log-hygiene.json` | 17 | 0 | 0 | 21 |
| `verify-authorization` (with role removal) | `rerun/verify-authorization.json` | 79 | 0 | 0 | 14 |
| `verify-internal-tls` | `rerun/verify-internal-tls.json` | 94 | 0 | 0 | 35 |
| `verify-renewal` (ingress; one overall result) | `rerun/ingress-certificate-renewal.json` | 1 | 0 | 0 | 0 |
| `scripts/test-security-lab-keycloak-tls.sh` (local, no Docker) | console | 44 | 0 | 0 | |

Every count equals the first run's. The tool's console summary prints one
total that adds passed checks and observations, so it is not a count of
passed checks:

- the full rerun's `verify-keycloak-tls` printed "passed (196 checks)":
  143 passed checks, 53 observations, 0 failed, 0 skipped;
- the targeted run printed "passed (227 checks)": 167 passed checks,
  60 observations, 0 failed, 0 skipped;
- standalone `capture-idp-hop` printed "passed (7 checks)": 0 checks and
  7 observations. On its own it records counts and asserts nothing. The
  zero-credential and zero-plaintext assertions, with the TLS-server-name
  positive control, are 3 passed checks inside `verify-keycloak-tls`
  (`passive capture of the ingress-Keycloak hop` in both runs' files).

### The IP-exclusion case

In the live lab, Keycloak was given a certificate from the identity-provider
CA naming both `DNS:keycloak` and `IP:127.0.0.1`. The ingress dials
`keycloak`, so hostname verification succeeds on its own; only the CA's
`excluded;IP:0.0.0.0/0.0.0.0` constraint can refuse it.

- **Valid certificate accepted:** before the case, the diagnostic route
  answered 200 with the valid `DNS:keycloak` certificate. The baseline and
  every earlier recovery did too.
- **Mixed certificate refused at nginx's upstream boundary:** OpenSSL code
  48. Discovery, the token request, and a new sign-in answered 503
  `identity_provider_unavailable`, three times `503 upstream=502` on 8443 in
  the access log. The diagnostic route answered 502 and logged
  `upstream SSL certificate verify error: (48:excluded subtree violation)`.
  The established connection, the saved-session probe (`New verify=48`), the
  earlier machine token, and the control nginx behaved as in the other
  cases.
- **Recovery:** after renewal, OpenSSL code 0 with the new serial;
  discovery, client-credentials token, and sign-in followed by
  `GET /api/items` all 200; diagnostic route 200.
- **Isolated positive control:** on a separate internal Docker network
  (`<project>-ipcontrol`), a stand-in TLS server with the alias `keycloak`
  served each certificate in turn. The verifier was nginx from the ingress
  image with the production `keycloak-upstream.conf`; only the trust anchor
  mounted at `/run/idp-ca/ca.crt` changed between cases.

  | Server certificate | Verifier trusts | Result |
  | --- | --- | --- |
  | `DNS:keycloak` from the identity-provider CA | identity-provider CA | 200 (the setup works) |
  | `DNS:keycloak, IP:127.0.0.1` from the identity-provider CA | identity-provider CA | 502, `(48:excluded subtree violation)` |
  | `DNS:keycloak, IP:127.0.0.1` from an unconstrained control CA | that control CA | **200** |

  The ingress's trust anchor had the identity-provider CA's SHA-256 before
  and after the control. The control CA's key existed only in the run's
  temporary directory, and the network and containers were removed.

CI covers the same rule offline. Certificates naming `keycloak` with
`IP:172.18.0.5`, `IP:127.0.0.1`, or `IP:::1` are refused with an excluded-subtree
violation. The same names, and `keycloak` plus `extra.keycloak`, from an
unconstrained CA verify for `keycloak` (positive controls). A mutation check
(not in CI) showed the exclusion matters: a CA with only `permitted;DNS:keycloak`
and `excluded;DNS:.keycloak` signed a `keycloak` + `IP:127.0.0.1` certificate
that OpenSSL accepted.

### What the rerun supersedes

- **`rerun-ip/verify-keycloak-tls.json` supersedes `rerun/verify-keycloak-tls.json`**,
  which supersedes the first run's `verify-keycloak-tls.json`. Each is a
  superset of the one before: the targeted run adds the IP case (19 passes,
  6 observations) and the isolated control (5 passes, 1 observation). Its
  other checks repeat the full rerun's with the same verdicts.
- **The other `rerun/` files supersede the first run's files of the same
  name** as the current evidence for the final files. Their verdicts and
  counts are unchanged.
- **Not superseded:** `before-capture-idp-hop.json` (the hop under `main`'s
  configuration) was not repeated, because it requires downgrading the lab.
  It remains the only "before" evidence. The first run's files are kept
  unchanged as the original record.
- **Assessment of the tool edit between the two runs.** The edit adds the
  `ip` kind to `issue_keycloak_certificate`, adds `ip` to the expected-code
  and expected-reason tables, and adds the `ip` case, the isolated-control
  functions, and their cleanup to `verify_keycloak_tls`. It also updates the
  help text and the evidence description. No other command calls these
  paths. `init`, `check-secrets`, and `issue-keycloak-certificate` use only
  the unchanged `valid` kind. So `verify`, `verify-failures`, `verify-realm`,
  `verify-authentication`, `verify-log-hygiene`, `verify-authorization`,
  `verify-internal-tls`, `verify-renewal`, and `capture-idp-hop` from the full
  rerun stay valid for the final tool, and were not rerun.

  Checked against the actual diff: reverse-applying the edit to the final
  tool reproduces blob `b4b2dc4` exactly. The diff has nine hunks: help text,
  the `ip` kind, the two expected-result tables, the isolated-control block,
  and three changes inside `verify_keycloak_tls` (cleanup, the case loop,
  and the evidence description). Every other call of
  `issue_keycloak_certificate` passes `valid`. The call that passes the case
  kind is in `keycloak_tls_failure_case`, which only `verify_keycloak_tls`
  calls.

### Log hygiene

- **Successful authentication:** `verify-log-hygiene` drove a successful
  sign-in, a machine request, and a session refresh, plus failed callbacks
  (reused code, Keycloak down, gateway down). It searched the ingress and
  oauth2-proxy logs (expectations) and the Keycloak, API, Valkey, and worker
  logs (observations) for 29 distinct actual values and 10 percent-encoded
  forms: codes from three flows, states from four, the session cookie before
  and after refresh, a session and a machine access token, and long-lived
  secrets. All counts were 0. Positive controls: a synthetic log matched
  29 of 29, and the run's own control path and callback request IDs were
  found in the logs searched.
- **Upstream TLS failures:** `verify-keycloak-tls` searched the ingress,
  oauth2-proxy, and control-nginx logs written during the run for 32 actual
  values, including those sent while Keycloak's certificate was refused (the
  automation client secret in each refused token request, codes, state,
  cookies, access tokens): 0 in each. The Keycloak and API logs (recorded as
  observations) also had 0. A synthetic log with all 32 was matched 32
  times. A second positive control confirmed the ingress log did hold the
  run's diagnostic TLS reasons, so the search covered the failure period.
- **Evidence:** a separate scan of every evidence file in
  `evidence/issue-121/keycloak-tls/` found no value of the lab's 21 runtime
  secret files (passwords, client secrets, connection URLs) and no private
  key; a planted value was found.

## Observations

1. **The two-member premise was already false.** The tool's admin probe sent
   the role-management client secret to Keycloak in plaintext, and ADR 0006's
   threat model did not list it.
2. **Keycloak's reload is periodic and silent about change.** It rereads the
   file every 35 s and logged about 20 INFO lines per reload whether or not the
   file changed (360 lines in 10 minutes). The logger is now at `warn`; the
   served serial is the evidence of a renewal.
3. **Keycloak accepts a combined key-and-certificate PEM** for both
   `https-certificate-file` and `https-certificate-key-file`, which keeps the
   single-rename renewal used elsewhere.
4. **Error visibility had to be designed.** The public server's error-log
   suppression, which keeps authorization codes out of logs, also hid every
   upstream TLS reason. A separate loopback route with a credential-free
   request restores the reason at the ingress boundary.
5. **Writing an HTTP request into `s_client` before the handshake finished**
   got no response, and s_client prints the session-ticket dump without a
   trailing newline. Both broke the first development runs' probes (see
   [development runs](#development-runs)).
6. **Keycloak's cache cluster** (JGroups `:7800` with mTLS, certificate issuer
   `CN=jgroups`; failure detection `:57800`, plain TCP connect accepted) listens
   on Keycloak's `idp` address. In each of the three recreations of Keycloak
   against the existing database whose log was checked, it reported a stale cluster
   member and "Split-brain detected … should not be serving requests";
   Keycloak served requests normally. The first start on an empty database
   did not log it. The stale member is the previous container, so this
   follows from recreating containers, not from TLS; it was not investigated
   further. Neither listener nor message is addressed by this slice.

## Development runs

Kept out of the evidence directory; summarized here:

- Run 1 aborted at the first failure case: the probe's `pkill -f est.in`
  matched and killed its own shell (exit 143). The exit trap removed the probe
  and control and left the valid certificate.
- Run 2 completed with 6 failures, all the established-connection check: the
  status line followed the ticket dump on the same line, so an anchored match
  found nothing. Every other check matched the final run.
- 2026-10-17: the browser attempt described in [setup](#setup) failed to
  load the page (untrusted lab CA). Neither the full rerun nor the targeted
  run had a failed or aborted attempt.

## What this does not prove

- Anything about the hops left plaintext (ingress → API and oauth2-proxy,
  oauth2-proxy → Valkey, Keycloak → its database).
- That an attacker could do more than the capture shows; no active
  impersonation was run on `idp`. Impersonation is covered by verification:
  every certificate not from the identity-provider CA, or not for `keycloak`,
  was refused.
- Access control to Keycloak on `idp`: any member can still reach every path
  over TLS. There is no TLS client-certificate (mTLS) authentication on the
  hop. Credential authentication does exist inside TLS: oauth2-proxy and
  the machine clients authenticate to Keycloak with their client secrets
  (client credentials), people with their passwords.
- Behavior at the real expiry of a running certificate (simulated with one
  issued already expired), CA rotation, revocation, or automated renewal.
- Browser sign-in: the attempted browser check was blocked by the separate
  lab's untrusted lab CA, so browser sign-in is unverified in this slice.
  The scripted checks show that the public TLS, names, metadata, and
  redirects a browser would see did not change.
- Other verifiers' reading of the leading-dot exclusion (OpenSSL in nginx and
  the probes is the only verifier on this hop).
- Single runs.

## Remaining gaps

- Plaintext internal hops, with ADR 0006's reconsideration conditions.
- No mTLS (TLS client-certificate authentication) to Keycloak; OAuth clients
  authenticate with client credentials inside TLS. Keycloak's cluster
  listeners remain on `idp`.
- Manual renewal only, no expiry monitoring, no revocation; three private CAs
  with separate manual lifecycles.
- Secret rotation, rate limiting, observability, availability, and restore
  (#130, #131) are unchanged.

## Decision and capability levels

[ADR 0007](../adr/0007-security-lab-identity-provider-transport.md) records
the decision as Proposed. No capability level changes.

## Evidence provenance

Every run used uncommitted working-tree files. Hashes are `git
hash-object` blobs of the files as they were on disk when the run started.
None of the tested blobs existed in a commit at the time.

**2026-10-17 runs.** Each run directory has a `provenance.json` with the
blob of every tested file, recorded right after that run. The tree did not
change during either run.

- Full rerun: tool `b4b2dc4`.
- Targeted run: tool `2b445a3`, the blob submitted for review with this
  record.
- The infrastructure files are identical in both runs and to the table
  below.
- The CI script `scripts/test-security-lab-keycloak-tls.sh` gained the IP
  and control cases after the first run (now `905dd44`). The lab runs do not
  use it.

**First run (2026-10-13), files directly in the evidence directory.** As
recorded at the time, the final evidence ran with working-tree tool blob
`ec0283f2b80c4f924811e9a816a30c84c4bc4298`. The working-tree tool at the end
of that work, `b4b2dc455bd38ede6cef3def33f18ffd139f7591` (also the full
rerun's), differed only in removing probe containers with `docker rm -fv`
(see [cleanup](#cleanup-and-final-state)) and a comment saying why. The
intermediate blobs `ec0283f` and `9f11945` were never committed or stored,
so these differences rest on the first run's notes and cannot be rechecked.

| File | Working-tree blob (first run) |
| --- | --- |
| `tools/security-lab-local` (evidence runs) | `ec0283f2b80c4f924811e9a816a30c84c4bc4298` |
| `infra/security-lab/compose.yaml` | `bbbd6c5549ab3560b571634fb3d20167e3984097` |
| `infra/security-lab/ingress/nginx.conf` | `8d7a9688634ba357694dd4df7efb096445d2510c` |
| `infra/security-lab/ingress/keycloak-upstream.conf` | `b3db07565d2a2ba3a89f11ca95de8f06a496a8f9` |
| `infra/security-lab/ingress/Dockerfile` | `b7874b68e54fbbac2719d736f24a2e247eda3419` |
| `scripts/test-security-lab-keycloak-tls.sh` | `dccb114c8a119967ccbb3a4811e419b552f81278` |

Sequence, all in `zero-to-prod-security-lab-kctls`:

1. Created from `main` (`4f8b299`), started, and captured (development run).
2. Upgraded with this branch (`init`, `start`): the first exercise of the
   upgrade path. Development runs of `verify-keycloak-tls` followed.
3. Regression runs (`verify` through `verify-internal-tls` in the table)
   with tool blob `9f11945c139dc927fb0685149085a06863bffb16`. As recorded
   then, it differed from the final first-run tool only by the
   unchanged-reload control in `verify-keycloak-tls`'s baseline, which none
   of those commands runs.
4. Downgraded to `main`'s tool and Compose files (`init`, `start`), captured
   with the branch's working-tree tool of the time (`before-capture-idp-hop.json`;
   the capture command works on labs from `main`), refused by the
   branch's `check-secrets` (`keycloak.conf does not restrict Keycloak to
   HTTPS`), and upgraded again: the second exercise of the upgrade path.
5. `verify-keycloak-tls` with tool `ec0283f` (`verify-keycloak-tls.json`).
6. 2026-10-17: `init` (no change: CAs and certificates still valid),
   `start` with tool `b4b2dc4`, and the full rerun into `rerun/`.
7. The tool edit for the IP case, then `verify-keycloak-tls` with tool
   `2b445a3` into `rerun-ip/` on the same running lab.

Containers were recreated by each `start`; the volumes and the state
directory were kept throughout.

## Cleanup and final state

The `zero-to-prod-security-lab-kctls` project is kept for review with its
volumes (`_postgres_data`, `_rabbitmq_data`, `_identity_postgres_data`) and
its state directory; it was stopped after the runs. The untrusted test CA,
the captures, the cookie jars, and the control nginx's configuration existed
only in temporary directories during each run; the probe and control
containers were removed by the tool. The `main` extraction in `/tmp` was
deleted.

The final inventory found that removing a probe container started with
`--rm` from the RabbitMQ image (which declares a volume) by `docker rm -f`
left an anonymous volume each time. This run left 10 (created between 21:39
and 22:25). They were checked to be dangling, labelled only as anonymous, and
created during these runs, and then removed. The tool now removes probes with
`docker rm -fv`, which a check with the same image confirmed leaves no
volume. Nine older anonymous volumes (19:57–21:06, during the internal TLS
runs of #136, probably by the same mechanism) were left as found.

After the 2026-10-17 runs the project was stopped again, keeping its
containers, its three volumes (created 2026-10-13 21:35), its networks, and
its state directory. The volume list before and after the runs was
identical, so the `docker rm -fv` change left no anonymous volume. No
probe, control nginx, or `-ipcontrol` container or network remained. The
untrusted and unconstrained control CAs existed only in the runs' temporary
directories.

The original `zero-to-prod-security-lab` (four volumes), the `-authz` and
`-tls` projects (three volumes each), and the development volumes kept their
names and creation times. They were not started, recreated, or deleted, and
their contents were not inspected. Only the stopped Keycloak containers'
existing logs were read, to check for the cluster message in observation 6;
none contained it. The images tagged `zero-to-prod-security-lab-*:local`
were rebuilt, as every `start` does; the other projects' containers keep
the image they were created with.
