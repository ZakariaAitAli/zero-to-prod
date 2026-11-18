# ADR 0008 — TLS to the API and session gateway

Status: Proposed.

## Decision

The API and oauth2-proxy serve only HTTPS on the application network. Ingress
verifies each server's chain and exact name using a separate application CA,
constrained to `api` and `oauth2-proxy`, excluding subdomains and IP addresses.
Each CA is trusted only by the clients of its intended hops.

The API reloads valid key pairs and retains its last valid pair if replacement
fails. oauth2-proxy reads its pair at startup and must be recreated after renewal.
Ingress's API keep-alive connections are bounded; established connections are not
re-verified. Gateway requests use new upstream connections.

## Rationale and alternatives

Native TLS avoids an extra terminating sidecar and plaintext application hop.
Private networks alone do not protect tokens or sessions. Mutual TLS would add
caller restrictions but does not replace application authentication.

A shared root with constrained intermediates would simplify trust-anchor
rotation. Consolidation is deferred because it changes every verifier, certificate
and client lifecycle together. Four parallel roots remain a maintenance cost,
not a recommended production topology. Consolidate before a fifth CA, automated
issuance or moving a CA off the lab host.

## Consequences

A bad or expired certificate causes availability failure for new connections.
The API retaining an old pair does not keep that pair valid indefinitely. Verify
new connections after renewal, and handle inode replacement and consumer reload
explicitly. Keys share one trusted host; separate CAs limit misissuance, not host theft.

See the [security guide](../guides/local-security-lab.md) and
[security model](../architecture/security-lab-security-model.md).
