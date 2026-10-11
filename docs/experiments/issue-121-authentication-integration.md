# Issue #121 — authentication integration

Recorded 2026-10-11. This record covers integrating the candidate chosen after
the [authentication comparison](issue-121-authentication-comparison.md) into
the base security lab: Keycloak, oauth2-proxy behind nginx `auth_request`
with a Valkey session store, and access-token validation in the Work Items
API. Keycloak, oauth2-proxy, and Valkey remain provisional;
[ADR 0004](../adr/0004-security-lab-authentication.md) stays Proposed.

## Question

With the integrated stack, do browser sessions and machine clients reach Work
Items only with tokens the API validates itself; do sign-out, cookie replay,
and captured-token replay behave as defined; are state changes protected
against cross-site requests; and do the defined dependency outages produce the
defined responses?

Environment classification: **LOCAL-FIRST**, in the security lab on one
workstation (Windows 11, WSL 2 in NAT mode, Docker Desktop with Docker Engine
29.6.2, Compose 5.3.1).

## What was built

| Part | Change |
| --- | --- |
| API | `internal/authn`: local validation against the issuer's JWKS (one signature, RS256 allow-list, known key ID with one rate-limited refresh, signature, issuer, audience, required `exp`, 30 s skew). Middleware on every Work Items route: 401 with a Bearer challenge for missing or rejected tokens, 503 `authentication_unavailable` when keys cannot be fetched. `AUTHENTICATION` must be `disabled` or `oidc` |
| Lab services | Keycloak 26.8.0 with its own PostgreSQL, oauth2-proxy 7.15.5, Valkey 9.1.2 (no persistence), on new internal networks `idp`, `idp-data`, and `session` |
| Ingress | Session path (`auth_request`, token replaces client credentials, cookie dropped), bearer path for machine clients, refusal of requests with both, CSRF checks, POST-only same-origin sign-out, Keycloak routes limited to the realm and resources, readiness-based store-outage classification |
| Sign-out | oauth2-proxy deletes the Valkey session and calls Keycloak's end-session endpoint with the session's ID token (`backend_logout_url`) |
| Web UI | Sends `X-Requested-With: work-items-web`; shows **Sign in** on `unauthenticated` and a POST **Sign out** form when the gateway reports a session |
| Logging | The ingress access log records the path without the query string and no headers or cookies, and keeps the status, upstream status, upstream address, upstream connect time, total request time, and a request ID. In the two public servers, nginx error-log entries below `crit` from request processing are suppressed, because nginx's error log writes the full request line and referrer; this includes the `error`-level entries for upstream connection failures and timeouts. oauth2-proxy's request log omits the request URI; its auth and error logs are unchanged. The request ID is passed to oauth2-proxy and returned to the client |
| Tooling | `security-lab-local` generates the realm, gateway, and store configuration from per-lab secrets; `verify-authentication` exercises the behavior below; `verify-log-hygiene` searches the logs for authentication material; `verify` and `verify-failures` cover the new services and settings |

Lifetimes: access token 300 s; oauth2-proxy refreshes a session older than
1 min on its next request; Keycloak SSO session 30 min idle, 8 h maximum.

## Methods and their limits

- **Browser sign-in is scripted**: curl follows the gateway's redirects and
  submits Keycloak's login form. No browser exercised the flows in this run.
- **Captured browser token**: the harness reads the token a session forwards
  from oauth2-proxy's `/oauth2/auth`, using a probe container on the internal
  `app` network. This stands in for a token captured downstream of the gateway;
  no public route exposes it.
- **Token rejection through the ingress** uses a client-credentials token with
  a changed payload under its original signature, an unsigned (`alg: none`)
  variant, a malformed value, and a Keycloak-signed ID token whose audience is
  the client, not the API. Wrong-issuer, unknown-key, and other algorithm
  cases are covered by unit tests only.
- **Rejection reasons** come from the API's log categories, recorded alongside
  the HTTP results.
- **Log hygiene** is checked with the actual values of a run, described
  under [authentication material in logs](#authentication-material-in-logs).
- **Concurrent key refresh** (unit test): the test that concurrent requests
  for an unknown key share one JWKS refresh detects a check-then-fetch race
  only probabilistically. Against the previous non-atomic shape, applied to a
  temporary copy, it failed in 17 of 20 runs at 1000 rounds; the final code
  passed every run. A deterministic test would need a hook in the validator.
  CI repeats the test five times under the race detector, which cannot run
  locally (no C compiler in WSL); that CI step has not run yet.
- **Expected results** were written in the harness before the run.
- One complete run per check; earlier runs that exposed defects are listed
  under [corrections](#corrections).

## Results

Evidence: `evidence/issue-121/auth-integration/` (`verify-authentication.json`,
`verify.json`, `missing-secret-failures.json`, `log-hygiene.json`,
`log-hygiene-previous-config.json`, `session-refresh-previous-ingress.json`).

`verify-authentication` (with the slow expiry checks): 72 checks passed, 10
observations recorded, none failed. `verify`: 78 checks passed, 4
observations, 1 skipped (the Windows "without lab CA" control, because Windows
trusts the lab CA). `verify-failures`: 10 of 10 cases passed.

### Token validation through the ingress

| Token | Result | API log category |
| --- | --- | --- |
| Client-credentials token (control) | 200 | — |
| Same token, payload changed under the original signature | 401, `error="invalid_token"` | `signature` |
| Unsigned (`alg: none`) | 401 | `algorithm` |
| Malformed value | 401 | `malformed` |
| Keycloak-signed ID token (`aud` = the client) | 401 | `audience` |
| No credentials; Basic credentials | 401 | — |

Decoded claims (recorded for orientation, not as validation): machine token
`aud=account,work-items-api`, `azp=work-items-automation`; forwarded browser
token `aud=work-items-api`, `azp=work-items-gateway`; both RS256, 300 s.

### Work Items on both paths

Through the session path (browser cookie) and the bearer path (machine
client), each: create (201), process (202), poll until `succeeded`, process
again (409 `work_item_already_done`). A session request that also carried a
bearer token was refused with 400 `ambiguous_credentials`.

### CSRF (session cookie present)

| Request | Result |
| --- | --- |
| POST without `X-Requested-With` | 403 |
| POST from `Origin: https://attacker.example` | 403 |
| POST with `Sec-Fetch-Site: cross-site` | 403 |
| POST with `Sec-Fetch-Site: same-site` | 403 |
| POST with the header, same `Origin`, `same-origin` (control) | 201 |
| GET without the header | 200 |
| Sign-out by GET | 405 |
| Sign-out POST without `Origin`; from another origin | 403; 403 |
| Session after the refused sign-outs | still 200 |

A machine client's POST without the header was accepted (201): the bearer
path carries no ambient credential.

### Sign-out, cookie replay, and captured-token replay

These are recorded separately because they answer different questions.

| Question | Check | Result |
| --- | --- | --- |
| Gateway session | Same browser after a same-origin POST sign-out (302 to `/`) | 401 |
| Cookie replay | Copy of the session cookie taken before sign-out | 401 |
| Keycloak session | Next sign-in with the same browser's Keycloak cookies | Login form (before sign-out: silent sign-in) |
| Keycloak session | Captured access token at Keycloak's userinfo | 401 |
| Captured-token replay | Same token at the API after sign-out, 298 s before expiry | 200 |
| Captured-token replay | Same token after expiry plus the 30 s skew | 401, `expired` |

The captured token is accepted until it expires, as expected under local
validation: the API does not consult Keycloak per request, so ending the
Keycloak session does not revoke tokens already issued.

### Dependency outages

| Condition | Result |
| --- | --- |
| Keycloak down: existing session, refresh not yet due | 200 |
| Keycloak down: machine token, API holding cached keys | 200 |
| Keycloak down: new sign-in; token endpoint | 503; 503 |
| Keycloak down, API restarted (no cached keys): unexpired token | 503 `authentication_unavailable` |
| Keycloak down, API restarted: no token | 401 |
| Keycloak down: session after a refresh was due (API restarted, no keys) | 503 (observation) |
| Keycloak back: API fetches keys; new sign-in | 200; 200 |
| Keycloak down again: session whose access token had expired | 401 after 6 s |
| oauth2-proxy down: session; machine client; no credentials | 503; 200; 503 |
| oauth2-proxy back: same session without sign-in | 200 |
| Valkey down: machine client | 200 |
| Valkey back: earlier session; new sign-in | 401 (sessions not persisted); 200 |

**Expiry is enforced at two boundaries.** These are two separate
observations:

1. **Gateway.** The browser session whose access token expired while Keycloak
   was down was rejected by oauth2-proxy. Its refresh failed and it answered
   401; the readiness classification then reported the store as reachable, so
   the ingress answered 401 after 6 s. The browser request did not reach the
   API: the API logged no rejection for it.
2. **API.** The captured-token replay sent an expired token directly to the
   API on the bearer path, without the gateway. The API rejected it and
   logged `expired`, which verifies the API's own expiry check independently
   of the gateway.

The comparison expected the API's `exp` check to bound the outage tolerance
for browser sessions. In this run the gateway enforced that bound first; the
API's check remains a second boundary that this browser request did not
reach.

### Store-outage classification (separate observation)

| Condition | Result |
| --- | --- |
| Valkey up: forged session cookie | 401 |
| Valkey down: existing session | 503 |
| Valkey down: forged session cookie (cannot be checked) | 503 |
| Valkey down: no session cookie | 401 |

nginx asks oauth2-proxy's `/ready` only after a 401 for a request carrying a
session cookie. `/ready` reports the store's state when asked, not the cause
of the original 401, so a request that races a store failure or recovery can
be misclassified. These results show the classification under steady outage
and steady recovery only.

### Session refresh through `auth_request`

oauth2-proxy refreshes a session older than 1 min on its next request and
answers the auth subrequest with a new session cookie: a new signed value with
`Path=/; Max-Age=28800; HttpOnly; Secure; SameSite=Lax`. Observed directly on
the internal network, the value differed from the login cookie.

Before the fix, nginx discarded that header. After a refresh was due, the
client received no `Set-Cookie`, and its cookie kept the login value and expiry
(observed with a probe and with the control below). The ingress now captures
the gateway's `Set-Cookie` with `auth_request_set` and adds it to the API
response unchanged. It repeats the server's security headers in that
location, because a location-level `add_header` replaces the inherited ones.

**Method and limit.** These checks use a scripted client: curl with a cookie
jar, which stores and resends cookies as instructed by `Set-Cookie`. A browser
applies further rules (the `__Host-` prefix, SameSite, cookie storage
policies) that curl does not enforce. Whether a real browser accepts, stores,
and resends the refreshed cookie is **not yet verified**; it is part of the
outstanding manual browser check.

| Check (session path, scripted cookie-jar client) | Fixed ingress | Previous ingress (control) |
| --- | --- | --- |
| Request before a refresh is due: status; session `Set-Cookie` headers | 200; 0 | 200; 0 |
| Request after a refresh is due: status | 200 | 200 |
| Refreshed session cookie received by the client | 1 | **0** |
| Its attributes | `Path=/ HttpOnly Secure SameSite=Lax Max-Age=28800` | **none** |
| Security headers on that response (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `X-Request-Id`) | 4 | 4 |
| Client's stored cookie value changed | yes | **no** |
| Signed issue time in the stored cookie advanced (observation) | 70 s | 0 s |
| Next request with the stored (refreshed) cookie; further `Set-Cookie` | 200; 0 | 200; 0 |
| State change with the stored cookie and the UI headers | 201 | 201 |
| Machine client response: `Set-Cookie` headers | 0 | 0 |

The control (`session-refresh-previous-ingress.json`) ran the same quick
suite with the previous `nginx.conf` mounted through a temporary Compose
override; only the three marked checks failed. The previous ingress still
authenticated the next request because the login cookie remains valid on the
server. Per oauth2-proxy's cookie validation, that cookie is rejected once its
signed issue time is older than `cookie_expire` (8 h), however recently the
session refreshed. That consequence was not measured here. Cookie values were
compared by digest and are not recorded.

### Authentication material in logs

`verify-log-hygiene` drives five flows and records the sensitive values it
uses:

| Flow | Path exercised |
| --- | --- |
| Successful sign-in, then API calls with the session and a machine token | Callback through the ingress and oauth2-proxy (302), session request (200), bearer request (200) |
| The same authorization code redeemed again | oauth2-proxy callback failure (403) |
| A fresh code redeemed while Keycloak is down; an authorization request to the stopped Keycloak | oauth2-proxy redemption failure (500); ingress upstream failure (503) |
| A fresh code returned while oauth2-proxy is down | ingress upstream failure on the callback (503) |
| The first session's next request after a refresh is due | Refreshed session cookie returned through the ingress (200) |

It then searches the logs written during the run for 16 values: 3
authorization codes, 4 state values, the session cookie before and after the
refresh, the session's access token, a machine access token, and 5 long-lived
secrets (both client secrets, the cookie secret, the Valkey password, and the
lab-user password). It also searches the 10 percent-encoded forms that
differ from them, which is how URLs appear in logs: 26 strings in all.
Values are never printed or recorded; the evidence holds counts and value
kinds only.

Positive controls, in the same run:

- the scanner finds each of the 26 strings in a synthetic log line;
- the ingress log window contains a unique, non-secret control path requested
  during the run;
- the callback's request ID appears in both the ingress and the oauth2-proxy
  log windows, so both logs were captured;
- what diagnostics remain was observed: oauth2-proxy logged its callback
  error, and the ingress access log recorded the 503s with upstream status
  502 or 504. nginx's error-log entries for those upstream failures were
  suppressed, so their reasons were not logged.

| Configuration | ingress | oauth2-proxy | Keycloak, API, Valkey, worker (observed) |
| --- | --- | --- | --- |
| Committed (`log-hygiene.json`) | 0 | 0 | 0 |
| Previous logging (control, `log-hygiene-previous-config.json`) | 3: 1 code, 2 encoded state | 6: 3 codes, 3 encoded state | 0 |

The control ran the same check with the previous logging configuration
mounted through a temporary Compose override: the nginx error log at the main
level and oauth2-proxy's default request log format. In that configuration the
nginx error log recorded the gateway-down callback and the authorization
request to the stopped Keycloak. oauth2-proxy's request log recorded the
callback URIs of the successful, reused, and Keycloak-down flows. Its capture
controls passed, so the leaks it found show that the check detects real
leakage. The committed configuration was then restored.

**Scope.** The result applies to the flows above: in them, none of the
26 searched strings appeared in the ingress or oauth2-proxy logs. It is
not a claim about other flows (for example sign-out, or error paths not driven
here), debug-level logging (off), or Keycloak's event logging (disabled by
default).

## Corrections

- The first lab run answered 503 for every valid token. jwx registers each
  JWKS URL with its own HTTP client and substitutes its default client unless
  one is given at registration, so the client trusting `OIDC_CA_FILE` was
  never used. The validator now passes it to `Register`. A unit test fetches
  keys over TLS from a server whose certificate only the configured CA trusts;
  it fails with the previous wiring (`x509: certificate signed by unknown
  authority`) and passes now. The unit tests had used a plain-HTTP server.
- The same run showed that the validator discarded the cause of a key-fetch
  failure, so the log said only "signing keys unavailable". The cause is now
  wrapped into the error and logged; no token material is included.
- Editing scripts through the Windows path to the WSL checkout dropped their
  executable bits; one such change had been committed and was restored in a
  separate commit.
- The comparison's Keycloak volume still held a realm with that experiment's
  client secrets. Keycloak skips importing an existing realm, so the lab's
  volume is named `identity_postgres_data`; the old volume was left in place
  (see [pending cleanup](#pending-cleanup)).
- The first integration run logged authorization codes (oauth2-proxy's
  request log) and authorization-request parameters (nginx's error log).
  Logging was changed and `verify-log-hygiene` added before committing.
- The first log-hygiene runs had three defects, each exposed by its own
  controls:
  - Five secrets were written without line breaks and merged into one search
    string, so 10 instead of 15 values were searched. Values are now
    collected with their kind and counted per kind.
  - Restoring a stopped service recreated its container when its
    configuration differed from the Compose file, which discarded its earlier
    log output. The capture controls failed in the previous-configuration
    control run. Restores now start the existing container.
  - Values were searched only URL-decoded, while logs contain URLs
    percent-encoded, so state values went unseen. Encoded forms are now
    searched as well.

- After the first commits, a review of the `auth_request` path found that
  refreshed session cookies never reached the browser. The earlier checks
  observed refresh only on the server side: the session kept working because
  the login cookie stays valid on the server. The ingress now forwards the
  cookie, and a scripted cookie-jar refresh test was added. Its control fails against
  the previous ingress.

## Findings for later slices

- oauth2-proxy's auth log records the user's email and ID for each sign-in;
  personal data, not authentication material. Input for the logging review.
- Suppressing the public servers' nginx error-log entries below `crit` hides
  upstream failure reasons (for example a connection refusal versus a
  timeout); the access log keeps status, upstream status and address,
  upstream connect time, request time, and request IDs.
- No authorization: the lab user and the machine client can do everything.
- The access-token lifetime bounds both captured-token replay and the
  identity-provider-outage tolerance; shortening it trades against refresh
  load.
- oauth2-proxy trusts `X-Forwarded-*` from any client on the `app` network,
  which includes the API.

## Outstanding verification

A manual check in a real browser (Brave on Windows) is outstanding. All flows
in this record were driven by scripted clients. It should cover sign-in
through Keycloak, creating and processing a Work Item, the session continuing
across a gateway refresh (the browser storing and resending the refreshed
`__Host-wi-session` cookie), and sign-out ending both the gateway and the
Keycloak session. The guide lists the steps.

## Pending cleanup

The comparison's leftovers remain and are **pending cleanup** by the operator.
Neither `destroy` nor `purge-state` removes them, because they are not part of
the lab's Compose model or state directory:

- the Docker volume `zero-to-prod-security-lab_keycloak_postgres_data` (its
  realm holds the comparison's client secrets);
- the state directory
  `~/.local/state/zero-to-prod/zero-to-prod-security-lab-auth-experiment/`
  (the comparison's generated secrets and certificate).

The comparison's evidence in `evidence/issue-121/auth-comparison/` is kept.

## Capability levels

No change. This is one integration of one candidate, verified with scripted
sign-ins and single runs; any level change is assessed when #121's evidence
is complete.
