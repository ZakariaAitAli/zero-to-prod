# Issue #121 — authentication comparison: feasibility stage

Recorded 2026-10-11. This record covers the feasibility stage of the
authentication comparison proposed in
[ADR 0004](../adr/0004-security-lab-authentication.md). It checks the critical
assumptions and compares session-gateway configurations before any API or
browser-UI integration. Keycloak, oauth2-proxy, and the server-side store
remain provisional.

## Question

Can the security lab provide browser sessions held server-side, a single
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
publishes a host port. The experiment uses short lifetimes so expiry is
observable: access token 120 s, oauth2-proxy refresh after 60 s. Browser login
is a scripted authorization-code flow (curl following Keycloak's form); no
browser was used in this stage.

The issuer is `https://auth.work-items.localhost:9443/realms/work-items`. The
ingress serves it on 8443 and 9443 internally and has network aliases for the
lab host names, so browsers and containers use the same URL. Certificates come
from the existing lab CA, whose name constraint already covers
`*.work-items.localhost`.

## Critical assumptions

| Assumption | Method | Result |
| --- | --- | --- |
| One issuer URL with verified TLS from the browser side | Discovery document from WSL `curl` and Windows `curl.exe`, both with the lab CA | **Confirmed**: issuer string identical |
| … and from containers | oauth2-proxy and Pomerium (Go resolver, lab CA) completed OIDC discovery and stayed running | **Confirmed** |
| Container curl as a client | curl in a container on the same network | Failed (exit 7): curl resolves `*.localhost` names to loopback itself. The URL works for Go clients; client-specific `.localhost` handling must be checked per client |
| Access-token audience, machine client | Client-credentials token decoded | **Confirmed**: `aud` includes `work-items-api` (also Keycloak's default `account`) |
| Access-token audience, browser client | Token received upstream after login, decoded | **Confirmed**: `aud=work-items-api` for oauth2-proxy and Pomerium clients |
| oauth2-proxy forwards the IdP access token through nginx `auth_request` | nginx copies `X-Auth-Request-Access-Token` into `Authorization`; an experiment-only upstream echoes it | **Confirmed** (A1 and A2) |
| Pomerium forwards the IdP access token | `set_request_headers: Authorization: Bearer ${pomerium.access_token}` | **Confirmed** after requesting `openid profile email` explicitly (see below) |
| Server-side session invalidation after logout | Copy the session cookie, sign out, replay the copy | **Confirmed for A2 and B**; A1 accepts the replay, as expected for a client-side store |

Valkey worked as oauth2-proxy's Redis store, resolving that ADR 0004
assumption. No token or secret value appears in the evidence; tokens are
summarized as claims (`iss`, `aud`, `azp`, `typ`, lifetime).

## Measured behavior

Evidence: [`evidence/issue-121/auth-comparison/`](../../evidence/issue-121/auth-comparison/).
Run files are kept with their failed expectations: oauth2-proxy cookie runs 1
and 2, and Pomerium runs 1–3, as described under
[Corrections during the stage](#corrections-during-the-stage). The tables
below use oauth2-proxy cookie run 2, the server-side run, and Pomerium run 3.

### Logout, IdP-side termination, and token replay

| Behavior | A1 cookie | A2 server-side | B Pomerium |
| --- | --- | --- | --- |
| Gateway sign-out, same browser | 401 | 401 | 302 to sign-in |
| Gateway sign-out, replayed copied cookie | **200 (accepted)** | 401 | 302 to sign-in |
| Session ended in Keycloak (admin API) | Gateway still serves until its refresh fails: 401 after 60–61 s | 401 after 60 s | not measured |
| Captured access token after gateway sign-out | Unchanged expiry; Keycloak userinfo 200 (gateway sign-out does not end the Keycloak session) | same | not measured |
| Introspection by the gateway client | `active=false` before and after sign-out | same | not measured |

The introspection control shows `active=false` while the session is active,
so introspection by the gateway client is not a usable revocation signal in
this configuration (the cause was not investigated). A captured access token
stays usable at the identity provider until it expires or the Keycloak
session ends; an API that validates tokens locally would accept it until
expiry. That window is bounded by the access-token lifetime.

### Outages

Expected results were defined before each run.

| Outage | Expected | A1 cookie | A2 server-side | B Pomerium |
| --- | --- | --- | --- | --- |
| Identity provider down, existing session, token unexpired | 200 | 200 | 200 | not measured |
| … refresh due, token unexpired | 401 (run 1) | 200 | 200 | — |
| … access token expired | 401 | 401 | 401 | — |
| Identity provider down, new login | 503 `identity_provider_unavailable` | 503 | 503 | — |
| Identity provider restored, new login | 200 | 200 | 200 | — |
| Gateway down, existing session | 503 `authentication_unavailable` | 503 | 503 | 503 |
| Gateway restored, existing session | 200 without re-login | 200 | 200 | **302** (in-memory sessions lost; expected) |
| Session store down, existing session | 503 `authentication_unavailable` | — | **401** | — |
| Session store restored (no persistence) | 401, then new login 200 | — | 401, 200 | — |

Two expectations were wrong and are kept as defined:

- When a refresh fails because the identity provider is unreachable,
  oauth2-proxy keeps the existing session until the access token expires; it
  does not reject at the first failed refresh.
- When the server-side store is unreachable, oauth2-proxy answers 401, not an
  error. A store outage is therefore indistinguishable from being signed out at
  the gateway; it would need monitoring through oauth2-proxy's `/ready`
  endpoint, which reports Redis connectivity.

## Footprint

Conditions: Docker Desktop VM with 15.4 GiB memory and 20 CPUs; the five base
security-lab services running; each configuration started cold from removed
containers, with Keycloak's database volume already holding the imported
realm. `docker stats` was sampled continuously (each call takes about 1–2 s)
for 150 s from start (startup peak), then five samples 10 s apart after 300 s
without traffic (idle median). Values are container memory usage as reported
by Docker.

| Service | A1 cookie peak / idle | A2 server-side peak / idle | B Pomerium peak / idle |
| --- | --- | --- | --- |
| Keycloak | 899.4 / 769.1 MiB | 816.5 / 787.4 MiB | 859.1 / 774.7 MiB |
| Keycloak PostgreSQL | 39.6 / 39.1 MiB | 41.5 / 39.1 MiB | 40.7 / 38.6 MiB |
| Gateway | oauth2-proxy 6.3 / 4.4 MiB | oauth2-proxy 6.6 / 5.4 MiB | Pomerium 71.8 / 65.4 MiB |
| Session store | — | Valkey 7.9 / 5.8 MiB | (in Pomerium) |
| **Combined idle** | **812.6 MiB** | **837.7 MiB** | **878.7 MiB** |

All three are below the provisional 1.5 GiB investigation threshold. Keycloak
accounts for about 93 % of each total, and its idle value varied by 18 MiB
across the three cold starts, more than the A1/A2 difference. The
gateway-plus-store difference between A2 (11.2 MiB) and B (65.4 MiB) is small
relative to Keycloak. Peaks are single-run observations, not distributions.

## Comparison against the ADR 0004 criteria

| Criterion | A1 cookie | A2 server-side | B Pomerium |
| --- | --- | --- | --- |
| C1 code flow with PKCE | Measured (S256 in the authorization request) | Measured | Measured (S256) |
| C2 one issuer URL, verified TLS | Measured | Measured | Measured |
| C3 IdP access token reaches the upstream | Measured | Measured | Measured, after setting scopes |
| C4 client credentials | Measured (token audience; not yet through the ingress) | same | same |
| C5 server-side sessions, revocation on logout | **Not met**: copied cookie accepted after sign-out | Measured | Measured, when the sign-out redirect is followed |
| C6 cookie attributes | Secure, HttpOnly, `Path=/`, SameSite=Lax configured; 3.4 KB cookie | Same; 180-byte ticket | Not inspected |
| C8 nginx remains the single ingress | Yes; one host, `/oauth2/*` paths | Yes | Partly: Pomerium handles routing for its own hosts and needs a separate authenticate host |
| C10 defined outage behavior | Measured | Measured; **store outage presents as 401** | Gateway down and restart measured; others not |
| C11 footprint | 812.6 MiB idle | 837.7 MiB idle | 878.7 MiB idle |
| Session survival | Gateway restart: kept | Gateway restart: kept; store restart: lost | Gateway restart: lost |

**Provisional assessment.** A1 is eliminated by C5, as designed. A2 and B both
meet the critical assumptions. A2 keeps nginx as the only router, uses one
host name for the application, and keeps sessions across gateway restarts;
its weakness is that a store outage looks like sign-out. B adds no separate
store service but loses sessions on every gateway restart, routes its own
hosts, and was measured on fewer behaviors. Footprint does not separate them.
The measured evidence favors A2 for this lab; B remains a credible alternative
if the separate store or its outage signal proves costly. This is a
recommendation for review, not an accepted decision; ADR 0004 stays Proposed.

## Corrections during the stage

- Compose resolved the overlay's relative path from the base file's directory,
  and Docker created a stray directory for the missing source (the same
  behavior recorded for missing secret files); the path was corrected and the
  directory removed.
- oauth2-proxy run 1 logged in while a restarted Keycloak was still
  bootstrapping (Keycloak answered 503 despite the Compose health wait); the
  harness now waits for the discovery document. Run 1 is retained.
- nginx initially hung on a stopped gateway's cached address; short resolver
  and connect timeouts now give an immediate 503.
- Pomerium run 1 failed the code exchange: Pomerium requests `offline_access`
  by default and the realm grants no offline tokens; Keycloak rejected the
  exchange and Pomerium retried the used code. Requesting `openid profile email`
  fixed it.
- The first footprint run started oauth2-proxy alongside a cold Keycloak:
  oauth2-proxy's startup OIDC discovery failed while Keycloak was still
  starting, and the process exited with no restart policy, so it was missing
  from the samples. Adding a health dependency was not enough: the experiment's
  Keycloak health check matched the string `"UP"` in the readiness response,
  which a nested check reports while Keycloak is still bootstrapping. Sampled
  during a restart, `/health/ready` returned 503 while the discovery document
  returned 503, and 200 at the same moment discovery returned 200. The check
  now requires HTTP 200, the gateways depend on it, and the footprint was
  remeasured. The behavioral runs were unaffected because Keycloak was already
  running when the gateways started.
- Pomerium run 2 measured the copied-cookie replay without following
  `/.pomerium/sign_out`'s redirect; the route host only redirects to the
  authenticate host, which ends the session. Run 3 follows the flow. Keycloak's
  post-logout redirect for Pomerium also needed the authenticate host.

## Findings for later slices

- oauth2-proxy's access log records the one-time authorization code in
  callback URLs and the user's email and ID: input for the logging review.
- oauth2-proxy trusts `X-Forwarded-*` from any address unless
  `--trusted-proxy-ip` is set.
- Keycloak warns that "Full scope allowed" is deprecated; the realm should list
  required roles explicitly when the authorization slice adds roles.
- Gateway sign-out does not end the Keycloak session unless the sign-out flow
  redirects to Keycloak's end-session endpoint (Pomerium does so; the
  oauth2-proxy configuration here does not yet).

## What this does not prove

- Any browser behavior: login was scripted with curl.
- API token validation: the API does not validate tokens yet; the
  upstream-view endpoint only shows what would reach it.
- Machine-client calls through the ingress.
- Behavior under load, multiple gateway instances, or key rotation.
- Pomerium's behavior for IdP-side termination, token replay, and identity
  provider or store outages, which were not measured in this bounded arm.

## Capability levels

No change. This stage is feasibility evidence for one identity provider and a
bounded comparison of gateway configurations; ADR 0004 remains Proposed.
