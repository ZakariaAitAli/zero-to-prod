# Security lab trust boundaries and residual risks

Scope: local Docker reference workload, ADRs 0004–0008 and 0010–0011. The host,
Docker administrator, GitHub repository administrators, and reviewed build
inputs are trusted. This is a bounded security model, not a production-ready
claim. The [transport model](security-lab-threat-model.md) details each network,
its members, certificates, and active/passive transport threats.

## Assets and boundaries

Assets include Work Item data/results and durable acceptance, user passwords,
OAuth client credentials, access tokens, gateway/session cookies, database and
broker credentials, CA/server keys, source/artifact integrity, and availability.

Untrusted clients cross the loopback HTTPS ingress. The ingress separates
allow-listed UI/API/gateway/realm routes from internal/admin paths. The session
gateway and identity provider establish a principal; the API independently
validates tokens and enforces route permissions. Private networks separate
application, datastore, identity, session, and identity-database participants.
Each service receives its own file mounts. The host state boundary protects
source credentials and CA keys; CI and image build contexts form a separate
artifact boundary. Database/broker recovery changes delivery responsibility
and must preserve the business invariant, not just transport availability.

## Threats, controls, and remaining exposure

| Threat | Existing control/evidence | Remaining exposure |
| --- | --- | --- |
| Spoofed identity or confused credentials | Strict issuer/audience/signature validation; malformed/expired token refusals; ambiguous bearer+cookie rejected; authentication experiments | Local validation accepts a stolen token until expiry/skew; no token revocation |
| Unauthorized action or ownership access | Deny-by-default API route permissions, principal/action matrix and side-effect checks | Route-level roles only; no per-item tenancy/ownership model |
| CSRF and session misuse | Secure/HttpOnly/host-only/Lax cookie, same-origin proof on writes and logout, replay/logout experiments | Cookie theft before logout and compromised browser/origin remain threats; no MFA |
| Transport observation or impersonation | Verified TLS on ingress upstreams and the data network; wrong-name/issuer/expiry controls; private networks | Valkey and identity-database hops remain plaintext by the recorded provisional transport decision; CA keys share a trusted host |
| Credential exposure in artifacts, arguments or logs | State outside Git, per-service file mounts, sanitized logs, image/layer/history and known-value scans | Host/Docker admins can read values; unknown encodings/unknown credentials exceed known-value scanning; migrator URL argument remains documented |
| Stale or partially propagated secret | Recoverable API database rotation, old-session termination, old/new login controls, consumer recreation | Other secret types lack equivalent procedures; rotation is disruptive and not distributed fencing |
| Abuse and accidental overload | Per-peer and aggregate ingress budgets, immediate 429, Retry-After, forwarding-header spoof controls | Shared NAT fairness, distributed attackers, TLS/socket exhaustion, per-principal quotas, and below-threshold backlog remain |
| Compromised image/dependency or CI writer | Pinned build inputs, CI gates, restricted build contexts | Full SBOM/vulnerability detection and exercised patching belong to #125; signatures do not prove code correctness |
| Loss, replay or ambiguity across restore | ADR 0003 completion invariant; #130/#131 delivery recovery tracked separately | Protocol-version and offline-recovery coordination are required; no exactly-once or recovery beyond the snapshot |
| Availability and operational failure | Explicit dependency errors and tested failure paths | Single host/dependencies, no proved capacity or SLO; #122–#126 remain necessary |

## Reconsideration

Revisit this model before internet exposure, multiple hosts/ingress replicas,
new principals or tenants, external business effects, a fifth CA or automated
issuance, off-host CA custody, a secret-manager integration, or new telemetry.
New telemetry must preserve credential hygiene and bounded metric labels.
Security acceptance follows measured controls and explicit gaps, not a claim
that every threat has been eliminated.
