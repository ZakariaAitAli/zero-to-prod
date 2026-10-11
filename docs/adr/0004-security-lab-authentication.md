# ADR 0004 — Security lab authentication: identity provider, session gateway, session store

## Status

Proposed (provisional). The comparison and a first integration are recorded
under [later evidence](#later-evidence); the decision awaits review.

## Context

The security lab ([architecture](../architecture/security-lab.md)) has an HTTPS
ingress, private networks, a private lab CA, and file-based runtime secrets, but
no authentication. Issue #121 requires browser authentication with server-side
sessions, machine-client authentication, and independent token validation in the
Work Items API. Authorization policy, datastore TLS, rate limiting, and secret
rotation are later slices.

An earlier design review agreed on server-managed sessions: tokens are held by a
session gateway, not by browser JavaScript, and logout revokes the session
immediately.

## Decision drivers

| ID | Criterion |
| --- | --- |
| C1 | Standard OIDC authorization code flow with PKCE; the gateway is a confidential client |
| C2 | One issuer URL reachable from the browser and from containers with verified TLS, behind the existing nginx ingress |
| C3 | The API independently validates identity-provider-issued tokens (signature, issuer, audience, expiry) with one trust anchor for browser and machine requests |
| C4 | Machine clients authenticate with the client-credentials grant |
| C5 | Server-side sessions; gateway logout revokes the session immediately |
| C6 | Tokens are not readable by browser JavaScript; Secure, HttpOnly, SameSite cookies; CSRF protection for state changes |
| C7 | Secrets are delivered as generated files or configuration, not environment variables or arguments |
| C8 | nginx remains the single ingress |
| C9 | Reproducible configuration from generated files; clean removal |
| C10 | Defined behavior when the identity provider, gateway, or session store is unavailable |
| C11 | Acceptable workstation footprint: provisional investigation threshold of 1.5 GiB combined idle memory for the new authentication services, with measurement conditions and startup peaks recorded separately. Exceeding it triggers analysis, not automatic replacement |
| C12 | Does not block later slices: roles in tokens, signing-key rotation, rate limiting by subject |

## Options

- **Identity provider:** Keycloak; Zitadel.
- **Session gateway:** oauth2-proxy behind nginx `auth_request`; Pomerium behind
  nginx.
- **Session store for oauth2-proxy:** encrypted cookie (client-side); a
  Redis-protocol server-side store.

## Documented basis

From official documentation read on 2026-10-11; nothing below is measured yet.

- Keycloak: `hostname` sets the published URLs, and back-channel URLs equal
  front-channel URLs by default; behind a TLS-terminating proxy only `/realms/`
  and `/resources/` should be exposed, never `/admin`, `/realms/master`, or the
  management port; `dev-file` is for development only; `--import-realm` reads
  realm files at startup and skips existing realms.
- Zitadel: requires PostgreSQL, a separate login service, and a 32-character
  masterkey that cannot change after initialization; serves only its configured
  external domain.
- oauth2-proxy: `/oauth2/auth` returns 202 or 401 for nginx `auth_request`; the
  cookie store is stateless; the Redis store keeps encrypted sessions server-side
  with only a ticket in the browser; PKCE S256 and file-based client secrets are
  supported.
- Pomerium: issues its own signed identity JWT to upstreams and never forwards the
  IdP ID token by default; `set_request_headers` can forward
  `${pomerium.access_token}` (not available with the hosted authenticate
  service); the `databroker` session store defaults to in-memory, with file and
  PostgreSQL options, and records are not encrypted.

Capabilities not required by this slice, and therefore not weighed: identity
brokering, federation, UMA, and token exchange (Keycloak); virtual instances,
the customizable login service, and management APIs (Zitadel); edge
authorization allow-lists (oauth2-proxy); policy language, device identity, and
built-in ingress (Pomerium).

## Provisional decision

Use Keycloak, oauth2-proxy, and a server-side Redis-protocol session store as
**experiment candidates**, not settled architecture. Compare:

- oauth2-proxy with the cookie store (control for C5) and with a server-side
  store (candidate);
- Pomerium with its in-memory `databroker`, limited to the IdP-token trust path,
  revocation, restart, outage, and footprint measurements, and stopped early if
  it cannot forward the IdP access token.

Run Zitadel only if Keycloak fails the issuer-URL or access-token audience
gates.

## Unresolved assumptions

- Container resolvers accept `*.localhost` names, or a static mapping works.
- oauth2-proxy in `auth_request` mode exposes the IdP access token to nginx.
- Keycloak issues JWT access tokens with `aud` including `work-items-api` for the
  browser client and the machine client.
- A copied cookie-store session remains usable after gateway logout; a
  server-side session does not.
- Valkey is compatible with oauth2-proxy's Redis store.
- The new services fit the footprint threshold.

## Consequences

A server-side store adds a stateful service with its own secret, health check,
updates, and failure handling, and sessions are lost when it restarts. nginx gains
`auth_request` routing and CSRF checks; the API gains token validation.

## Reconsideration conditions

- oauth2-proxy cannot forward the IdP access token through `auth_request`.
- The server-side store fails revocation or defined outage behavior.
- Pomerium forwards the IdP access token and meets revocation and outage criteria
  with a smaller operational footprint.
- Keycloak fails the issuer-URL or audience gates, or exceeds the footprint
  threshold without an acceptable explanation.
- The server-side-session requirement (C5) is explicitly revised.
- Multiple gateway instances are introduced, requiring shared persistent sessions.

## Capability levels

No change. A documentation comparison and a single implementation do not
establish comparison-level capability.

## Later evidence

The provisional decision above is unchanged. Entries record evidence gathered
after this ADR was proposed.

### 2026-10-11 — Feasibility stage

The [comparison record](../experiments/issue-121-authentication-comparison.md)
measured:

- One issuer URL with verified TLS from a Linux client (WSL `curl`), a Windows
  client (`curl.exe`; not a browser), and Go clients in containers
  (oauth2-proxy, Pomerium). curl inside a container resolves `*.localhost` to
  loopback itself, so client-specific handling must be checked.
- Access tokens whose decoded `aud` includes `work-items-api` for the browser
  and machine clients (claims decoded, not cryptographically validated).
- oauth2-proxy forwarding the IdP access token through nginx `auth_request`;
  Pomerium forwarding it through `set_request_headers` once its scopes exclude
  `offline_access`.
- Server-side session invalidation after gateway sign-out for oauth2-proxy with
  Valkey and for Pomerium; the cookie store accepted a copied cookie after
  sign-out and was removed from the experiment.
- Valkey compatibility with oauth2-proxy's Redis store.
- Combined idle memory of 812.6–878.7 MiB across the three configurations,
  below the investigation threshold, about 93 % of it Keycloak.

Measured behavior that contradicts this ADR's expectations: with the
server-side store unreachable, oauth2-proxy answers 401, so a store outage is
indistinguishable from sign-out; when the identity provider is unreachable,
existing sessions continue until the access token expires.

The measured evidence favors oauth2-proxy with a server-side store over
Pomerium for this lab; that choice is open for review.

### 2026-10-11 — Completion stage

Pomerium's remaining behavior was measured under the same conditions, and an
nginx classification for oauth2-proxy store outages was tested:

- **Identity-provider session termination:** both gateways rejected the
  session 60 s after Keycloak ended it.
- **Captured access-token replay after gateway sign-out:** the token's expiry
  was unchanged in both. oauth2-proxy's sign-out left the Keycloak session
  active (userinfo 200); Pomerium's sign-out continued to Keycloak's
  end-session endpoint (userinfo 401). Under the planned local-validation
  policy (signature, issuer, audience, and expiry checked against the issuer's
  keys, without per-request identity-provider calls), the expected behavior is
  acceptance until expiry in either case, pending validator tests.
- **Identity provider down:** oauth2-proxy kept existing sessions until the
  access token expired; Pomerium ended them at the first failed refresh. New
  logins returned 503 for both.
- **Gateway restart:** oauth2-proxy sessions survived (store intact);
  Pomerium's in-memory sessions were lost.
- **Store outage signal:** nginx distinguishes oauth2-proxy's 401 using the
  session cookie and oauth2-proxy's `/ready` endpoint; all six store cases
  matched expectations. Trade-offs: unverifiable cookies receive 503 during an
  outage, and rejected requests carrying cookies cost one extra gateway
  request.

**Recommendation for review:** integrate Keycloak, oauth2-proxy behind nginx
`auth_request`, a Valkey server-side store, and the store-outage
classification. Integration requirements: configure and test oauth2-proxy
sign-out through Keycloak's end-session endpoint, and test that API
validation of `exp` bounds the session tolerated while the identity provider
is unreachable. Pomerium remains the alternative if fail-closed refresh
becomes a requirement or the separate store proves costly.

**Unresolved:** no cryptographic token validation by the API yet; no browser
exercised the flows; machine-client calls through the ingress untested;
introspection by the gateway client returned `active=false` for active
sessions, cause not investigated; single runs only.

This ADR stays Proposed until integration evidence is recorded.

### 2026-10-11 — Integration stage

The candidate was integrated into the base security lab and verified with
`security-lab-local verify-authentication`
([integration record](../experiments/issue-121-authentication-integration.md)):

- **Validation in the API (C3):** local validation against Keycloak's JWKS,
  RS256 allow-list, issuer, audience, and required `exp`. Through the ingress,
  a changed payload, an unsigned token, a malformed value, and a
  Keycloak-signed ID token for another audience were rejected with 401; the
  API logged `signature`, `algorithm`, `malformed`, and `audience`.
- **Both paths (C4, C8):** browser sessions (oauth2-proxy, Valkey) and
  client-credentials tokens (bearer path, gateway not involved) created,
  processed, and polled Work Items with unchanged API semantics, including
  409 `work_item_already_done`. A request with both credentials is refused.
- **CSRF (C6):** session state changes without the UI header, from another
  origin, or with cross-site or same-site fetch metadata were refused with
  403; sign-out is POST-only and same-origin.
- **Sign-out (C5):** a copied session cookie was rejected after sign-out, and
  Keycloak's session ended (`backend_logout_url`): the next sign-in showed the
  login form and Keycloak's userinfo rejected the captured token.
- **Captured access-token replay:** accepted by the API after sign-out until
  expiry, as expected under local validation, and rejected by the API
  (`expired`) after expiry plus the 30 s skew.
- **Expiry at two boundaries:** a browser session whose access token expired
  while Keycloak was down was rejected by **oauth2-proxy** (failed refresh);
  that request did not reach the API. The captured-token replay
  independently verified the **API's** expiry check on the bearer path. The
  integration requirement expected the API's check to bound the outage
  tolerance for sessions; the gateway enforced it first, and the API's check
  is a second boundary that this browser request did not reach.
- **Outages (C10):** Keycloak down: existing sessions and machine clients
  with cached keys continued; new sign-ins and the token endpoint answered
  503; an API restarted without cached keys answered 503
  `authentication_unavailable` for unexpired tokens. Gateway down: sessions
  503, machine clients unaffected. Store down: sessions 503 through the readiness
  classification (a separate observation with a documented race), machine
  clients unaffected; sessions are lost when the store restarts.

- **Authentication material in logs:** the first integration run logged
  authorization codes and state (oauth2-proxy's request log, nginx's error
  log). After the logging change, a check that searches the logs for the 15
  actual codes, state values, cookie, tokens, and secrets of a run found none
  in the ingress or gateway logs for the tested flows: a sign-in and failed
  callbacks (reused code, Keycloak down, gateway down). Its positive controls
  passed, and the same check found codes and state with the previous
  configuration. Trade-off: in the public servers, nginx error-log entries
  below `crit` are suppressed, so upstream failure reasons are not logged;
  access logs keep status, upstream status and connect time, request time,
  and request IDs.

A defect found by the first lab run (the JWKS client did not use the
configured CA) was fixed with a regression test. The test for concurrent
key refreshes detects a non-atomic refresh only probabilistically (17 of 20
runs against the previous shape).

**Unresolved:** no browser exercised the integrated flows (scripted
sign-ins only); no authorization; single runs.

The decision stays Proposed pending review.
