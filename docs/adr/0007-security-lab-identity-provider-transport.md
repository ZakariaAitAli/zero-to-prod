# ADR 0007 — Security lab identity-provider hop: verified TLS from the ingress to Keycloak

## Status

Proposed (provisional). Replaces the "Ingress → Keycloak: plaintext
(provisional)" row of [ADR 0006](0006-security-lab-internal-transport.md);
the rest of ADR 0006 is unchanged. Builds on ADR 0004 and ADR 0005, which are
also Proposed and unchanged.

## Context

The ingress fronts Keycloak for the public name
`auth.work-items.localhost:9443`. Browser TLS ends at the ingress; until this
decision the ingress forwarded to `http://keycloak:8080` on the internal
`idp` network. That hop carries the most sensitive traffic in the lab:

| What crosses | Sender |
| --- | --- |
| Person passwords (login form) | Browser, through the ingress |
| Authorization codes | Browser (authorization response), then oauth2-proxy (code redemption) |
| Gateway and automation client secrets (HTTP Basic) | oauth2-proxy, machine clients |
| Access, ID, and refresh tokens (token responses) | Keycloak |
| Lab role-management client secret and admin tokens | `tools/security-lab-local` (`verify-realm`, `verify-authorization`) |

A passive capture of the hop in a lab built from `main` (`capture-idp-hop`,
counts only) found during one sign-in and one client-credentials request: the
password once, the authorization code twice, both clients' Basic credentials,
the issued access token, and 12 plaintext HTTP messages.

ADR 0006 left this hop plaintext because `idp` had two members, so only the
peer could observe. It named "`idp` gains a member" as the trigger to encrypt
it first. Inspection for this decision found that the premise did not hold:

- the lab tool's admin-API probe joins `idp` and sent the role-management
  client secret and admin tokens to `http://keycloak:8080`;
- the reachability probes of `verify` join `idp` as well;
- Keycloak's management interface (`:9000`, health) and its embedded cache
  cluster (JGroups `:7800` with mTLS, failure detection `:57800`) listen on the
  `idp` address.

Only the ingress and the admin probe call Keycloak. The API and oauth2-proxy
reach the issuer only through the ingress, over the `app` network with the
lab CA, so the public issuer URL does not depend on this hop.

## Decision drivers

| ID | Requirement |
| --- | --- |
| K1 | Confidentiality and integrity of the hop against another member of `idp` (threats T1–T3) |
| K2 | The ingress verifies the chain and the name it dialed; no plaintext fallback; Keycloak offers no plaintext listener for realm traffic (T6) |
| K3 | Unchanged public contract: issuer URL, discovery metadata, redirect and front-channel URLs, forwarded-header handling, browser and machine flows |
| K4 | Neither the browser-trusted lab CA nor the datastore CA can vouch for Keycloak, and the CA that can vouches for nothing else (T7) |
| K5 | Failures are closed and observable at the ingress, with a reason, without logging credentials |
| K6 | Issuance, trust distribution, renewal and reload, and ownership reproducible by the lab tool; renewal without restarting Keycloak or the ingress |
| K7 | No application code change; lifecycle cost proportionate to the threat removed |

## Options

### Who signs Keycloak's certificate

| Option | Assessment |
| --- | --- |
| **A. The lab CA** (browser-trusted) | Fails K4. The lab CA is name-constrained to `work-items.localhost`, so Keycloak would need a name below it; the ingress would then trust a CA that people also trust in Windows and browsers, and whose key already signs the ingress's own certificate |
| **B. The internal CA**, extended to permit `keycloak` | Name constraints are fixed in the CA certificate, so every existing lab would have to replace its internal CA, reissue both datastore certificates, and restart the API and worker (pgx reads the CA when its pool is created). One key would then vouch for all three services, and ADR 0006's exact-name policy would change |
| **C. A separate identity-provider CA**, exactly `keycloak`, trusted only by the ingress | Meets K4 without touching ADR 0006: neither CA's key can vouch for the other hop. Cost: a third CA (90 days, key in `pki/`) and one more trust file |
| **D. A self-signed certificate pinned in the ingress** | No CA, but every renewal changes the trust anchor: the ingress's trust file must change and reload in step with Keycloak, or carry old and new anchors. Renewal becomes a two-party operation (K6) |
| **E. A public or enterprise CA (ACME)** | No public name and no enterprise PKI in a local lab |

### How the hop is protected

| Option | Assessment |
| --- | --- |
| **F. Server-authenticated TLS**, the ingress verifies chain and name | Meets K1–K2. Client identity is not authenticated at the transport layer |
| **G. Mutual TLS** (`https-client-auth=required`) | Adds a check that a caller holds a client key, which would restrict direct access to Keycloak on `idp`. It adds no confidentiality over F. The client key would live in the ingress next to its server key, and the lab's admin probe would need one too. Not adopted |
| **H. TLS passthrough** (Keycloak terminates browser TLS) | End-to-end, but the ingress loses its route allow-list (administration console and master realm hidden), header replacement, and logging, and Keycloak would need the public certificate's key. Rejected |
| **I. Plaintext with tighter network membership** | Cannot exclude the tool's probes, which are needed. Leaves T1–T3. Rejected |

mTLS is not assumed: its benefit here is access control, not transport
protection, and that is not the problem this decision solves.

## Decision

**C + F**: Keycloak serves only HTTPS with a certificate from a separate
identity-provider CA; the ingress verifies it.

- **Identity-provider CA.** ECDSA P-256, 90 days, `pathlen:0`; permitted
  `DNS:keycloak`, excluded `DNS:.keycloak` and all IP addresses (the same
  exact-name pattern as ADR 0006). Both exclusions are load-bearing: a
  certificate naming `keycloak` plus `extra.keycloak` or plus
  `IP:127.0.0.1` passes the ingress's hostname check, and only the
  constraints refuse it (measured; without the IP exclusion OpenSSL
  accepted the IP variant). Created by `init`; its key never leaves
  `pki/` (mode 600). Its certificate is mounted only into the ingress (and
  into the tool's test probes).
- **Keycloak's certificate.** 14 days, SAN `DNS:keycloak` (the name the
  ingress dials), `serverAuth`, key and certificate in one
  `idp-tls/tls.pem` replaced by a single rename, in a mode-711 directory
  mounted read-only, as for the other services.
- **Keycloak.** `http-enabled=false`; HTTPS on 8443 from that file
  (`https-certificate-file` and `https-certificate-key-file`), TLS 1.2 and
  1.3, reread every 35 s (`https-certificates-reload-period`, which must be
  more than 30 s). The management interface (health only) stays plain HTTP
  and is bound to `127.0.0.1` (`http-management-scheme=http`,
  `http-management-host`) for the container health check. `hostname` and
  `proxy-headers=xforwarded` are unchanged.
- **Ingress.** `proxy_pass https://keycloak:8443` with
  `infra/security-lab/ingress/keycloak-upstream.conf`: `proxy_ssl_verify on`,
  depth 1, the identity-provider CA as the only trust anchor,
  `proxy_ssl_name keycloak` (also sent as SNI), TLS 1.2 and 1.3, and
  `proxy_ssl_session_reuse off`. The upstream stays a variable, so nginx
  keeps no idle connections to Keycloak. Forwarded headers are set as before.
- **Diagnostics.** The public server suppresses error-log entries below
  `crit`, because nginx's error log writes the request line (with
  authorization codes on failed callbacks); that also hides upstream TLS
  errors. A loopback-only server (`127.0.0.1:8082/keycloak-upstream`) fetches
  issuer discovery with the same include and logs at `error` level. Its
  request carries no credentials, so its log line states the TLS verdict
  without exposing any. `check-keycloak-upstream` drives it.
- **Lab tooling.** The admin-API probe uses `https://keycloak:8443` and
  verifies against the identity-provider CA.
- **Preflight.** `check-secrets` (and so `start`) refuses a missing,
  expired, wrong-name, or mismatched Keycloak certificate, an
  identity-provider CA whose constraints are not exactly `keycloak`, and a
  `keycloak.conf` that enables HTTP or lacks the certificate or the
  loopback-bound management interface.
- **Renewal and ownership.** `issue-keycloak-certificate` issues, replaces
  the file, and succeeds only when five consecutive new connections verify
  (code 0) with the new serial; Keycloak picks the file up on its next reload.
  `init` reissues within two days of expiry or when the certificate does not
  verify. The lab operator owns the CA and certificate through the tool.
  Nothing renews automatically; there is no revocation.

## Consequences

- **The hop no longer carries credentials in clear.** The same capture after
  the change found no password, code, client secret, or token and no
  plaintext HTTP message, while the TLS ClientHello was seen (positive
  control).
- **The public contract is unchanged.** The discovery document's digest was
  identical before and after, also with spoofed forwarding headers; redirects,
  sign-in, machine tokens, and sign-out work as before.
- **A certificate problem is an availability problem for new
  identity-provider requests.** Discovery, sign-in, and token requests
  through the ingress fail with 503 `identity_provider_unavailable`, and so
  would session refresh and sign-out, which use the same route (not exercised
  under a bad certificate); access tokens already issued keep working at the
  API until they expire. The public access log shows `upstream=502`; the
  reason is in the diagnostic route's log line.
- **No masking in the chosen configuration.** The ingress keeps no upstream
  connections and does not reuse sessions, so each request verifies the
  certificate served at that moment. Measured controls show what would mask a
  failure: a kept-alive upstream connection opened before the change kept
  answering 200. Keycloak resumed a saved TLS session while its certificate
  was unchanged, and after every change declined it and performed a full
  handshake with the new certificate. No session was resumed across a
  change, so revalidation of a resumed session was not exercised (a resumed
  session carries no certificate). Session reuse stays off so the ingress
  does not depend on that server behavior. A future upstream keep-alive for
  performance must bound connection lifetime or be paired with an ingress
  reload on renewal.
- **Cost.** A full TLS handshake per request from the ingress to Keycloak
  (latency not measured); a third private CA with its own key and 90-day
  lifetime; Keycloak's certificate change takes effect within 35 s.
- **Upgrade.** Labs created before this decision are refused by
  `check-secrets` until `init` (new CA, certificate, `keycloak.conf`) and
  `start`; the Keycloak database is kept. Exercised twice in the isolated lab
  only.
- **Not changed.** Any member of `idp` can still reach every Keycloak path
  over TLS; Keycloak's cache-cluster listeners stay on `idp`; the other
  plaintext hops of ADR 0006 stay plaintext.

## Reconsideration conditions

- Another component must call Keycloak directly (for example a backchannel
  from the API): give it the identity-provider CA, or move to a common
  internal PKI with per-hop issuing policy.
- Direct access to Keycloak on `idp` must be restricted to the ingress:
  mTLS (option G) or a separate network for the tool's probes.
- More than one Keycloak node: a certificate per node or a shared service
  name, and an analysis of the cluster transport.
- Certificates are issued by automation (for example cert-manager or an
  internal ACME service): replace the tool's issuance and wait steps and add
  expiry monitoring.
- A fourth private CA appears: consolidate into one root with constrained
  intermediates rather than parallel roots.

## Capability levels

No change.

## Evidence

From the [Keycloak TLS experiment](../experiments/issue-121-keycloak-tls.md),
in a separate lab project (`zero-to-prod-security-lab-kctls`), scripted
clients only:

- `capture-idp-hop` before (configuration of `main`) and after;
- `verify-keycloak-tls`: baseline, untrusted CA, lab CA, wrong name,
  expired, and excluded-subdomain certificates with new versus established
  connections, session resumption, control nginx variants, recovery and
  renewal without restarts, and a log search;
- `verify`, `verify-failures`, `verify-realm`, `verify-authentication`,
  `verify-log-hygiene`, `verify-authorization`, `verify-internal-tls`, and the
  ingress renewal in the same lab.

Rerun on 2026-10-17 with the final files in the same lab (scripted
clients only; per-run file hashes in each directory's `provenance.json`):

- `rerun/`: the whole suite, with the same counts as the first run and
  0 failures.
- `rerun-ip/verify-keycloak-tls.json`: 167 passed checks, 60 observations,
  0 failed, 0 skipped. It adds a `keycloak` + `IP:127.0.0.1` certificate from the
  identity-provider CA, refused at nginx's upstream verification
  (`48:excluded subtree violation`, 503 on the public route) and recovered
  by renewal with sign-in. It also adds an isolated control in which the
  same nginx verifier accepted the equivalent certificate from an
  unconstrained CA.

A browser sign-in was attempted and blocked: Windows does not trust the
separate lab's CA. Browser sign-in is unverified for this decision.

Open: TLS client-certificate (mTLS) authentication to Keycloak (absent;
OAuth clients authenticate with client credentials inside TLS), Keycloak's cluster listeners,
automated renewal and monitoring, the remaining plaintext hops, browser
verification, and single runs.
