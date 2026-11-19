# ADR 0007 — TLS to Keycloak

Status: Proposed.

## Decision

Keycloak serves HTTPS for ingress requests using a separate identity-provider CA.
Ingress verifies the certificate chain and exact name `keycloak`. The CA permits
that name and excludes its subdomains and all IP addresses; trust is limited to
ingress and explicit diagnostic probes. Keycloak's public plaintext listener is
disabled; its health interface is container-loopback only.

Ingress uses a new TLS connection with no session reuse for these requests so
certificate changes affect subsequent requests. A failed upstream verification
produces a dependency failure rather than a fallback to plaintext.

## Alternatives and rationale

Extending an existing CA reduces roots but increases the names one stolen key
can impersonate. Pinning a leaf certificate complicates renewal. Mutual TLS adds
caller authentication and key management beyond the current service boundary.
A constrained trust domain keeps issuer transport separate from browser trust.

## Consequences

New certificate failures can interrupt discovery, sign-in, refresh and sign-out.
Already-issued access tokens may remain valid at the API until expiry. There is
no general revocation service, automated renewal or host-compromise isolation.
Keycloak administration and cluster listeners still need their own access controls.

Multiple parallel CAs increase rotation cost. Consolidate trust domains before
adding more CAs, automating issuance or moving a CA off the host; ADR 0008 records
the current deferral and its limits. See the [security guide](../guides/local-security-lab.md).
