# Security lab threat model: internal transport

Scope: the connections inside the local security lab of Issue #121, as built
after the authorization slice (#135), and what changes when the transport
decisions of [ADR 0006](../adr/0006-security-lab-internal-transport.md)
(data network) and [ADR 0007](../adr/0007-security-lab-identity-provider-transport.md)
(ingress → Keycloak) are applied. It is bounded to transport: who can observe
or alter traffic between the lab's components, and what a certificate failure
does. Authentication and authorization threats are in [ADR 0004](../adr/0004-security-lab-authentication.md)
and [ADR 0005](../adr/0005-security-lab-authorization.md); rate limiting,
secret rotation, and availability are later work.

The lab runs on one Docker engine (Docker Desktop, WSL 2). Statements about
network membership and capabilities below were measured by
`verify-internal-tls` (network inventory in
[`verify-internal-tls.json`](../../evidence/issue-121/internal-tls/verify-internal-tls.json));
Keycloak's listeners and the content of the ingress → Keycloak hop were
measured by `capture-idp-hop` and `verify-keycloak-tls`
([evidence](../../evidence/issue-121/keycloak-tls/)).

## Assets

| Asset | Where it lives or travels |
| --- | --- |
| Work Items data and results | PostgreSQL; API and worker queries |
| Processing messages | RabbitMQ; API publisher, worker consumer |
| PostgreSQL passwords (migrator, app, worker) | Connection-URL files; SCRAM exchange (the password itself does not cross the wire) |
| RabbitMQ passwords (publisher, worker) | Connection-URL files; AMQP `PLAIN` sends the password itself |
| Person passwords | Browser → ingress (TLS) → Keycloak login form |
| Client secrets (gateway, automation) | Token requests → ingress (TLS) → Keycloak |
| Lab role-management client secret and admin tokens | Lab tool's admin probe on `idp` → Keycloak |
| Access tokens, ID tokens, authorization codes | Ingress ↔ API, ingress ↔ oauth2-proxy, ingress ↔ Keycloak |
| Session tickets (`__Host-wi-session`) and sessions | Ingress → oauth2-proxy; oauth2-proxy ↔ Valkey (stored encrypted with the ticket) |
| Keycloak realm data (client secrets, password hashes, signing keys) | Keycloak ↔ its PostgreSQL |
| CA keys (lab CA, internal CA, identity-provider CA) | Host state directory, mode 600; never mounted |
| Server private keys (ingress, PostgreSQL, RabbitMQ, Keycloak) | Host state directory; mounted read-only into their own service |

## Principals

- **People**: `lab-user`, `lab-viewer`, `lab-outsider` (browser sessions).
- **Machine client**: `work-items-automation` (bearer tokens).
- **Workloads**: ingress, API, worker, migrator, oauth2-proxy, Keycloak, and
  the datastores. Each holds only its own secret files.
- **Lab operator**: runs `tools/security-lab-local`, owns the state directory
  and the three CAs. Docker access is root-equivalent and outside the model:
  such a principal can read every key and secret regardless of transport.
- **Lab tool probes**: short-lived containers the tool attaches to internal
  networks while a check runs (reachability, TLS probes, and the admin-API
  probe on `idp`, which holds the role-management client secret).

## Trust boundaries and connections

Members are the containers attached to the network the connection uses.
"Raw sockets" means `CAP_NET_RAW` in the container's bounding set (needed to
capture or forge traffic, for example by ARP spoofing on the bridge); a
process gains it by running as root in that container.

| Connection | Network and members | What crosses | Before ADR 0006 | Now (ADR 0006, ADR 0007) |
| --- | --- | --- | --- | --- |
| Client → ingress | host loopback → `edge` | Everything a client sends | TLS (lab CA) | Unchanged |
| API, oauth2-proxy → issuer | `app`, through the ingress | Discovery, keys, token requests with client secrets | TLS (lab CA) | Unchanged |
| Ingress → API | `app`: ingress, API, oauth2-proxy; all `cap_drop: ALL`, `no-new-privileges`, read-only | Access tokens, Work Items data | Plaintext | Plaintext (ADR 0006) |
| Ingress → oauth2-proxy | `app` (as above) | Session tickets, authorization codes, returned access tokens | Plaintext | Plaintext |
| Ingress → Keycloak | `idp`: ingress, Keycloak; **and the lab tool's probes while they run** (ADR 0006 assumed two members) | Person passwords, client secrets, codes, tokens (measured in plaintext before ADR 0007) | Plaintext | **TLS (ADR 0007): chain and name `keycloak` verified by the ingress against the identity-provider CA; Keycloak has no plaintext listener** |
| Lab tool admin probe → Keycloak admin API | `idp` (while `verify-realm` or `verify-authorization` runs) | Role-management client secret, admin tokens, role mappings | Plaintext | **TLS, verified like the ingress (ADR 0007)** |
| Keycloak management (health) | Keycloak's own loopback since ADR 0007; before, reachable on `idp` | Health status only | Plain HTTP on `idp` | Plain HTTP, `127.0.0.1` only |
| Keycloak cache cluster (JGroups `:7800` with mTLS, failure detection `:57800`) | Listens on Keycloak's `idp` address | Cluster membership and cache traffic (single node) | Present | Unchanged; observed, not analyzed |
| oauth2-proxy → Valkey | `session`: oauth2-proxy, Valkey (two members; Valkey's PID 1 runs as root with raw sockets) | Valkey password, encrypted sessions | Plaintext | Plaintext |
| Keycloak → its PostgreSQL | `idp-data`: Keycloak, its PostgreSQL (two members) | Realm data incl. client secrets; SCRAM | Plaintext | Plaintext |
| API, worker, migrator → PostgreSQL | `data`: API, worker (`cap_drop: ALL`), PostgreSQL, RabbitMQ (raw sockets in bounding set, no `no-new-privileges`), migrator on demand (image default user, no `cap_drop`) | Work Items data; SCRAM | Plaintext (`sslmode=disable`) | **TLS 1.3, server verified (`verify-full`), plaintext rejected by the server** |
| API, worker → RabbitMQ | `data` (as above) | Messages; **AMQP `PLAIN` passwords** | Plaintext | **TLS 1.3, server verified, plaintext listener disabled** |
| Health checks, initialization | container loopback / Unix socket | — | Local | Unchanged |

## Threats

| ID | Threat | Applies to | Realistic here because |
| --- | --- | --- | --- |
| T1 | Passive capture by another member of the same network | Networks with more than two members, where some member can gain raw sockets | `data` has five possible members; PostgreSQL, RabbitMQ, and the migrator keep `CAP_NET_RAW` in their bounding sets and lack `no-new-privileges`. `idp` gains the tool's probes while checks run, and Keycloak keeps `CAP_NET_RAW` in its bounding set; `capture-idp-hop` demonstrates the capture from Keycloak's network namespace |
| T2 | Impersonation of a backend (answering as `postgres` or `rabbitmq`) to harvest credentials or alter results | Same as T1; also any container wrongly attached to the network | A spoofed RabbitMQ receives AMQP `PLAIN` passwords; a spoofed PostgreSQL can return forged rows |
| T3 | A container attached to a network by mistake | All internal networks | Compose changes are reviewed but not policy-checked |
| T4 | Traffic crossing hosts (multi-host networking, managed datastores) | All hops if the lab moves off one engine | Not the case today; datastores are the most likely to move first |
| T5 | Certificate failure: expiry, wrong name, untrusted issuer | Every verified TLS hop | Certificates last 14 days; renewal is manual |
| T6 | Downgrade: a client accepting plaintext or an unverified server | Every TLS hop | `sslmode=prefer` or `require` would encrypt without verifying, or fall back |
| T7 | Theft of the internal CA key or the identity-provider CA key | Internal TLS | Keys are files in the operator's state directory |
| T8 | Host or Docker-engine compromise | Everything | Out of model: such an attacker reads keys and secrets directly |

## Mitigations and what remains

| Threat | Now (ADR 0006, ADR 0007) | Remaining |
| --- | --- | --- |
| T1, T2 on `data` | TLS. Each client verifies the server's certificate against the internal CA and checks the one name it dialed (`postgres` or `rabbitmq`). The CA's constraints allow exactly those two names (subdomains excluded). A spoofing member has no certificate for the name dialed | Client identity is still a password, readable by any process in the client's container |
| T1, T2 on `idp` (ingress → Keycloak, admin probe → Keycloak) | TLS (ADR 0007). The ingress and the admin probe verify Keycloak's certificate against the identity-provider CA and check the name `keycloak`; the CA may issue for exactly that name. Keycloak has no plaintext listener; its health interface is bound to its loopback. A passive capture during a sign-in and token request found no credential and no plaintext HTTP (it found the password, code, client secrets, and a token before) | No TLS client-certificate (mTLS) authentication (OAuth clients authenticate with client credentials inside TLS): any member of `idp` can open a TLS connection to Keycloak and reach every path, including the admin API (as the tool's probe does). Keycloak's cluster listeners (`:7800` mTLS, `:57800`) remain on `idp` |
| T1, T2 on `app` | Not encrypted. Every member is `cap_drop: ALL`, `no-new-privileges`, read-only, so raw sockets require a kernel escape | Tokens and session tickets in plaintext on the bridge |
| T1, T2 on two-member networks (`idp-data`, `session`) | Not encrypted. The only other party on each network is the peer itself | Realm data (Keycloak → PostgreSQL) and the Valkey password and encrypted sessions in plaintext |
| T3 | `data` and `idp` are protected by TLS; other networks are not | No policy check of network membership |
| T4 | `data` and ingress → Keycloak already verified | Ingress → API and oauth2-proxy, oauth2-proxy → Valkey, and Keycloak → its database must be encrypted before any move |
| T5 | Failure is closed and observable (measured on both decisions); renewal restores new connections without restarting clients, Keycloak, or the ingress. At the ingress, the reason is logged only by a loopback diagnostic route, because public request errors are suppressed below `crit` | No automated renewal, alerting, or revocation. An established connection is not re-verified (measured with a kept-alive control); the production ingress keeps none to Keycloak |
| T6 | `verify-full` and `amqps` in the generated URLs; `check-secrets` refuses any other form; PostgreSQL rejects non-TLS clients and RabbitMQ has no plaintext listener. Keycloak: `http-enabled=false`, `check-secrets` refuses a configuration that enables HTTP, and the ingress verifies with `proxy_ssl_verify on` and no session reuse | — |
| T7 | Name constraints limit certificates from a stolen internal CA key to exactly `postgres` and `rabbitmq`, and from a stolen identity-provider CA key to exactly `keycloak`: permitted names, with their subdomains and all IP addresses excluded. Enforced as intended by OpenSSL (including nginx) and Go's `crypto/x509`, the verifiers used here; for the identity-provider CA, a `keycloak` certificate that also names a subdomain or an IP address was refused by nginx, while an unconstrained CA's equivalent was accepted (control). Each CA is trusted only by the clients of its hop and is separate from the browser-trusted lab CA | Each key can still impersonate its own services. No HSM, no revocation, single tier. Another verifier may read the leading-dot exclusion differently (RFC 5280 leaves it undefined) |
| T8 | Out of scope | — |

The plaintext hops are a provisional, measured decision, not an oversight;
the reconsideration conditions are in ADR 0006 and ADR 0007.
