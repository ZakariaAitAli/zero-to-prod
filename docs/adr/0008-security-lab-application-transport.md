# ADR 0008 — Security lab application hops: verified TLS from the ingress to the API and oauth2-proxy

## Status

Proposed (provisional). Replaces the "Ingress → API, ingress → oauth2-proxy:
plaintext (provisional)" row of
[ADR 0006](0006-security-lab-internal-transport.md); the rest of ADR 0006 and
all of [ADR 0007](0007-security-lab-identity-provider-transport.md) are
unchanged. Builds on ADR 0004 and ADR 0005, which are also Proposed.

## Context

The ingress forwards two kinds of requests over the internal `app` network,
which also holds the API and oauth2-proxy:

| Caller → server | Path | What crosses |
| --- | --- | --- |
| Ingress → API | Session API routes, after `auth_request` | The session's access token (`Authorization: Bearer`), Work Items data |
| Ingress → API | Bearer API routes (`/_bearer/`) | Machine clients' access tokens, Work Items data |
| Ingress → oauth2-proxy | `/oauth2/start`, `/oauth2/callback`, `/oauth2/userinfo` | Authorization codes and state (callback), new session cookies |
| Ingress → oauth2-proxy | `/oauth2/sign_out` | Session cookie; triggers the backend logout |
| Ingress → oauth2-proxy | `auth_request` (`/oauth2/auth`) on every session API request | Session cookie in; the access token (`X-Auth-Request-Access-Token`) and refreshed session cookies out |
| Ingress → oauth2-proxy | `/ready` after a 401 with a session cookie | Nothing sensitive |
| Lab tool → API, oauth2-proxy | Reachability probes, the API's version and health from the ingress container, the session-token probe (`session_access_token`, sends a session cookie), the authorization baseline control | As the paths above |

No other component calls the API or oauth2-proxy. The worker uses only the
datastores; the API and oauth2-proxy reach the issuer through the ingress
(ADR 0007). Valkey and Keycloak's database are not on this network.

A passive capture of both hops in a lab built from `main` (`3c1104c`,
`capture-app-hops`, counts only) found the bearer header and the machine
token on the API hop, and the authorization code, session cookie, and
access-token header on the oauth2-proxy hop, in plaintext HTTP.

ADR 0006 left these hops plaintext because every `app` member drops all
capabilities and runs with `no-new-privileges`. That still holds for the
three services, but the lab tool's probes join `app`, and the hops carry
the credentials an attacker most wants after passwords: replayable bearer
tokens and session cookies. With the data network and the Keycloak hop
already encrypted, these are the last ingress hops in clear.

## Decision drivers

| ID | Requirement |
| --- | --- |
| A1 | Confidentiality and integrity of both hops against another member of `app` (threats T1–T3) |
| A2 | The ingress verifies the chain and the exact name it dialed on each hop; no plaintext fallback; neither server keeps a plaintext listener |
| A3 | Unchanged public contract: URLs, cookies and their attributes, CSRF checks, session refresh through `auth_request`, authorization (401 versus 403), machine tokens, and failure classification (401, 403, 400 ambiguous, 503 `authentication_unavailable`) |
| A4 | No browser-trusted CA can vouch for either service; existing CA restrictions (ADR 0006, ADR 0007) stay unchanged unless explicitly decided otherwise |
| A5 | Failures closed at the ingress and diagnosable from logs without credentials |
| A6 | Issuance, mounting, renewal, and reload or restart reproducible by the lab tool |
| A7 | Proportionate: application change only where the server cannot express TLS |

## Options

### Who signs the API's and oauth2-proxy's certificates

| Option | Assessment |
| --- | --- |
| **A. The lab CA** (browser-trusted) | Fails A4: name-constrained to `work-items.localhost`, trusted by people's browsers, and its key already signs the public certificate |
| **B. The internal CA**, extended to `api` and `oauth2-proxy` | Changes ADR 0006's policy; its verifiers (pgx, amqp091-go in the API and worker) would trust a key that also vouches for the ingress-facing servers; replacing it restarts the API and worker |
| **C. The identity-provider CA**, widened into an "ingress upstream" CA for `keycloak`, `api`, `oauth2-proxy` | The verifier is the same (the ingress), so per-hop name checks still hold. But it changes ADR 0007's exact-`keycloak` policy, every existing lab must replace that CA and reissue Keycloak's certificate, and its rotation would couple Keycloak (reloads by itself) with oauth2-proxy (needs a restart) |
| **D. A separate application CA**, exactly `api` and `oauth2-proxy`, trusted only by the ingress | Meets A4 without changing any existing policy; additive upgrade (a new CA and two certificates). Cost: a fourth private CA |
| **E. One root with name-constrained intermediates per trust domain** (the consolidation ADR 0007 names when a fourth CA appears) | The better long-term structure, but it re-anchors every verifier: the API, worker, and migrator (datastores), the ingress (Keycloak and these hops), and the tool's probes; every certificate is reissued and every client restarted; chain depth 2 needs `proxy_ssl_verify_depth 2` and fresh evidence that each verifier enforces constraints on an intermediate |
| **F. Self-signed certificates pinned in the ingress** | Every renewal changes the trust anchor in step with the server (ADR 0007's reasoning) |

### How the hops are protected

| Option | Assessment |
| --- | --- |
| **G. Server-authenticated TLS** in each server, the ingress verifies | Meets A1–A2. The API needs a small change (it served only plain HTTP); oauth2-proxy has native TLS |
| **H. Mutual TLS** | The API authenticates every request with a token, and oauth2-proxy's endpoints need a session cookie; mTLS would add caller restriction on `app`, not transport protection. The ingress's client key would sit next to its other keys. Not adopted |
| **I. A TLS-terminating sidecar** in front of each server (mesh pattern) | No API code change, but an extra container per service and a plaintext loopback hop; the API would still need a change to bind to loopback only, or it stays reachable in plaintext on `app` |
| **J. Plaintext with tighter membership** | The tool's probes must join `app`; leaves T1–T3 |

## Decision

**D + G**: the API and oauth2-proxy serve only HTTPS with certificates from a
separate application CA; the ingress verifies each.

### Why a fourth CA now, although ADR 0007 names consolidation

ADR 0007's reconsideration condition ("a fourth private CA appears:
consolidate into one root with constrained intermediates rather than
parallel roots") is met by this decision. It is recorded here as met and
deliberately deferred, for these reasons:

- **Scope.** Consolidation (option E) changes the trust anchors of all three
  existing trust domains and their verifiers, reissues every certificate,
  and restarts every client. Doing it in this slice would couple a migration
  of the datastore and Keycloak hops to adding two new hops, and the
  evidence of ADR 0006 and ADR 0007 would have to be redone.
- **No policy change now.** Option D leaves ADR 0006's and ADR 0007's
  name policies and their tested behavior intact, and its upgrade is
  additive.
- **Same verifier-side guarantee.** Each CA is trusted only by the clients of
  its hops and permits exactly their names, which is what an intermediate per
  trust domain would also give. What parallel roots lack is one place to
  rotate and one anchor to distribute; with four manually managed CAs that
  cost is now visible.
- **Limits of the separation.** All CA keys are files in the same state
  directory (mode 600). Separation limits what a misissued or misused
  certificate can impersonate; it does not protect one key if another is
  stolen from the same host (threat T8).

Consolidation is the next CA change, and it must happen before a fifth CA,
before automated issuance, or before any CA moves off the lab host,
whichever comes first.

### Details

- **Application CA.** ECDSA P-256, 90 days, `pathlen:0`; permitted
  `DNS:api` and `DNS:oauth2-proxy`; excluded `DNS:.api`, `DNS:.oauth2-proxy`,
  and all IPv4 and IPv6. Checked by the shared DER validator
  (`ca_name_policy_check`, as for the other CAs since #137/#138). Mounted
  only into the ingress.
- **Certificates.** 14 days, SAN equal to the Docker service name,
  `serverAuth`, key and certificate in one `app-tls/<service>/tls.pem` in a
  mode-711 directory, replaced by rename, mounted read-only into its own
  service.
- **API (application change).** With `TLS_CERT_FILE` set, the listener
  accepts only TLS (`tls.NewListener`); there is no plaintext listener, and
  a plaintext request is answered with Go's 400 before any handler. TLS 1.2
  minimum; session tickets disabled, so every new connection is a full
  handshake whose client verifies the certificate served at that moment.
  Without `TLS_CERT_FILE` the API serves plain HTTP as before (local
  development). Port 8443 in the lab; the health check uses HTTPS over the
  container loopback without verification (liveness only).
- **API last good certificate.** The file is read at startup (a missing or
  invalid file stops the process) and reread at most once per second on
  handshakes; a changed file with a valid key pair is served from then on.
  An unreadable file, or a changed file that is not a valid key pair, is
  ignored: the pair already loaded stays in use, including after it
  expires. The server does not judge validity; the ingress does, so an
  expired certificate is refused there (502, reason in the diagnostic log).
  Observable: the API logs every load with serial and expiry, and reports
  a rejected or unreadable file once (not on every check); `check-secrets`
  refuses the file; `issue-app-certificate` and `verify-app-tls` compare the
  served serial with the file's. Recovery: write a valid file; the next
  handshake at least a second later serves it, without a restart.
- **oauth2-proxy.** `http_address = "-"` (measured: only the HTTPS port
  listens), `https_address` on 4443, `tls_min_version = "TLS1.2"`. It reads
  the key pair at start (measured: a replaced file is not picked up), so a
  renewal restarts it; sessions live in Valkey and survive the restart.
- **Ingress.** Per hop an include (`api-upstream.conf`,
  `oauth2-proxy-upstream.conf`): `proxy_ssl_verify on`, depth 1, the
  application CA as the only trust anchor, `proxy_ssl_name` equal to the
  service name (also SNI), TLS 1.2/1.3, no session reuse. Every location
  that proxies to either service includes its file (tested in CI).
- **API keep-alive bounds.** The API upstream keeps up to 8 idle connections.
  A kept-alive connection is not re-verified, so a certificate change, good
  or bad, reaches the traffic on it only when it closes:
  `keepalive_timeout 30s` closes idle connections, and `keepalive_time 5m`
  caps any connection's lifetime even under continuous load (the API's own
  idle timeout is 60 s). Detection of a bad certificate at the ingress is
  therefore immediate for new connections and at most 30 s (idle) or 5 min
  (busy) for pooled ones. A renewal does not need them to close, because
  the old certificate is still valid. The upstream resolves `api` at run
  time (`resolve`), so a recreated API container is found without reloading
  nginx. oauth2-proxy is reached through a variable with no pool: one full
  handshake per `auth_request`, so nothing masks a change there.
- **Diagnostics.** The public servers suppress error-log entries below
  `crit` (they carry request lines). The loopback diagnostic server gains
  `/api-upstream`, `/api-version`, and `/oauth2-proxy-upstream`: new
  connections with the production directives and no credentials, logged at
  `error` level, so nginx's TLS reason is visible
  (`check-app-upstream`).
- **Failure classification.** Unchanged: an API that cannot be reached or
  verified answers 502, as before for an unreachable API; a gateway that
  cannot be reached or verified answers 503 `authentication_unavailable` on
  session routes and sign-in, while the bearer path, which does not use the
  gateway, keeps working.
- **Preflight.** `check-secrets` refuses a missing, expired, wrong-name, or
  mismatched certificate, an application CA with any other policy, and an
  oauth2-proxy configuration with a plaintext listener.
- **Lab tool.** The session-token probe uses HTTPS with the application CA;
  the API's version and health are read through the diagnostic routes.
- **Authorization baseline control.** `verify-authorization` compares the
  current API with one built from the pinned pre-authorization commit, which
  serves only plain HTTP. For that control only, the old API runs as a
  separate container and a **test-only TLS front** (nginx from the ingress
  image) takes the name `api`, presents the API's certificate, and forwards
  to it. The control therefore tests the old API's **authorization
  behavior** behind the same verified hop; it does not test, and does not
  claim, TLS support in that old API. The front-to-old-API hop is plaintext
  on `app` while the control runs.

## Consequences

- Every ingress upstream is now verified TLS. The remaining plaintext hops
  are oauth2-proxy → Valkey and Keycloak → its database (ADR 0006).
- A certificate problem on the API is an availability problem for API
  routes (502); on oauth2-proxy, for session routes and sign-in (503); the
  bearer path is independent of oauth2-proxy.
- One more private CA, with its own key, 90-day lifetime, and manual
  lifecycle. The API renews without a restart; oauth2-proxy renews with one.
- A full TLS handshake per session API request to oauth2-proxy
  (`auth_request`), and per new API connection; latency was not measured.
- Upgrade: `check-secrets` refuses labs created before this decision until
  `init` (application CA, certificates, oauth2-proxy configuration) and
  `start`.

## Reconsideration conditions

- A fifth CA, automated issuance, or a CA moving off the lab host:
  consolidate into one root with constrained intermediates (option E) first.
- Another component must call the API or oauth2-proxy directly: give it the
  application CA, or consolidate.
- Restricting which callers may reach the API or oauth2-proxy on `app`
  becomes a requirement: mTLS (option H) or separate networks.
- Measured latency of the per-request handshake to oauth2-proxy matters:
  an upstream pool for oauth2-proxy with bounded lifetime.
- oauth2-proxy gains certificate reloading: renew without a restart.

## Capability levels

No change.

## Evidence

From the [application TLS experiment](../experiments/issue-121-app-tls.md),
in a separate lab project (`zero-to-prod-security-lab-apptls`, explicit
subnets because Docker's address pools were exhausted), scripted clients
only:

- `capture-app-hops` with `main`'s configuration found the bearer header,
  the machine token, the authorization code, state, the session cookie, and
  the access-token header in 10 plaintext HTTP messages; after the change,
  none of them and no plaintext HTTP, with TLS records on both hops'
  ports (positive control).
- `verify-app-tls`: 200 passed checks, 57 observations, 0 failed,
  0 skipped. It covers trust separation, closed plaintext ports, the public
  flows and refusals, session refresh and sign-out, untrusted, lab-CA,
  wrong-name, and expired certificates on each service, kept-alive versus
  new connections, session resumption, the last-good-certificate behavior,
  recovery, renewal, and a log search.
- Regression runs of `verify`, `verify-failures` (6 new cases),
  `verify-realm`, `verify-authentication`, `verify-log-hygiene`,
  `verify-authorization` (baseline behind the test-only TLS front),
  `verify-internal-tls`, `verify-keycloak-tls`, and the ingress renewal:
  no failures after a test-helper fix (connection counts by address, since
  Keycloak and the API both use 8443); the failed run is kept.

- Browser check (2026-10-18): headless Chromium 131 trusting only the lab
  CA signed in through the Web UI and Keycloak, created and processed a Work
  Item, signed out, and saw `lab-viewer`'s create refused (17 passed checks,
  7 observations, 0 failed); without the lab CA it refused the page
  (`ERR_CERT_AUTHORITY_INVALID`). No desktop browser on Windows was used.
