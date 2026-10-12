# ADR 0005 — Security lab authorization: permission source and enforcement point

## Status

Proposed (provisional). Builds on
[ADR 0004](0004-security-lab-authentication.md), which is also Proposed and
unchanged by this decision.

## Context

After the authentication slice, every authenticated principal could perform
every Work Items action: the lab user and the automation client had identical
power. Issue #121 requires explicit authorization rules, authentication and
authorization demonstrated as separate controls, and an "authenticated but
unauthorized request" experiment.

The Work Items API exposes four business routes: list items, create an item,
request processing, and read a processing job. Browser sessions reach the API
through oauth2-proxy (nginx `auth_request`). Machine clients send their own
bearer token, and the ingress passes it to the API without involving the
gateway. The API validates every token locally against Keycloak's keys.

### Threat model (bounded to this decision)

| Asset | Exposure |
| --- | --- |
| Work Items data | Read and changed through the API |
| Processing capacity | Each process request costs worker and broker work |
| Access tokens | Bearer credentials, valid until expiry (300 s lifetime, 30 s skew) |
| Role assignments in Keycloak | Changing them changes what a principal can do |

Abuse cases:

- an authenticated but under-privileged person changes data;
- a leaked automation secret is used to create content;
- permission names placed in claims the API should not honor;
- an issuer-signed token for another audience;
- a revoked role still used by tokens already in circulation;
- a new route shipped without an authorization decision;
- unauthorized principals probing whether items exist.

## Decision drivers

| ID | Criterion |
| --- | --- |
| D1 | Both paths (browser session and machine bearer) enforce the same rules |
| D2 | Per-route and per-method rules |
| D3 | Least privilege: people and machine clients get only what they need |
| D4 | Deny by default: no permission, no access; a route cannot exist without a decision |
| D5 | Distinguishable failures: 401 (authentication) versus 403 (authorization) |
| D6 | Permissions come from one claim that misplaced or malformed data cannot satisfy |
| D7 | No new per-request dependency on the identity provider |
| D8 | No schema change or ownership feature in this slice |

## Options

**Enforcement point**

- *API:* the API already validates every token, sees both paths, and knows the route and method.
- *Edge:* oauth2-proxy `allowed_roles`/`allowed_groups` apply per auth endpoint, not per route or method. The bearer path bypasses oauth2-proxy, so nginx would need JWT parsing (njs or a module). Fails D1 and D2 without extra machinery.

**Permission source**

- *Keycloak client roles* on a `work-items-api` client, in
  `resource_access["work-items-api"].roles`: per principal (person or service
  account), namespaced to this API, so misplaced roles are detectable.
- *OAuth scopes:* describe what a client was delegated, not what a person may
  do. Two people using the same gateway client would get the same scopes
  unless scope issuance were tied to role mappings, which amounts to roles.

**Policy engine** (OPA, Cedar): four routes, three permissions, and static
role checks with no attributes or ownership do not justify one.

## Provisional decision

Enforce in the API, with permissions from Keycloak client roles. The edge stays
authentication-only.

| Route | Permission | `lab-user` (editor) | `lab-viewer` | `lab-outsider` | `work-items-automation` |
| --- | --- | --- | --- | --- | --- |
| `GET /items` | `items.read` | ✓ | ✓ | — | ✓ |
| `POST /items` | `items.create` | ✓ | — | — | — |
| `POST /items/{id}/process` | `items.process` | ✓ | — | — | ✓ |
| `GET /processing-jobs/{id}` | `items.read` | ✓ | ✓ | — | ✓ |

- **Least privilege:**
  - reading discloses data but changes nothing;
  - creating adds content and is needed only by the editor;
  - processing consumes capacity and is needed by the editor and the automation client.

  The automation client cannot create, so a leaked automation secret cannot add content. People get the composite roles `editor` or `viewer`. `lab-outsider` is an authenticated test principal with no Work Items permission; it holds permission names only as a realm role and a gateway-client role, which must grant nothing.
- **Claims:**
  - only `resource_access["work-items-api"].roles` counts, and every entry must be a string;
  - any malformed shape grants no permissions;
  - unknown names are ignored, and matching is exact;
  - claims are read only after signature, issuer, audience, and expiry checks pass.
- **Enforcement:**
  - business routes are registered through an interface whose only method requires a permission; the zero permission panics at startup;
  - registration functions receive that interface, not the mux.

  This prevents accidental omission, not deliberate bypass: a handler registered directly on the mux in the constructor would escape it. The constructor registers only the three operational routes, and request tests cover every business route in the matrix.
- **Responses:**
  - missing, invalid, or expired token: 401 with a Bearer challenge;
  - unavailable keys: 503;
  - valid token without the permission: 403 `{"error":"insufficient_permissions"}` with `WWW-Authenticate: Bearer realm="work-items", error="insufficient_scope"`.

  The check runs before the handler, so 403 precedes 404 and 409. Denials are logged by the API with subject, client, permission, and route. Edge-generated 403s, such as CSRF rejections, are not.
- **Development:** `AUTHENTICATION=disabled` disables both checks.

## Consequences

- Role changes take effect only for newly issued tokens. A token issued before
  a removal keeps its roles until expiry plus skew (up to about 330 s after
  issuance). Browser sessions pick up changes at the gateway's next refresh.
- Realms imported before this decision lack the roles. Keycloak does not
  re-import an existing realm, so such a lab refuses Work Items requests with
  403 until it is recreated. The authorization experiment therefore runs in a
  separate lab project.
- A role-management client (`work-items-lab-admin`) exists in the lab realm
  for readiness checks and role-removal experiments. It is a standing
  privileged credential, reachable only on the internal identity-provider
  network.
- The UI cannot know permissions in advance; it explains a 403 after the
  fact.

## Reconsideration conditions

- Rules need attributes, ownership, or context (consider a policy engine or per-item checks).
- Delegated access by third-party clients appears (add scopes alongside roles).
- Revocation before expiry becomes a requirement (shorter lifetimes, introspection, or a deny list).
- Edge enforcement becomes necessary, for example for non-API upstreams.
- The measured role-removal delays differ materially from the token-lifetime bound.

## Capability levels

No change.

## Later evidence

### 2026-10-12 — Authorization experiment

From the [authorization experiment](../experiments/issue-121-authorization.md),
run in a separate lab project; scripted clients only:

- **The matrix held on both paths.** Six unauthorized actions were refused with 403 and left no side effect (item counts unchanged, items still pending). The same actions succeeded against the API built from the pre-authorization commit `0682b52`, so the checks detect missing authorization. Allowed actions succeeded on both.
- **Claims:**
  - permission names only as a realm role or another client's role gave 403;
  - an issuer-signed token for another audience, and a token with a role added under its signature, gave 401;
  - 403 preceded 404 and 409.
- **Logs:** an authorization denial produced one API log line and `upstream=403` at the ingress; a CSRF rejection produced none and `upstream=-`.
- **Role removal, as first-observed rejections:** polled every 5 s plus request time; each change happened between the last successful request and the first rejection.
  - a browser session at 43 s after removal (the gateway's refresh was due at about 39 s);
  - tokens issued before removal at 312 s, against computed boundaries of `exp` + 30 s skew at 309 s and 310 s;
  - a token issued after removal on its first request.
- **Identity provider down:** outcomes were unchanged.
- **Role-management client:** an irreducible role set (`view-clients`, `manage-users`), determined in a throwaway project.

Open: browser verification (operator); the original lab is not upgraded; single runs.
The decision stays Proposed pending review.
