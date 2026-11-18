# ADR 0006 — TLS to PostgreSQL and RabbitMQ

Status: Proposed. API/gateway and identity-provider hops are specified separately
in ADRs 0007 and 0008.

## Decision

Use server-verified TLS from the API, worker and migrator to PostgreSQL, and from
the API/worker to RabbitMQ. Disable plaintext listeners. PostgreSQL clients use
`verify-full`; broker connections use verified AMQPS. Encryption without chain
and hostname verification is insufficient.

Use a separate, name-constrained internal CA for exactly `postgres` and `rabbitmq`,
excluding their subdomains and all IP addresses. Trust it only in the clients of
those hops. CA keys stay in private host state; service key pairs are mounted only
where required. Name policies must be verified with the actual TLS implementations.

## Alternatives and rationale

Private networks restrict reachability but do not prevent another member from
observing or impersonating a peer. Public certificates do not fit private service
names. Mutual TLS would restrict callers but adds another credential lifecycle;
service-specific authentication remains required with server-only TLS.

## Consequences

Renewal must replace the intended file, reload the service and verify new
connections. Existing pooled connections do not re-verify a changed certificate.
There is no automated renewal or general revocation service. Host/Docker
administrators remain trusted; keeping CA keys together does not isolate host theft.

The session-store and identity-database hops remain plaintext in this local
profile. Reassess them before moving off one host or expanding network membership.
Use the [security guide](../guides/local-security-lab.md) for renewal procedures.
