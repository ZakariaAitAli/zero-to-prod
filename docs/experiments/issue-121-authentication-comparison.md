# Issue #121 — authentication comparison

Recorded 2026-10-11. This record covers the bounded comparison proposed in
[ADR 0004](../adr/0004-security-lab-authentication.md), in two stages on the
same day: a feasibility stage (critical assumptions, first gateway
measurements, footprint) and a completion stage (Pomerium's remaining
behavior and a session-store outage signal for oauth2-proxy). It precedes any
API or browser-UI integration. Keycloak, oauth2-proxy, and the server-side
store remain provisional; ADR 0004 stays Proposed.

## Question

Can the security lab provide server-side browser sessions, a single
verifiable issuer, and identity-provider access tokens that the Work Items API
could validate, and how do the candidate gateway configurations differ in
measured behavior and footprint?

Environment classification: **LOCAL-FIRST**, in the security lab on one
workstation (Windows 11, WSL 2 in NAT mode, Docker Desktop with Docker Engine
29.6.2, Compose 5.3.1).

## Configurations

| Configuration | Identity provider | Gateway | Session store |
| --- | --- | --- | --- |
| A1 (control) | Keycloak 26.8.0 with PostgreSQL 18.6 | oauth2-proxy v7.15.5 via nginx `auth_request` | Encrypted cookie |
| A2 (candidate) | same | same | Valkey 9.1.2 (Redis protocol), no persistence |
| B (alternative) | same | Pomerium 0.33.4 behind nginx | In-memory `databroker` |

All run as an isolated overlay (`infra/security-lab/auth-experiment/`,
`tools/security-lab-auth-experiment`) with its own state directory; nothing
publishes a host port. Lifetimes are shortened so expiry is observable: access
token 120 s, oauth2-proxy refresh after 60 s. The issuer is
`https://auth.work-items.localhost:9443/realms/work-items`; the ingress serves
it on 8443 and 9443 internally with network aliases for the lab host names, and
certificates come from the existing lab CA.

## Methods and their limits

- **Login** is a scripted authorization-code flow: curl follows the gateway's
  redirects and submits Keycloak's login form. No browser was used.
- **Windows checks** use Windows `curl.exe` (Schannel) with the lab CA. They
  show that a Windows TLS client reaches the issuer and verifies its
  certificate; they are not browser verification.
- **Token checks decode claims only** (`iss`, `aud`, `azp`, `typ`, lifetime,
  remaining validity) from the token's payload. They do not verify the
  signature. Cryptographic validation of signature, issuer, audience, and
  expiry by the Work Items API is part of integration and has not been
  performed.
- **What reaches the upstream** is shown by an experiment-only endpoint that
  echoes the forwarded `Authorization` header; the tooling records only the
  decoded claims, never the token.
- **Expected results** were defined before each run. Runs with wrong
  expectations or harness defects are kept in the evidence with their
  corrections.

## Critical assumptions

| Assumption | Method | Result |
| --- | --- | --- |
| One issuer URL with verified TLS, Linux client | Discovery document from WSL `curl` with the lab CA | **Confirmed**: issuer string identical |
| … Windows client | Windows `curl.exe` with the lab CA | **Confirmed** (a Windows client check, not a browser) |
| … containers | oauth2-proxy and Pomerium (Go resolver, lab CA) completed OIDC discovery and stayed running | **Confirmed** |
| Container curl as a client | curl in a container on the same network | Failed (exit 7): curl resolves `*.localhost` names to loopback itself. Client-specific `.localhost` handling must be checked per client |
| Access-token audience, machine client | Client-credentials token, decoded claims | **Confirmed**: `aud` includes `work-items-api` (also Keycloak's default `account`) |
| Access-token audience, browser clients | Token received upstream after login, decoded claims | **Confirmed**: `aud=work-items-api` for oauth2-proxy and Pomerium clients |
| oauth2-proxy forwards the IdP access token through nginx `auth_request` | nginx copies `X-Auth-Request-Access-Token` into `Authorization` | **Confirmed** (A1, A2) |
| Pomerium forwards the IdP access token | `set_request_headers: Authorization: Bearer ${pomerium.access_token}` | **Confirmed** after requesting `openid profile email` explicitly |
| Server-side session invalidation after gateway sign-out | Copy the session cookie, sign out, replay the copy | **Confirmed for A2 and B**; A1 accepts the replay, as expected for a client-side store |

Valkey worked as oauth2-proxy's Redis store.

## Measured behavior

Evidence: [`evidence/issue-121/auth-comparison/`](../../evidence/issue-121/auth-comparison/).
Tables use oauth2-proxy cookie run 2 (A1), oauth2-proxy server-side stage 2
run 2 (A2), and Pomerium stage 2 (B); the remaining files are earlier or
failed runs described under [Corrections](#corrections).

### Gateway-session revocation

Ending the gateway's own session, independent of the identity provider.

| Check | A1 cookie | A2 server-side | B Pomerium |
| --- | --- | --- | --- |
| Sign-out, same browser | 401 | 401 | 302 to sign-in |
| Sign-out, replayed copied cookie | **200 (accepted)** | 401 | 302 to sign-in |
| Session cookie | 3.4 KB encrypted session | 180-byte ticket | not inspected |

### Identity-provider session termination

Ending the user's Keycloak session through its admin API, without touching the
gateway.

| Check | A2 server-side | B Pomerium |
| --- | --- | --- |
| Immediately after | 200 | 200 |
| Gateway rejects after | 60 s (401) | 60 s (302 to sign-in) |

Both gateways notice only when they next refresh against Keycloak.

### Captured access-token replay

A token captured from the upstream while the session was active, examined
after gateway sign-out. Decoded claims only; no signature validation.

| Check | A2 server-side | B Pomerium |
| --- | --- | --- |
| Remaining validity after sign-out | Unchanged (~120 s) | Unchanged (~119 s) |
| Keycloak userinfo before sign-out | 200 | 200 |
| Keycloak userinfo after sign-out | **200**: oauth2-proxy's sign-out does not end the Keycloak session | **401**: Pomerium's sign-out continues to Keycloak's end-session endpoint |
| Introspection by the gateway client | `active=false` before and after sign-out; not usable as a signal here | not measured |

In both configurations a captured access token keeps its expiry after
sign-out. The token's expiry alone does not determine acceptance; that depends
on the validator. Under the planned validation policy, in which the API
verifies signature, issuer, audience, and expiry locally against the issuer's
keys and does not consult the identity provider per request, the **expected**
behavior is acceptance until expiry, whatever the identity-provider session
state, bounding the exposure by the access-token lifetime. This expectation is
pending tests against the actual validator.

### Outages

| Outage | Expected | A2 server-side | B Pomerium |
| --- | --- | --- | --- |
| IdP down, existing session, before refresh | 200 | 200 | 200 |
| IdP down, refresh due, token unexpired (75 s) | observed | **200**: session kept until the token expires | **302**: session ended when refresh fails |
| IdP down, access token expired | 401 (A2) / 302 (B) | 401 | 302 |
| IdP down, new login | 503 `identity_provider_unavailable` | 503 | 503 |
| IdP restored, new login | succeeds | 200 | succeeds |
| Gateway down, existing session | 503 `authentication_unavailable` | 503 | 503 |
| Gateway restored, existing session | A2: 200 without re-login; B: 302 (in-memory) | 200 | 302 |
| Store down, existing session | 503 `authentication_unavailable` | 503 (with the nginx classification below) | n/a |
| Store down, invalid session cookie | 503 (cannot be checked) | 503 | n/a |
| Store down, no session cookie | 401 | 401 | n/a |
| Store up, invalid session cookie | 401 | 401 | n/a |
| Store restored (no persistence), old session | 401, then new login 200 | 401, 200 | n/a |

## Session-store outage signal (oauth2-proxy)

**Problem.** In the feasibility stage, oauth2-proxy answered 401 when its
store was unreachable, so a store outage looked the same as being signed out.

**Mechanism tested.** nginx handles oauth2-proxy's 401 in a named location:

1. no session cookie in the request: 401 (genuinely unauthenticated);
2. a session cookie: an internal `auth_request` to oauth2-proxy's `/ready`
   endpoint, which reports session-store connectivity. Ready means the session
   is genuinely invalid: 401. Not ready (oauth2-proxy returned 500 with the
   store stopped) means it cannot be checked: 503 `authentication_unavailable`.

It needs `recursive_error_pages on` (the readiness failure occurs inside an
`error_page` redirect) and a `map` on `Cookie`, because nginx's `$cookie_*`
variables cannot name a cookie containing a hyphen.

**Result.** All six store cases matched expectations (table above).

**Trade-off.**

- An invalid or forged cookie gets 503 rather than 401 while the store is
  down: the system cannot tell, and fails closed with an infrastructure error.
- Each rejected request that carries a session cookie costs one extra request
  to oauth2-proxy. Junk cookies can therefore double the gateway's request
  load for rejected traffic; the rate-limiting slice should account for it.
- `/ready` reports the gateway's store connectivity at that moment, not the
  cause of the original 401; a store failing between the two requests is
  classified by the second.
- The classification depends on the cookie name matching the gateway
  configuration.
- It adds nginx configuration only; no gateway code changes.

## Footprint

Measured once in the feasibility stage; the completion stage changed no
candidate service.

Conditions: Docker Desktop VM with 15.4 GiB memory and 20 CPUs; the five base
security-lab services running; each configuration started cold from removed
containers, with Keycloak's database volume already holding the imported
realm. `docker stats` sampled continuously (each call takes about 1–2 s) for
150 s from start (startup peak), then five samples 10 s apart after 300 s
without traffic (idle median).

| Service | A1 cookie peak / idle | A2 server-side peak / idle | B Pomerium peak / idle |
| --- | --- | --- | --- |
| Keycloak | 899.4 / 769.1 MiB | 816.5 / 787.4 MiB | 859.1 / 774.7 MiB |
| Keycloak PostgreSQL | 39.6 / 39.1 MiB | 41.5 / 39.1 MiB | 40.7 / 38.6 MiB |
| Gateway | oauth2-proxy 6.3 / 4.4 MiB | oauth2-proxy 6.6 / 5.4 MiB | Pomerium 71.8 / 65.4 MiB |
| Session store | — | Valkey 7.9 / 5.8 MiB | (in Pomerium) |
| **Combined idle** | **812.6 MiB** | **837.7 MiB** | **878.7 MiB** |

All are below the provisional 1.5 GiB investigation threshold. Keycloak is
about 93 % of each total and varied by 18 MiB across cold starts; peaks are
single-run observations.

## Comparison against the ADR 0004 criteria

| Criterion | A1 cookie | A2 server-side | B Pomerium |
| --- | --- | --- | --- |
| C1 code flow with PKCE | S256 in the authorization request | same | same |
| C2 one issuer URL, verified TLS | Linux, Windows client, Go containers | same | same |
| C3 IdP access token reaches the upstream | Decoded claims match | same | Decoded claims match, after setting scopes |
| C4 client credentials | Token audience only; no call through the ingress | same | same |
| C5 server-side sessions, revocation | **Not met** | Met | Met when the sign-out redirect is followed |
| C6 cookie attributes | Secure, HttpOnly, `Path=/`, SameSite=Lax | same; ticket cookie | not inspected |
| C8 nginx as the single ingress | Yes; one host | Yes; one host | Partly: own routing and a separate authenticate host |
| C10 defined outage behavior | Measured | Measured, store outage distinguished | Measured; no store |
| C11 footprint | 812.6 MiB | 837.7 MiB | 878.7 MiB |
| Gateway sign-out ends the IdP session | No (as configured) | No (as configured) | Yes |
| Refresh failure with IdP down | Session kept until token expiry | same | Session ended at first failed refresh |
| Sessions after a gateway restart | Kept | Kept (store intact) | Lost |

## Recommendation

**Integrate A2 (Keycloak, oauth2-proxy behind nginx `auth_request`, Valkey
server-side store, nginx store-outage classification), subject to review.**

Reasons, from measured behavior:

- It keeps nginx as the only router and the application on one host name (C8),
  where Pomerium needs its own routing and a separate authenticate host.
- Sessions survive gateway restarts; Pomerium's in-memory sessions are lost on
  every restart, and persistence would require its PostgreSQL `databroker`,
  whose records are not encrypted (documented).
- Its one measured weakness from the feasibility stage, store outages
  presenting as sign-out, is resolved by configuration with a recorded
  trade-off.
- Footprint does not separate the candidates.

Pomerium measured better in two respects, which become integration
requirements for A2:

1. **Sign-out should end the Keycloak session.** oauth2-proxy documents a
   sign-out redirect (`rd`, `{id_token}`, `--whitelist-domain`) to the
   provider's end-session endpoint; the experiment did not configure it.
   Integration must configure and test it.
2. **Refresh failure.** oauth2-proxy keeps a session until the access token
   expires when the identity provider is unreachable. The exposure is bounded
   by the access-token lifetime (five minutes planned), and API validation of
   `exp` enforces that bound; integration must test it.

Pomerium remains a credible alternative if the separate store, the extra
nginx classification, or oauth2-proxy's refresh tolerance proves costly, or if
fail-closed refresh becomes a hard requirement.

## Unresolved gaps

- No API token validation yet: claims were decoded, not cryptographically
  validated.
- No browser exercised the flows; login was scripted.
- Machine-client calls through the ingress, and the bearer path in nginx, are
  untested.
- oauth2-proxy sign-out to Keycloak's end-session endpoint is not configured.
- Pomerium's cookie attributes, introspection, and store-related behavior were
  not inspected (it has no separate store).
- Introspection by the gateway client returns `active=false` even for an
  active session; the cause was not investigated.
- One run per configuration; no load, multiple gateway instances, key
  rotation, or long-running sessions.

## Corrections

Feasibility stage:

- Compose resolved the overlay's relative path from the base file's
  directory, and Docker created a stray directory for the missing source; the
  path was corrected and the directory removed.
- oauth2-proxy cookie run 1 logged in while a restarted Keycloak was still
  bootstrapping; the harness now waits for the discovery document. Run 1 also
  expected 401 once a refresh was due with the identity provider down; the
  observed 200 is kept.
- nginx initially hung on a stopped gateway's cached address; short resolver
  and connect timeouts now give an immediate 503.
- Pomerium run 1 failed the code exchange: Pomerium requests `offline_access`
  by default, Keycloak refused offline tokens, and Pomerium retried the used
  code. Requesting `openid profile email` fixed it.
- Pomerium run 2 replayed the cookie without following `/.pomerium/sign_out`'s
  redirect; the route host only redirects to the authenticate host, which ends
  the session. Run 3 follows the flow, and Keycloak's post-logout redirect for
  Pomerium also needed the authenticate host.
- The first footprint run started oauth2-proxy alongside a cold Keycloak. Its
  startup discovery failed and the process exited with no restart policy. The
  experiment's Keycloak health check had matched the string `"UP"`, which a
  nested readiness check reports during bootstrap; sampled during a restart,
  `/health/ready` returned 503 until the discovery document returned 200. The
  check now requires HTTP 200 and the gateways depend on it.

Completion stage:

- Adding the Keycloak dependency made every Compose command that enabled a
  gateway profile without the `keycloak` profile invalid; the tool's restore
  steps discarded the error, so Valkey never restarted. An apparent stale
  `/ready` was in fact a stopped store. The tool now always enables the shared
  profile and restore steps fail loudly.
- Stage 2 run 1 of the server-side arm wrote "no cookie" responses to an
  unwritable path, and nginx returned 500 instead of 503 until
  `recursive_error_pages` was enabled. Run 2 passed.

## Findings for later slices

- oauth2-proxy's access log records the one-time authorization code in
  callback URLs and the user's email and ID: input for the logging review.
- oauth2-proxy trusts `X-Forwarded-*` from any address unless
  `--trusted-proxy-ip` is set.
- Keycloak warns that "Full scope allowed" is deprecated; the realm should list
  required roles explicitly when the authorization slice adds roles.
- The store-outage classification doubles gateway requests for rejected
  traffic carrying cookies; rate limiting should consider it.

## After this record

A2 was integrated into the base security lab; see the
[integration record](issue-121-authentication-integration.md). The overlay
(`infra/security-lab/auth-experiment/`) and its tool
(`tools/security-lab-auth-experiment`) were then removed; their evidence stays
in `evidence/issue-121/auth-comparison/`. Integration requirement 1 was met
with oauth2-proxy's `backend_logout_url` (a server-side call to Keycloak's
end-session endpoint with the session's ID token) rather than the sign-out
redirect described above.

## Capability levels

No change. Two gateway architectures were implemented and compared on the same
criteria, but with one identity provider, scripted logins, decoded rather than
validated tokens, and single runs. Any level change is assessed when #121's
evidence, including integration, is complete.
