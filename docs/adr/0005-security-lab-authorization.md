# ADR 0005 — API authorization

Status: Proposed.

## Decision

Enforce permissions in the API on both browser and bearer paths. Use Keycloak
client roles from `resource_access["work-items-api"].roles`, after token validation.
Do not treat a realm role, another client's role or OAuth scope as an API permission.
Malformed claim shapes grant no permission; names match exactly and unknown names
are ignored.

| Route | Required permission |
|---|---|
| `GET /items` | `items.read` |
| `POST /items` | `items.create` |
| `POST /items/{id}/process` | `items.process` |
| `GET /processing-jobs/{id}` | `items.read` |

The editor has all three permissions; the viewer has read only. The automation
client can read and process but cannot create. Business-route registration requires
a permission; direct mux registration must be reviewed to prevent bypass.

## Rationale

The API sees the method, route and validated principal on both paths. Enforcing
only at the session gateway would miss machine requests. A policy engine adds
little value for this small static role model without ownership attributes.

## Consequences

Invalid credentials return 401; unavailable signing keys return 503; a valid
principal lacking permission receives 403. Role changes are bounded by token/session
lifetime, not immediate request-time identity-provider lookup. There is no per-item
tenancy, ownership policy or distributed quota. Reassess before adding those features.
