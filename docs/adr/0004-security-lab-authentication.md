# ADR 0004 — Authentication boundary

Status: Proposed. The local implementation is Keycloak, oauth2-proxy and Valkey;
production selection and operation remain separate decisions.

## Decision

Use a session gateway for browser sign-in and server-side sessions. Keep access
tokens out of browser JavaScript. Machine clients supply their own access token.
The API independently validates signature, issuer, audience and expiry on both
paths; ingress authentication is not sufficient authorization.

Store sessions server-side so gateway logout can invalidate session state.
Reject requests carrying both a session cookie and a bearer token. Protect
state-changing browser requests with same-origin and CSRF checks.

## Alternatives and rationale

A cookie-only store simplifies infrastructure but has different revocation and
size constraints. A combined identity-aware proxy could reduce components but
must forward the intended token and preserve API validation. A different identity
provider must meet the same issuer, audience and lifecycle requirements.

## Consequences

The gateway, identity provider and session store become availability dependencies.
Local token validation can continue while a previously issued token and cached
signing key remain valid; it is not immediate token revocation. Password, client
secret, signing-key and session lifecycles need explicit procedures. This choice
does not provide MFA, tenant isolation or production identity operations.

See the [security guide](../guides/local-security-lab.md) for operation and the
[authorization decision](0005-security-lab-authorization.md) for permissions.
