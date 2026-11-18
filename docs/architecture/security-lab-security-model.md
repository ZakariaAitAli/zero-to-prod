# Security boundaries and risks

Scope: the local security lab. The Docker host, its administrators, repository
administrators and reviewed build inputs are trusted. This model describes
configuration and residual exposure; it is not a production security assessment.

## Assets and trust boundaries

Protect Work Item data/results, accepted jobs, user and client credentials,
session cookies, access tokens, database/broker credentials, CA/server keys,
artifacts and availability.

Clients cross a loopback HTTPS ingress. The gateway manages browser sessions;
the API independently validates tokens and enforces route permissions. Separate
networks contain application, datastore, identity, session and identity-database
services. Private networks do not make another attached container trustworthy.

Source credentials and CA keys live outside Git. Services receive only their
required files. Image build contexts and CI permissions are separate boundaries.
Recovery must preserve the database's business invariant and delivery identity.

## Transport

| Connection | Protection | Remaining boundary |
|---|---|---|
| Client → ingress | HTTPS with private lab CA | Browser trust and host security. |
| Ingress → API and session gateway | Chain/name-verified TLS using the application CA | No mutual TLS; other network members can connect. |
| Ingress → Keycloak | Chain/name-verified TLS using the identity-provider CA | No mutual TLS; administrative paths still require access control. |
| API/worker/migrator → PostgreSQL | TLS with server verification | Runtime/migration roles and application semantics remain trusted. |
| API/worker → RabbitMQ | Verified AMQPS; no plaintext listener | Broker permissions and message validation remain necessary. |
| Session gateway → Valkey | Plaintext on the session network | Session data and credentials require encryption before expanding this boundary. |
| Keycloak → its database | Plaintext on the identity-data network | Realm data and credentials require encryption before expanding this boundary. |
| Local health and operator probes | Container-local or explicit lab networks | Probe membership and diagnostic routes must not expose public administration. |

Established TLS connections are not re-verified after a certificate file changes.
Ingress API keep-alives are bounded; other service-specific renewal behavior is
in the [security guide](../guides/local-security-lab.md). CA keys share one trusted
host. Name constraints limit misissuance, not host compromise. Renewal is manual;
there is no general certificate revocation service.

## Threats and controls

| Threat | Implemented control | Remaining exposure |
|---|---|---|
| Spoofed identity | Token signature/issuer/audience/expiry validation; ambiguous cookie+bearer refusal | A stolen token remains usable until expiry/skew; no general token revocation. |
| Unauthorized action | Deny-by-default API route permissions | Route-level roles; no per-item tenant model. |
| CSRF/session misuse | Secure/HttpOnly/host-only cookies and same-origin write/logout checks | Browser/origin compromise, stolen live sessions and lack of MFA. |
| Transport observation or impersonation | Verified TLS on the listed hops | Plaintext session/identity-data networks; no mutual TLS. |
| Secrets in files, logs or artifacts | Private state, per-service mounts, restricted build contexts and sanitized logging | Host/Docker administrators can read secrets; allowlists and known-value checks are not exhaustive detection. |
| Partially applied rotation | Journaled API database password rotation, old-session termination and consumer recreation | Other secret types lack equivalent procedures; no distributed fencing. |
| Abuse/overload | Per-peer and aggregate ingress budgets, 429 and Retry-After | Shared-NAT fairness, distributed abuse, TLS/socket exhaustion and unbounded backlog below admission limits. |
| Compromised build inputs | Pinned images, CI checks and release scans | Pinning/signatures do not establish correctness or exhaustive vulnerability coverage. |
| Replay or loss during restore | Delivery identity and offline redrive | Snapshot losses, protocol coordination, external side effects and exactly-once delivery remain unresolved guarantees. |
| Operational failure | Explicit dependency errors and diagnostic tooling | Shared host/disk, manual response, incomplete integrated alerting and unproven production capacity. |

## Changes that require reassessment

Review network membership, trust roots and verifier behavior before moving a
service to another host. Reassess before internet exposure, multiple replicas,
additional issuers or tenants, new operator paths, or combining this profile
with capacity, availability, delivery or operations labs. Check actual running
listeners and privileges; configuration alone is not verification.
