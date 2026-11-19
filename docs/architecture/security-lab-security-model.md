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

## Optional profiles and telemetry

The security, observability, capacity, availability and delivery profiles are
separate experiments. Do not merge their Compose files and inherit a security
claim from one profile. The current integration decision is to keep them isolated
until the following boundaries are explicitly configured and tested:

| Boundary | Current exposure and integration requirement |
|---|---|
| API/worker → collector → Jaeger/Loki | Observability uses unauthenticated plaintext OTLP/HTTP, collector-to-Jaeger gRPC and collector-to-Loki HTTP on its lab network. Logs/traces contain identifiers and business timing; a network member can observe, forge or exhaust telemetry. Restrict network membership; authenticated encrypted transport and ingestion quotas are required before expanding trust. |
| Prometheus → metrics/exporters | Plaintext unauthenticated scrapes expose resource, database and workload metadata. The database exporter has a dedicated monitor credential. Never publish metrics/exporters alongside the public ingress; scope exporter privileges and scrape identities when integrating. |
| Operator → dashboards/query APIs | Grafana permits anonymous read-only viewing; Grafana, Prometheus and Jaeger publish on loopback. A local process/browser can read telemetry, and Prometheus enables lifecycle reload. Host-local access is trusted here, not an authorization boundary for a shared host. Require authenticated operator access before sharing. |
| Multiple APIs and failover database names | Capacity uses dynamic discovery without authentication; availability adds replicas and database failover. A security integration must reconcile certificate SANs/name constraints, issuer reachability, verified failover endpoints and credential rotation for every consumer. Never use skipped hostname verification to bridge these profiles. |
| Broker topology | Security uses a classic queue; availability uses quorum queues. Queue declaration, publisher/consumer permissions, verified AMQPS node names and failure behavior must agree before combining them. Clustering alone does not secure or replicate a classic queue. |
| Immutable release/rollback | Delivery identity does not distribute trust or secrets. Candidate and rollback images need compatible TLS/authentication configuration, least-privilege runtime files and a tested renewal/rotation path. Never place runtime credentials in an image to simplify rollout. |

The local capacity harness and authenticated ingress comparison have different
workloads and boundaries; neither establishes their combined behavior. Telemetry
retention limits bound some storage use, not disclosure or authenticity. A
collector compromise can falsify observations; independent client checks remain
necessary. Third-party image findings are reported by the delivery tooling;
report generation is not a clean-image gate or vulnerability exception approval.

## Changes that require reassessment

Review network membership, trust roots and verifier behavior before moving a
service to another host. Reassess before internet exposure, multiple replicas,
additional issuers or tenants, new operator paths, or combining this profile
with capacity, availability, delivery or operations labs. Check actual running
listeners and privileges; configuration alone is not verification.
