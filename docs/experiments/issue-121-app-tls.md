# Issue #121 — TLS from the ingress to the API and oauth2-proxy

Recorded 2026-10-17; browser check added 2026-10-18. This record covers the two application hops decided in
[ADR 0008](../adr/0008-security-lab-application-transport.md) (Proposed):
the API and oauth2-proxy serve only HTTPS with certificates from a separate
application CA, and the ingress verifies both. It builds on the merged
datastore (#136, ADR 0006), Keycloak (#137, ADR 0007), and datastore-CA
validation (#138) slices. It does **not** complete #121: oauth2-proxy →
Valkey and Keycloak → its database stay plaintext, and rate limiting and
secret rotation are open.

## Question

The ingress sends bearer tokens to the API and session cookies,
authorization codes, and returned access tokens to and from oauth2-proxy
over plaintext on the `app` network. Can both hops be encrypted and verified
without changing the public URLs, cookies, CSRF checks, session refresh,
authorization, machine tokens, or failure classification? What happens to
new and kept-alive connections, and to resumed TLS sessions, when either
certificate is untrusted, names the wrong host, or has expired, and how do
the services recover?

Environment classification: **LOCAL-FIRST**, in a separate security lab
project on one workstation.

## Environment

| Component | Version or mode |
| --- | --- |
| Host | Windows 11 Pro (build 26300), WSL 2 in NAT mode, Docker Desktop (Engine 29.6.2, Compose 5.3.1) |
| API | Go 1.27.1 build of this branch (`crypto/tls`), Alpine 3.22 runtime |
| oauth2-proxy | 7.15.5 (digest-pinned) |
| Ingress | nginx 1.30.5 (Alpine, digest-pinned) |
| Probes | OpenSSL 3.5.8 `s_client` and Perl (RabbitMQ 4.3.6 image); curl 8.22.0; WSL OpenSSL 3.5.5 for issuing |

## Setup

- **Separate lab.** All runs used the new project
  `zero-to-prod-security-lab-apptls` with its own state directory, CAs,
  realm, and volumes. Before it was created, no container, volume, network,
  or state directory of that name existed. The original, `-authz`, `-tls`,
  and `-kctls` labs, the development projects, and their networks and
  volumes were not started, changed, or removed (see
  [cleanup](#cleanup-and-final-state)).
- **Explicit subnets (environment adjustment).** Docker's default address
  pools were exhausted: 28 of the 31 automatic subnets belong to stopped
  retained projects. Without touching them or the daemon, the six `-apptls`
  networks were created with explicit subnets in `10.213.0.0/16` (unused by
  any WSL, Windows, Docker, or Docker Desktop route; no VPN present), carrying
  Compose's project and network labels and the same `internal` setting as
  `compose.yaml`. Compose adopted them (confirmed first with a throwaway
  project). The tool's own probe and control networks got subnets through the
  new opt-in `ZTP_SECURITY_LAB_TOOL_SUBNETS`. Details, routes, and the exact
  commands: [`environment.json`](../../evidence/issue-121/app-tls/environment.json).
  Network membership was the same as with automatic subnets.
- **Before and after in the same lab.** The project was created and started
  with **main's own tool and Compose file** (`3c1104c`, extracted with `git
  archive`; main's `check-secrets` passed). The "before" capture ran the
  branch's `capture-app-hops` from a copy **placed inside main's tree**, so
  every Compose operation used main's `compose.yaml`; the command only reads
  and captures. The same project was then upgraded with this branch's `init`
  and `start`. That path was exercised twice (see
  [provenance](#evidence-provenance)).
- **Method.** Scripted clients (curl with cookie jars), OpenSSL probe
  containers on `app`, a control nginx from the ingress image, raw-socket
  captures in the API's and oauth2-proxy's network namespaces, and container
  logs; on 2026-10-18, additionally a real browser engine driving the Web UI
  (see [browser check](#browser-check-2026-10-18)). No desktop browser on
  Windows was used.

## Design questions resolved before the runs

1. **Why a fourth CA despite ADR 0007's consolidation trigger.** ADR 0008
   records the trigger as met and deferred: consolidation re-anchors every
   verifier (API, worker, migrator, ingress, probes), reissues every
   certificate, restarts every client, and needs new evidence for name
   constraints on an intermediate; that is a separate migration. A separate
   application CA leaves ADR 0006's and ADR 0007's policies untouched and is
   additive. Consolidation must come before a fifth CA, automated issuance,
   or a CA leaving the lab host.
2. **The API's last good certificate.** After a failed reload (unreadable
   file, or a changed file that is not a valid key pair) the API keeps the
   pair it has, also past its expiry; the ingress then refuses an expired
   certificate (502). Observable: a log line per load with serial and expiry,
   a single report per rejected or unreadable file, `check-secrets` refusing
   the file, and served-versus-file serial comparisons. Recovery: write a
   valid file; served within a second, no restart. Exercised at runtime
   below.
3. **Keep-alive bounds.** Kept-alive API connections are not re-verified:
   a bad certificate affects traffic on them only when they close (30 s idle,
   5 min total). New connections fail at once. Measured below for the idle
   case; the 5-minute cap under continuous load was not exercised.
4. **The pre-authorization TLS front.** `verify-authorization`'s control
   runs the old API (plain HTTP only) behind a **test-only TLS front**. It
   tests that API's authorization behavior through the same verified hop,
   not TLS support in the old API; the evidence labels it so.
5. **Why the first main-built attempt mentioned the application CA.** Main's
   own `start` ran main's preflight (it passed) and then failed creating
   networks (address pools). The two lines expecting the application CA came
   from **this branch's** `check-secrets`, which the attempt ran deliberately
   afterwards to show the upgrade refusal; they were not part of main's
   start. The capture of that attempt also ran from the branch tree and found
   no running ingress. The before-capture was then redone as described under
   setup, with main's tool and Compose file throughout.

## What was built

- **Application CA** (`init`): ECDSA P-256, 90 days, `pathlen:0`, permitted
  `DNS:api` and `DNS:oauth2-proxy`, excluded their subdomains and all IP
  addresses; checked by the shared DER validator. Mounted only into the
  ingress.
- **Certificates**: 14 days, SAN equal to the service name,
  `app-tls/<service>/tls.pem`, reissued by `init` near expiry or when not
  verifying; `issue-app-certificate <service>` renews.
- **API** (application change, `apps/work-items/api/tls.go`): with
  `TLS_CERT_FILE`, a TLS-only listener (TLS 1.2+, no session tickets) whose
  certificate file is reread on change; last good pair kept on a bad file;
  loads logged with serial and expiry. Without it, plain HTTP as before.
  Unit tests cover verification, name and CA refusal, the plaintext
  refusal, reload, last good pair, logging once, and no resumption; two
  mutations (tickets on, no TLS listener) fail them.
- **oauth2-proxy**: `http_address = "-"`, HTTPS on 4443, TLS 1.2+.
- **Ingress**: `api-upstream.conf` and `oauth2-proxy-upstream.conf`
  (verification, name, application CA, no session reuse) in every relevant
  location; the API upstream with `resolve`, `keepalive_timeout 30s`,
  `keepalive_time 5m`; diagnostic routes `/api-upstream`, `/api-version`,
  `/oauth2-proxy-upstream`.
- **Lab tool**: preflight for the CA policy, certificates, and an HTTPS-only
  oauth2-proxy configuration; `issue-app-certificate`, `check-app-upstream`,
  `capture-app-hops`, `verify-app-tls`; TLS for the session-token probe;
  the API's version and health through the diagnostic routes; the test-only
  TLS front for the authorization baseline; `ZTP_SECURITY_LAB_TOOL_SUBNETS`;
  a port filter for captures; connection counts by address and port.
- **CI**: `scripts/test-security-lab-app-tls.sh` (57 checks, no Docker).

## Results

Evidence: [`evidence/issue-121/app-tls/`](../../evidence/issue-121/app-tls).
All runs on 2026-10-17 (UTC). Pass and fail are expectations; observations
are recorded values without an expectation. The tool's console total adds
both.

| Check | Evidence | Pass | Fail | Skip | Observations |
| --- | --- | --- | --- | --- | --- |
| `capture-app-hops`, `main`'s configuration (observation-only) | `before-capture-app-hops.json` | 0 | 0 | 0 | 9 |
| `verify-app-tls` (current) | `rerun/verify-app-tls.json` | 200 | 0 | 0 | 57 |
| `verify-app-tls` (full run; same results, earlier tool) | `verify-app-tls.json` | 200 | 0 | 0 | 57 |
| `capture-app-hops`, after the change (observation-only) | `after-capture-app-hops.json` | 0 | 0 | 0 | 9 |
| `check-app-upstream`, `issue-app-certificate api`, `issue-app-certificate oauth2-proxy` | console only | | | | both routes 200; each renewal confirmed by five verified connections |
| `verify` (current; Windows LAN address redacted) | `rerun/verify.json` | 87 | 0 | 0 | 4 |
| `verify-failures` | `missing-secret-failures.json` | 24 | 0 | 0 | 0 |
| `verify-realm` | `verify-realm.json` | 11 | 0 | 0 | 0 |
| `verify-authentication` (with the slow expiry checks) | `verify-authentication.json` | 77 | 0 | 0 | 10 |
| `verify-log-hygiene` | `log-hygiene.json` | 17 | 0 | 0 | 21 |
| `verify-authorization` (with role removal; baseline behind the test-only TLS front) | `verify-authorization.json` | 79 | 0 | 0 | 15 |
| `verify-internal-tls` | `verify-internal-tls.json` | 94 | 0 | 0 | 35 |
| `verify-keycloak-tls` (current) | `rerun/verify-keycloak-tls.json` | 167 | 0 | 0 | 60 |
| `verify-keycloak-tls` (full run, superseded) | `verify-keycloak-tls.json` | 160 | **7** | 0 | 60 |
| `verify-renewal` (ingress) | `ingress-certificate-renewal.json` | 1 | 0 | 0 | 0 |
| `go test ./...` in `apps/work-items` (local) | console | all packages ok | | | |
| `scripts/test-security-lab-app-tls.sh` / `-keycloak-tls` / `-internal-tls` (local, no Docker) | console | 57 / 65 / 66 | 0 | 0 | |

Compared with #137/#138: `verify-failures` gained 6 cases (24), `verify`
3 (87), `verify-authorization` 1 observation (the label of the baseline
control); the authentication, log-hygiene, realm, internal-TLS, and Keycloak
counts are unchanged.

### Before and after: what crosses the hops

`capture-app-hops` joined the API's and oauth2-proxy's network namespaces
with `CAP_NET_RAW` and kept, for 25 s, the TCP payload between the ingress
and each service **on the service's port** (8080/8443 for the API, 4180/4443
for oauth2-proxy), while it drove a sign-in callback, a session
`GET /api/items`, and a machine `GET /api/items`. It counted fixed strings
and deleted the captures.

| Occurrences | API hop, before (`main`) | API hop, after | oauth2-proxy hop, before | oauth2-proxy hop, after |
| --- | --- | --- | --- | --- |
| Bearer header (`Bearer ey`) | 2 | 0 | 0 | 0 |
| Machine access token | 1 | 0 | 0 | 0 |
| Authorization code | 0 | 0 | 1 | 0 |
| State (percent-encoded) | 0 | 0 | 3 | 0 |
| Session cookie | 0 | 0 | 2 | 0 |
| `X-Auth-Request-Access-Token` header | 0 | 0 | 1 | 0 |
| Plaintext HTTP messages | 4 | 0 | 6 | 0 |
| TLS application-data records (positive control) | 0 | 4 | 0 | 32 |
| Listener | `:::8080` | `:::8443` | `:::4180` | `:::4443` |

Before the change the ingress's upstreams were on 8080 and 4180; after, on
8443 and 4443 only.

The first before-capture filtered by address only. It had the same
credential and HTTP counts, but also 9 and 6 TLS records: the API's and
oauth2-proxy's own TLS calls to the issuer through the ingress, between the
same two containers. That made the positive control ambiguous, so the
capture now keeps only the service's port, and the before-capture was
repeated (the first is kept in `attempts/`).

### Baseline after the change

| Hypothesis | Observed |
| --- | --- |
| New connections verify against the application CA for each name | Code 0 and the file's serial, for `api` and `oauth2-proxy` |
| Neither the lab, internal, nor identity-provider CA verifies either service | Code 21 with each |
| No plaintext listener | `api:8080` and `oauth2-proxy:4180` refused; listeners `:::8443` and `:::4443` only; plain HTTP to either TLS port answered with Go's `Client sent an HTTP request to an HTTPS server.` and no application content (the 400 status is asserted by the API's unit test) |
| Public flows unchanged | 401 unauthenticated; redirect to the public authorization endpoint; callback 302; session `GET /api/items` 200; session access token (read over verified TLS) with the public issuer; create and process `succeeded`; CSRF 403 `csrf_rejected`; ambiguous credentials 400; machine 200 and `POST` 403 `insufficient_permissions`; `lab-viewer` `POST` 403; session refresh returned a new cookie through `auth_request`; sign-out 302 and the Keycloak session ended |
| Upstream transport | Ingress access log: upstreams on 8443 and 4443 only; one kept-alive API connection; none to oauth2-proxy |
| Session resumption, certificates unchanged | API: no session ticket issued, so nothing to resume; oauth2-proxy: `Reused` (Go defaults). The ingress reuses no sessions either way |

### Failure cases

For each kind the tool installed the certificate, made the service serve it
(the API by itself; oauth2-proxy by restart), waited for five consecutive new
connections with the bad serial, then observed:

| Service and certificate | OpenSSL code | Public effect | Reason (diagnostic route) | Recovery |
| --- | --- | --- | --- | --- |
| API: untrusted CA | 21 | API routes 502 (session and machine) on new connections; sign-in 302 | `(21:unable to verify the first certificate)` | Renewal without restart; old session 200, machine 200, new sign-in 200 |
| API: lab CA | 21 | same | same | same |
| API: name `oauth2-proxy` | 62 | same | `upstream SSL certificate does not match "api"` | same |
| API: expired | 10 | same | `(10:certificate has expired)` | same |
| oauth2-proxy: untrusted CA | 21 | Session routes and sign-in 503 `authentication_unavailable`; machine 200 | `(21:unable to verify the first certificate)` | Renewal and restart; the session from before (in Valkey) 200; new sign-in 200 |
| oauth2-proxy: lab CA | 21 | same | same | same |
| oauth2-proxy: name `api` | 62 | same | `upstream SSL certificate does not match "oauth2-proxy"` | same |
| oauth2-proxy: expired | 10 | same | `(10:certificate has expired)` | same |

In every case the public servers logged no TLS reason (suppressed below
`crit`), the control nginx with the production directives answered 502 and
with verification off 200 (negative control), and the access log showed the
failed upstream statuses.

**Kept-alive connections (API).** Right after each bad API certificate took
effect, a session request still answered **200** over the connection kept
alive from before (1 established). After the 30-second idle timeout the
connection was gone (0) and new requests answered 502. So a bad API
certificate reaches traffic on a pooled connection only when it closes.

**Session resumption.** The API never issued a ticket, so a probe had no
session to offer before or after any change. For oauth2-proxy a saved
session was not resumed after each restart with a new certificate: `New`,
with the bad certificate's code. A resumed session was not observed across
a change, so revalidation of a resumed session was not exercised.

### Last good certificate (API)

A non-key-pair file was installed by rename. New connections kept verifying
with the previous serial (code 0), a session request answered 200,
`check-secrets` refused the file, and the API logged the rejection exactly
once, naming the serial still served. Restoring the file passed
`check-secrets` and kept the serial.

### Renewal

- API: new serial served to new connections within the wait, without a
  restart (container start time unchanged); a kept-alive connection existed
  at renewal; session and machine requests 200.
- oauth2-proxy: the served serial was unchanged 5 s after the file was
  replaced (read at start only); after the restart the new serial was served
  and the session from before still worked, including create and process.
- `issue-app-certificate api` and `oauth2-proxy` (separate runs): five
  consecutive verified connections with the new serial each.

### Logs

The run searched the ingress, oauth2-proxy, API, and control-nginx logs
written during it for 48 strings (long-lived secrets, codes, state, session
cookies including refreshed ones, session and machine access tokens, and
their percent-encoded forms), including those sent while certificates were
refused: 0 in each; the Keycloak log (observation) also 0. A synthetic log
matched all 48 (positive control); the ingress log held the run's diagnostic
TLS reasons and the API log its certificate reloads (positive controls).
`verify-log-hygiene` (successful sign-in, refresh, failed callbacks) found
none of its 29 values. A separate scan of all evidence found none of the
lab's 21 runtime secret values and no private key.

## Browser check (2026-10-18)

The built-in browser of the Claude desktop app again refused
`https://work-items.localhost:9443/`: Windows does not trust this separate
lab's CA, and the CA was not added to the Windows trust store, nor was any
certificate warning bypassed. Instead, headless **Chromium 131.0.6778.33**
(Playwright 1.49.1, `mcr.microsoft.com/playwright:v1.49.1-noble` pinned by
digest) drove the real Web UI in a container:

- **Trust.** Chromium's NSS database held exactly one certificate, this
  lab's CA; no certificate error was ignored. Negative control: the same
  Chromium without that database failed with `net::ERR_CERT_AUTHORITY_INVALID`.
- **Network.** Chromium resolves `*.localhost` to its own loopback and
  ignores `/etc/hosts`, as browsers on Windows do. The container therefore
  shared the ingress container's network namespace, where `127.0.0.1:9443`
  is the ingress's own listener, mirroring Windows (`127.0.0.1:9443` is
  published to the ingress). The URLs were the public ones.
- **Runner and script:**
  [`browser/run-browser-check.sh`](../../evidence/issue-121/app-tls/browser/run-browser-check.sh)
  and [`browser-check.js`](../../evidence/issue-121/app-tls/browser/browser-check.js);
  only the two test users' password files are mounted, and no value is
  recorded.

Result ([`browser-results.json`](../../evidence/issue-121/app-tls/browser/browser-results.json),
committed runner): **17 passed checks, 7 observations, 0 failed, 0 skipped.**

| Step | Observed |
| --- | --- |
| Public TLS | UI 200 over TLS 1.3; certificate `work-items.localhost` from `Zero-to-Prod security lab CA (zero-to-prod-security-lab-apptls)`, verified by Chromium |
| Signed out | The UI showed "Sign in required" |
| Sign-in | The UI's "Sign in" link led to Keycloak's form on `auth.work-items.localhost:9443`; after submitting `lab-user`'s password the browser returned to the UI with a session (`lab-user@work-items.localhost`, "Sign out" shown) |
| Session cookie | `__Host-wi-session`: `Secure`, `HttpOnly`, `SameSite=Lax`, path `/`, host-only `work-items.localhost` (no value recorded) |
| Create and process through the UI | `POST /api/items` 201; the item listed; its "Process" button: `POST /api/items/{id}/process` 202; status `done` |
| Sign-out through the UI | `POST /oauth2/sign_out` 302; "Sign in required" again; the session cookie gone; "Sign in" led to Keycloak's password form again |
| Authorization through the UI | `lab-viewer` signed in and listed items (200); "Create": `POST /api/items` 403 and the dialog's "Not allowed" message |
| Gateway saw the browser | 23 oauth2-proxy log lines with the `HeadlessChrome` user agent during the run |
| Console | Only the expected 401s before sign-in (the UI's `/oauth2/userinfo` and `/api/items` probes) and the viewer's 403 |

Screenshots: [signed in](../../evidence/issue-121/app-tls/browser/1-signed-in.png),
[created and processed](../../evidence/issue-121/app-tls/browser/2-created-and-processed.png),
[signed out](../../evidence/issue-121/app-tls/browser/3-signed-out.png),
[viewer not allowed](../../evidence/issue-121/app-tls/browser/4-viewer-not-allowed.png).

This verifies browser sign-in, the UI's same-origin requests (including the
CSRF header the UI adds), session handling, sign-out, and authorization
through the encrypted hops with a real browser engine. It is headless
Chromium in a container, not a person's desktop browser on Windows; a manual
check there would require trusting this lab's CA on Windows. Three attempts
aborted before the passing runs (permissions of the password files,
`*.localhost` resolution, a script error); they are listed in
[`browser/provenance.json`](../../evidence/issue-121/app-tls/browser/provenance.json).

## Observations

1. **A second TLS stream between the same containers.** The API and
   oauth2-proxy reach the issuer through the ingress, so the ingress and each
   service exchange TLS in both directions. A capture by address alone mixes
   them; port filtering separates them.
2. **Connection counts by port alone collided.** Since the API also listens
   on 8443, `verify-keycloak-tls`'s check of "no kept-alive connection to
   Keycloak" counted the ingress's API connection (7 failures in the full
   run). The helper now matches address and port; the rerun passed.
3. **oauth2-proxy does not reload certificates.** A replaced file was not
   served until a restart (also measured before implementation).
4. **The plaintext listener can be disabled in oauth2-proxy.** With
   `http_address = "-"` only the HTTPS port listened (measured inside its
   network namespace before and after).

## Development and failed attempts

- First start from `main` failed at network creation (address pools); see
  setup. The branch's `check-secrets` was run against the main-created state
  on purpose.
- `attempts/dev1-verify-app-tls.json` (tool `604bd3d`): 198 passed,
  58 observations, 2 failed. Both failures expected a saved TLS session from
  the API; with tickets disabled none is issued. The check now expects none.
- A full run was stopped early (34 results, no evidence) to fix the capture
  filter; its control container was removed.
- `attempts/before-capture-app-hops-unfiltered.json`: the address-only
  capture described above.
- Full run `verify-keycloak-tls.json`: 7 failures from the connection-count
  collision; superseded by `rerun/verify-keycloak-tls.json`.

## What this does not prove

- A desktop browser on Windows: the browser check used headless Chromium in
  a container that trusts only the lab CA.
- The 5-minute `keepalive_time` cap under continuous load, and latency of
  the extra handshakes (not measured).
- Behavior at the real expiry of a running certificate (simulated with
  pre-expired ones), CA rotation, revocation, automated renewal.
- Access control on `app`: any member can open a TLS connection to either
  service (no mTLS).
- Revalidation of a resumed session across a certificate change (not
  observed).
- Single runs.

## Remaining gaps

- oauth2-proxy → Valkey and Keycloak → its database stay plaintext.
- Four parallel private CAs with manual lifecycles; consolidation deferred
  (ADR 0008).
- oauth2-proxy renewal needs a restart.
- No mTLS (not adopted), no expiry monitoring, no revocation.
- Secret rotation, rate limiting, observability, availability (#130, #131).

## Decision and capability levels

[ADR 0008](../adr/0008-security-lab-application-transport.md) records the
decision as Proposed. No capability level changes.

## Evidence provenance

All runs used uncommitted working-tree files; hashes are `git hash-object`
blobs recorded at the start of each run (the tree did not change during a
run).

| Run | When (UTC) | Tool blob | Files |
| --- | --- | --- | --- |
| Before-capture (lab from `main` `3c1104c`: tool `10fa664`, `compose.yaml` `bbbd6c5`, `nginx.conf` `8d7a968`; capture command copied into main's tree) | 15:38 | `6d42379` (capture command only) | `before-capture-app-hops.json` |
| First before-capture (address filter only; same lab and method) | 15:23 | `604bd3d` (capture command only) | `attempts/before-capture-app-hops-unfiltered.json` |
| Development run of `verify-app-tls` | 15:25–15:33 | `604bd3d` | `attempts/dev1-verify-app-tls.json` |
| Full run | 15:39–16:19 | `6d42379` | [`provenance.json`](../../evidence/issue-121/app-tls/provenance.json) and every file directly in the directory |
| Targeted rerun (`verify-app-tls`, `verify-keycloak-tls`) | 16:21–16:38 | `d013f82` | [`rerun/provenance.json`](../../evidence/issue-121/app-tls/rerun/provenance.json), `rerun/verify-app-tls.json`, `rerun/verify-keycloak-tls.json` |
| `verify` rerun | 16:40 | `18c212e` (final) | [`rerun/provenance-verify.json`](../../evidence/issue-121/app-tls/rerun/provenance-verify.json), `rerun/verify.json` |
| Browser check (2026-10-18; lab restarted with the final tool) | 12:07 | `18c212e` (final) | [`browser/provenance.json`](../../evidence/issue-121/app-tls/browser/provenance.json) and the files in `browser/` |

The infrastructure and API files were the same in every branch run:
`compose.yaml` `f52fd82`, `nginx.conf` `e28f011`, `api-upstream.conf`
`fcc3002`, `oauth2-proxy-upstream.conf` `0be5ded`, `keycloak-upstream.conf`
`b3db075`, ingress `Dockerfile` `92e5dda`, `main.go` `bd5128b`, `tls.go`
`f598ecf`. The API image was built from them by `start` before the runs.

**Changes after the full run, and what they affect** (checked by
reverse-applying them to the final tool, which reproduces `6d42379` and
`d013f82` byte for byte):

- `established_to_port` now matches the remote address as well as the port,
  and its callers pass it. Called only from `verify-app-tls` and
  `verify-keycloak-tls`, which were rerun.
- `verify`'s WSL-side API probe uses 8443 instead of 8080 (the old port
  passed trivially). `verify` was rerun.
- So the full run's `verify-failures`, `verify-realm`,
  `verify-authentication`, `verify-log-hygiene`, `verify-authorization`,
  `verify-internal-tls`, `verify-renewal`, `capture-app-hops`,
  `check-app-upstream`, and both `issue-app-certificate` runs apply to the
  final tool; `rerun/` supersedes the full run's `verify-app-tls.json`,
  `verify-keycloak-tls.json` (7 failures), and `verify.json`.

The before-capture used the same tool blob as the full run (`6d42379`), so
before and after counts come from identical capture code.

**Upgrade path.** Exercised twice on this lab: main-created state was refused
by this branch's `check-secrets` (application CA missing; plaintext
`oauth2-proxy.cfg`), then `init` and `start` upgraded it; after a downgrade
with main's `init` and `start` (for the repeated before-capture) the branch's
`check-secrets` refused the plaintext configuration again and `init`/`start`
upgraded it. Volumes and state were kept throughout.

## Cleanup and final state

The `zero-to-prod-security-lab-apptls` project is stopped and kept for
review: 9 containers, 3 volumes (`_postgres_data`, `_rabbitmq_data`,
`_identity_postgres_data`, created 15:22:42), the six explicit-subnet
networks, and its state directory. No probe, control, baseline-front, or
tool network remains. The three networks created by the failed first start
were removed (confirmed by project label, creation time, and no containers);
the throwaway adoption-test project removed itself. The `main` extraction in
`/tmp` and the untrusted and control CAs (temporary directories) were
deleted.

Compared with the inventory taken before this slice, the only new volumes
are the three above; the 18 named volumes of the retained projects kept their
names and creation times, and their 29 networks were not changed. No retained
project was started, and no Docker daemon setting was changed.

On 2026-10-18 the same project was started again (same networks, volumes,
and state) for the browser check and stopped afterwards; the volume list and
the retained volumes' creation times were unchanged. The browser containers
ran with `--rm`; the temporary directory with the npm package and the NSS
database was deleted. The pulled image
`mcr.microsoft.com/playwright:v1.49.1-noble` remains in the local image
store.
