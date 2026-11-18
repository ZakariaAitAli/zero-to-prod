# Security lab

A separate local profile adds HTTPS ingress, Keycloak authentication,
oauth2-proxy sessions, Valkey session storage, API authorization and verified
TLS to application services and datastores. Only ingress is published, on host
loopback port 9443. This is not the unauthenticated development profile.

Read the [architecture](../architecture/security-lab.md) and
[security model](../architecture/security-lab-security-model.md) before changing
network membership, permissions or trust roots.

## Requirements

Docker with Compose, Bash, OpenSSL and curl. Initial builds need network access
for pinned images and application dependencies. Port 9443 must be free; issuer
URLs, client registrations and listeners depend on this fixed port.

## Initialize and start

From the repository root:

```sh
./tools/security-lab-local init
./tools/security-lab-local start
```

`init` creates private state outside Git, under
`~/.local/state/zero-to-prod/zero-to-prod-security-lab/`. It holds the lab CAs,
keys, generated passwords, derived connection files and service certificates.
Keep CA keys and source passwords private; do not copy this state into the
repository or a report.

Initialization preserves existing source secrets but may renew certificates
near expiry and regenerate derived files. `start` validates required files,
builds images, starts dependencies, applies migrations and starts consumers in
dependency order. Containers are recreated while volumes are retained, so their
secret mounts reference current files. Use the wrapper rather than a bare
Compose startup that can bypass preflight checks.

Do not run initialization, startup, renewal or rotation concurrently. Preserve
marked state and existing volumes when investigating failures.

## Access and trust

The application is reached through `https://work-items.localhost:9443`; the
issuer is `https://auth.work-items.localhost:9443/realms/work-items`. Resolve
both names to host loopback and trust only this lab's public CA certificate in
the browser profile used for testing. Never install a private CA key. Do not
disable certificate verification to make a request succeed.

Browser sign-in uses the session gateway. API clients provide their own bearer
access token. The API validates signature, issuer, audience and expiry itself;
it does not trust a principal simply because traffic came through ingress.
Session cookies and bearer credentials together are rejected as ambiguous.
State-changing browser requests require the same-origin CSRF controls.

Authorization is enforced by the API. Use the role assignments and verification
commands maintained by `tools/security-lab-local`; do not bypass authorization
by calling a backend or trusting a forwarding header.

The [transport threat model](../architecture/security-lab-security-model.md)
lists encrypted and remaining plaintext connections. Private networks alone do
not establish confidentiality.

## Verify

```sh
./tools/security-lab-local verify
./tools/security-lab-local verify-internal-tls
./tools/security-lab-local verify-app-tls
./tools/security-lab-local verify-authorization --skip-slow
./tools/security-lab-local verify-authentication --skip-slow
./tools/security-lab-local verify-log-hygiene
```

Use `./tools/security-lab-local --help` and the source for fixture prerequisites
and full checks. Failure verifiers deliberately change certificates, principals or
dependencies; run them only in the intended isolated lab, not against unrelated data. `--skip-slow` omits long-duration cases; do not describe that run as
complete expiration or revocation coverage. The historical authorization control
uses a separate temporary database and an unavailable publisher endpoint, so its
legacy messages cannot strand jobs in the current lab. Run the control in a
dedicated lab with no other clients; temporary fixture data is discarded when
the control ends. Verification is scoped to this
profile and does not establish compatibility with the HA or delivery profiles.

## Certificate renewal

| Target | Command |
|---|---|
| Ingress | `./tools/security-lab-local issue-certificate` |
| PostgreSQL | `./tools/security-lab-local issue-internal-certificate postgres` |
| RabbitMQ | `./tools/security-lab-local issue-internal-certificate rabbitmq` |
| Keycloak | `./tools/security-lab-local issue-keycloak-certificate` |
| API | `./tools/security-lab-local issue-app-certificate api` |
| Session gateway | `./tools/security-lab-local issue-app-certificate oauth2-proxy` |

Confirm the renewal command succeeds and new connections verify the new serial.
A file replacement alone is insufficient: existing file bind mounts may retain
the old inode, and established TLS connections do not re-verify certificates.
The API can reload its certificate; oauth2-proxy needs recreation. Follow the
wrapper's service-specific reload/restart behavior.

Renewal is manual. Initialization renews certificates close to expiry, and
preflight refuses expired files, but an already-running process may retain an
established connection. The API retains its last valid key pair when a new file
is invalid, even if that old pair later expires; restore a valid pair and check
new connections. There is no general CRL/OCSP revocation service.

## Database password rotation

```sh
./tools/security-lab-local rotate-app-db-password
```

This rotates only the API database credential. It requires marked private
state, a running target database and exclusive access to the operator workflow.
The tool journals the planned value before changing the server, updates derived
files, terminates old database sessions, recreates the consumer and verifies
readiness. On failure it retains the journal; rerunning continues that rotation
rather than restoring a compromised password. Do not delete unknown journal data.

See [the rotation decision](../adr/0011-security-lab-secret-distribution-rotation.md)
for file replacement and recovery requirements.

## Admission control

Ingress uses per-peer and aggregate API/authentication budgets. Excess requests
return 429 with Retry-After. Forwarding headers cannot select the budget. Clients
sharing a NAT or Docker forwarding address share a peer budget. These limits do
not bound storage growth, processing backlog, open sockets or distributed abuse.
See [the admission policy](../adr/0010-security-lab-rate-limiting.md).

## Stop and troubleshoot

```sh
./tools/security-lab-local stop
```

Stop preserves volumes and private state. Destructive removal is separate and
requires confirming the exact project and state paths first.

For startup failures, check secret-file existence/permissions, certificate chain,
name and expiry, dependency health and port 9443. Inspect safe diagnostics rather
than printing passwords, tokens or authorization query strings. Do not reset a
database, delete private state or disable TLS verification as a troubleshooting shortcut.

## Limits

The profile runs on one host with private local CAs and manual secret/certificate
lifecycles. Some internal connections remain plaintext. It does not establish
internet-ingress safety, independent-host resilience, complete credential
rotation or production identity operations. Other profiles require explicit
integration of names, trust boundaries, queue types and telemetry.
