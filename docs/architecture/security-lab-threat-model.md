# Security lab threat model: internal transport

Scope: the connections inside the local security lab of Issue #121, as built
after the authorization slice (#135), and what changes when the transport
decision of [ADR 0006](../adr/0006-security-lab-internal-transport.md) is
applied. It is bounded to transport: who can observe or alter traffic between
the lab's components, and what a certificate failure does. Authentication
and authorization threats are in [ADR 0004](../adr/0004-security-lab-authentication.md)
and [ADR 0005](../adr/0005-security-lab-authorization.md); rate limiting,
secret rotation, and availability are later work.

The lab runs on one Docker engine (Docker Desktop, WSL 2). Statements about
network membership and capabilities below were measured by
`verify-internal-tls` (network inventory in
[`verify-internal-tls.json`](../../evidence/issue-121/internal-tls/verify-internal-tls.json)).

## Assets

| Asset | Where it lives or travels |
| --- | --- |
| Work Items data and results | PostgreSQL; API and worker queries |
| Processing messages | RabbitMQ; API publisher, worker consumer |
| PostgreSQL passwords (migrator, app, worker) | Connection-URL files; SCRAM exchange (the password itself does not cross the wire) |
| RabbitMQ passwords (publisher, worker) | Connection-URL files; AMQP `PLAIN` sends the password itself |
| Person passwords | Browser → ingress (TLS) → Keycloak login form |
| Client secrets (gateway, automation, lab admin) | Token requests → ingress (TLS) → Keycloak |
| Access tokens, ID tokens, authorization codes | Ingress ↔ API, ingress ↔ oauth2-proxy, ingress ↔ Keycloak |
| Session tickets (`__Host-wi-session`) and sessions | Ingress → oauth2-proxy; oauth2-proxy ↔ Valkey (stored encrypted with the ticket) |
| Keycloak realm data (client secrets, password hashes, signing keys) | Keycloak ↔ its PostgreSQL |
| CA keys (lab CA, internal CA) | Host state directory, mode 600; never mounted |
| Server private keys (ingress, PostgreSQL, RabbitMQ) | Host state directory; mounted read-only into their own service |

## Principals

- **People**: `lab-user`, `lab-viewer`, `lab-outsider` (browser sessions).
- **Machine client**: `work-items-automation` (bearer tokens).
- **Workloads**: ingress, API, worker, migrator, oauth2-proxy, Keycloak, and
  the datastores. Each holds only its own secret files.
- **Lab operator**: runs `tools/security-lab-local`, owns the state directory
  and both CAs. Docker access is root-equivalent and outside the model: such
  a principal can read every key and secret regardless of transport.

## Trust boundaries and connections

Members are the containers attached to the network the connection uses.
"Raw sockets" means `CAP_NET_RAW` in the container's bounding set (needed to
capture or forge traffic, for example by ARP spoofing on the bridge); a
process gains it by running as root in that container.

| Connection | Network and members | What crosses | Before ADR 0006 | After ADR 0006 |
| --- | --- | --- | --- | --- |
| Client → ingress | host loopback → `edge` | Everything a client sends | TLS (lab CA) | Unchanged |
| API, oauth2-proxy → issuer | `app`, through the ingress | Discovery, keys, token requests with client secrets | TLS (lab CA) | Unchanged |
| Ingress → API | `app`: ingress, API, oauth2-proxy; all `cap_drop: ALL`, `no-new-privileges`, read-only | Access tokens, Work Items data | Plaintext | Plaintext (decision below) |
| Ingress → oauth2-proxy | `app` (as above) | Session tickets, authorization codes, returned access tokens | Plaintext | Plaintext |
| Ingress → Keycloak | `idp`: ingress, Keycloak (two members) | Person passwords, client secrets, codes, tokens | Plaintext | Plaintext |
| oauth2-proxy → Valkey | `session`: oauth2-proxy, Valkey (two members; Valkey's PID 1 runs as root with raw sockets) | Valkey password, encrypted sessions | Plaintext | Plaintext |
| Keycloak → its PostgreSQL | `idp-data`: Keycloak, its PostgreSQL (two members) | Realm data incl. client secrets; SCRAM | Plaintext | Plaintext |
| API, worker, migrator → PostgreSQL | `data`: API, worker (`cap_drop: ALL`), PostgreSQL, RabbitMQ (raw sockets in bounding set, no `no-new-privileges`), migrator on demand (image default user, no `cap_drop`) | Work Items data; SCRAM | Plaintext (`sslmode=disable`) | **TLS 1.3, server verified (`verify-full`), plaintext rejected by the server** |
| API, worker → RabbitMQ | `data` (as above) | Messages; **AMQP `PLAIN` passwords** | Plaintext | **TLS 1.3, server verified, plaintext listener disabled** |
| Health checks, initialization | container loopback / Unix socket | — | Local | Unchanged |

## Threats

| ID | Threat | Applies to | Realistic here because |
| --- | --- | --- | --- |
| T1 | Passive capture by another member of the same network | Networks with more than two members, where some member can gain raw sockets | `data` has five possible members; PostgreSQL, RabbitMQ, and the migrator keep `CAP_NET_RAW` in their bounding sets and lack `no-new-privileges` |
| T2 | Impersonation of a backend (answering as `postgres` or `rabbitmq`) to harvest credentials or alter results | Same as T1; also any container wrongly attached to the network | A spoofed RabbitMQ receives AMQP `PLAIN` passwords; a spoofed PostgreSQL can return forged rows |
| T3 | A container attached to a network by mistake | All internal networks | Compose changes are reviewed but not policy-checked |
| T4 | Traffic crossing hosts (multi-host networking, managed datastores) | All hops if the lab moves off one engine | Not the case today; datastores are the most likely to move first |
| T5 | Certificate failure: expiry, wrong name, untrusted issuer | Every verified TLS hop | Certificates last 14 days; renewal is manual |
| T6 | Downgrade: a client accepting plaintext or an unverified server | Every TLS hop | `sslmode=prefer` or `require` would encrypt without verifying, or fall back |
| T7 | Theft of the internal CA key | Internal TLS | Key is a file in the operator's state directory |
| T8 | Host or Docker-engine compromise | Everything | Out of model: such an attacker reads keys and secrets directly |

## Mitigations and what remains

| Threat | After ADR 0006 | Remaining |
| --- | --- | --- |
| T1, T2 on `data` | TLS. Each client verifies the server's certificate against the internal CA and checks the one name it dialed (`postgres` or `rabbitmq`). The CA's constraints allow exactly those two names (subdomains excluded). A spoofing member has no certificate for the name dialed | Client identity is still a password, readable by any process in the client's container |
| T1, T2 on `app` | Not encrypted. Every member is `cap_drop: ALL`, `no-new-privileges`, read-only, so raw sockets require a kernel escape | Tokens and session tickets in plaintext on the bridge |
| T1, T2 on two-member networks (`idp`, `idp-data`, `session`) | Not encrypted. The only other party on each network is the peer itself | Person passwords and client secrets in plaintext between ingress and Keycloak |
| T3 | `data` is protected by TLS; other networks are not | No policy check of network membership |
| T4 | `data` hops already verified | Every other hop must be encrypted before any move |
| T5 | Failure is closed and observable (measured); renewal restores new connections without restarting clients | No automated renewal, alerting, or revocation |
| T6 | `verify-full` and `amqps` in the generated URLs; `check-secrets` refuses any other form; PostgreSQL rejects non-TLS clients and RabbitMQ has no plaintext listener | — |
| T7 | Name constraints limit certificates from a stolen internal CA key to exactly `postgres` and `rabbitmq`: permitted names, with their subdomains excluded. Enforced as intended by OpenSSL and Go's `crypto/x509`, the verifiers used here. The CA is separate from the browser-trusted lab CA | The key can still impersonate both datastores. No HSM, no revocation, single tier. Another verifier may read the leading-dot exclusion differently (RFC 5280 leaves it undefined) |
| T8 | Out of scope | — |

The plaintext hops are a provisional, measured decision, not an oversight;
the reconsideration conditions are in ADR 0006.
