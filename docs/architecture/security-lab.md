# Security lab architecture

The security lab is an opt-in, local-first environment for Issue #121
(identity, secrets, TLS, and ingress boundary). It runs the unchanged Work
Items reference workload behind one HTTPS entry point with private backends.

**Status:** intermediate. Ingress, network isolation, a private lab CA,
file-based runtime secrets, and authentication (Keycloak, oauth2-proxy with a
Valkey session store, and token validation in the API;
[ADR 0004](../adr/0004-security-lab-authentication.md), Proposed), and
role-based authorization in the API
([ADR 0005](../adr/0005-security-lab-authorization.md), Proposed), and
verified TLS to PostgreSQL and RabbitMQ on the data network from a separate
internal CA ([ADR 0006](../adr/0006-security-lab-internal-transport.md),
Proposed), and verified TLS from the ingress to Keycloak from a third,
identity-provider CA ([ADR 0007](../adr/0007-security-lab-identity-provider-transport.md),
Proposed) exist. The other internal hops stay plaintext by decision; rate
limiting and secret rotation do not exist. The lab must stay on host
loopback.

Operating procedures are in the [security lab guide](../guides/local-security-lab.md);
the transport threats are in the [threat model](security-lab-threat-model.md);
measurements are in the [first-slice experiment record](../experiments/issue-121-local-https-ingress.md),
the [authentication comparison](../experiments/issue-121-authentication-comparison.md),
the [authentication integration record](../experiments/issue-121-authentication-integration.md),
the [authorization record](../experiments/issue-121-authorization.md), the
[internal TLS record](../experiments/issue-121-internal-tls.md), and the
[Keycloak TLS record](../experiments/issue-121-keycloak-tls.md).

## Structure

```text
host loopback 127.0.0.1:9443
  │  HTTPS (lab CA, work-items.localhost, auth.work-items.localhost)
  ▼
edge network
  ingress — nginx, UID 101, read-only: TLS, static Web UI, API allow-list,
            auth_request, CSRF checks, identity-provider routes
  │  HTTP                        │  HTTPS, Keycloak verified (identity-provider CA)
  ├──────────────────────────────┐
  ▼                              ▼
app network (internal)         idp network (internal)
  api — Work Items API,          keycloak :8443 (HTTPS only) ── idp-data network ── keycloak-postgres
        validates tokens
  oauth2-proxy ── session network (internal) ── valkey
  │
  ▼
data network (internal)            TLS only, server verified (internal CA)
  api, worker, migrate (on demand) ──► PostgreSQL :5432 (hostssl only)
  api, worker                      ──► RabbitMQ   :5671 (no plaintext listener)
```

| Component | Image | Networks | Published | Runtime inputs |
| --- | --- | --- | --- | --- |
| ingress | `infra/security-lab/ingress/Dockerfile` (nginx 1.30.5 with the built Web UI) | edge, app (aliases for both lab names), idp | `127.0.0.1:9443` → 8443 | Directory mount `ingress-tls/` (key and certificate in `tls.pem`); identity-provider CA certificate |
| api | `apps/work-items/api/Dockerfile` | app, data | none | `DATABASE_URL_FILE`, `RABBITMQ_PUBLISHER_URL_FILE`, `AUTHENTICATION=oidc`, `OIDC_*` settings, lab CA and internal CA certificates |
| keycloak | Keycloak 26.8.0 (digest-pinned), production mode; HTTPS only on 8443, management (health) on its loopback | idp, idp-data | none | Generated `keycloak.conf` and realm file; directory mount `idp-tls/` (key and certificate in `tls.pem`, reread every 35 s) |
| keycloak-postgres | PostgreSQL 18.6 (digest-pinned) | idp-data | none | Database password file |
| oauth2-proxy | oauth2-proxy 7.15.5 (digest-pinned), read-only | app, session | none | Generated configuration file; lab CA certificate |
| valkey | Valkey 9.1.2 (digest-pinned), no persistence | session | none | Generated configuration file |
| worker | `apps/work-items/worker/Dockerfile` | data | none | `DATABASE_URL_FILE`, `RABBITMQ_WORKER_URL_FILE`, internal CA certificate |
| postgres | PostgreSQL 18.6 (digest-pinned); `ssl=on`, repository `pg_hba.conf` | data | none | `POSTGRES_PASSWORD_FILE` and the migrator, app, and worker password files; directory mount `internal-tls/postgres/` (copied to a tmpfs by `postgres/tls.sh`) |
| rabbitmq | RabbitMQ 4.3.6 (digest-pinned), plugins disabled, TLS listener only | data | none | Generated definitions file loaded at boot; directory mount `internal-tls/rabbitmq/` |
| migrate | `tools/migrate/Dockerfile` | data | none | Migrator connection-URL file; internal CA certificate |

All networks but `edge` are Docker `internal` networks. `edge` is the only
network with external connectivity, which Docker requires for port
publication. The
Compose project is `zero-to-prod-security-lab`, separate from the development
lab's projects and volumes.

## Trust boundaries and connections

| Connection | Boundary | Encryption | Authentication / verification |
| --- | --- | --- | --- |
| Client → ingress | Host loopback into the lab | TLS 1.2/1.3 | Server certificate from the lab CA for both lab names; other names and IP addresses fail the handshake. No TLS client authentication |
| Browser → API (session path) | edge/app | TLS to the ingress | Session ticket cookie checked by oauth2-proxy (`auth_request`); nginx forwards the session's access token and drops the cookie; CSRF checks on state changes |
| Machine client → API (bearer path) | edge/app | TLS to the ingress | Client-credentials access token, passed unchanged |
| Ingress → API | edge/app | None | Access token in `Authorization`, validated by the API (signature, algorithm, issuer, audience, expiry); then the route's permission from `resource_access["work-items-api"].roles` (403 without it) |
| API, oauth2-proxy → issuer | app, through the ingress | TLS 1.2/1.3 | Lab CA certificate file |
| Ingress → oauth2-proxy | app | None (ADR 0006: provisional) | None |
| Ingress → Keycloak | idp | TLS 1.2/1.3 (ADR 0007); Keycloak has no plaintext listener | Keycloak's certificate and the name `keycloak` verified against the identity-provider CA (`proxy_ssl_verify on`, no session reuse, no kept-alive upstream connections). No TLS client-certificate (mTLS) authentication; oauth2-proxy and machine clients authenticate to Keycloak with client credentials inside TLS. The public issuer URL, forwarded headers, and Keycloak's `hostname` are unchanged |
| Lab tool admin probe → Keycloak admin API | idp (while a check runs) | TLS 1.2/1.3 (ADR 0007) | As for the ingress; role-management client credentials inside TLS |
| oauth2-proxy → Valkey | session | None (ADR 0006: two-member network) | Valkey password |
| Keycloak → keycloak-postgres | idp-data | None (ADR 0006: two-member network) | Database password |
| API, worker, migrate → PostgreSQL | data | TLS 1.2/1.3 (1.3 observed); non-TLS clients rejected by `pg_hba.conf` | Server certificate and name `postgres` verified against the internal CA (`sslmode=verify-full`); clients by per-identity passwords (SCRAM) |
| API publisher, worker → RabbitMQ | data | TLS 1.2/1.3 (1.3 observed) on 5671; no plaintext listener | Server certificate and name `rabbitmq` verified against the internal CA (`amqps`, `cacertfile`); clients by per-identity passwords (`PLAIN` inside TLS) and vhost permissions |
| Operator → containers | Docker socket | — | Docker access is root-equivalent and outside the model |

The [threat model](security-lab-threat-model.md) explains, per network, why
these hops are encrypted and the others are not yet.

The ingress routes only `/api/items`, `/api/items/{id}/process`, and
`/api/processing-jobs/{id}`; every other `/api` path, including the API's
health, readiness, and version endpoints, returns 404. It replaces client
`X-Forwarded-*`, `Forwarded`, and `X-Real-IP` headers. The API does not use
those headers. Its access log records paths without query strings and no
headers or cookies, and keeps status, upstream status and address, upstream
connect time, request time, and request IDs. In its public servers,
error-log entries below `crit` from request processing are suppressed,
because nginx's error log writes the full request line. For
Keycloak it routes only `/realms/work-items/` and
`/resources/`; for oauth2-proxy only `/oauth2/start`, `/oauth2/callback`,
`/oauth2/userinfo`, and POST `/oauth2/sign_out`.

## Identities and secrets

Work Items keeps its development role and user names, because migrations grant
privileges to fixed names. Only the credentials differ.

| Principal | PostgreSQL | RabbitMQ |
| --- | --- | --- |
| API (including the outbox publisher) | `zero_to_prod_app` | `zero_to_prod_publisher`: write to the default exchange only |
| worker | `zero_to_prod_worker` | `zero_to_prod_worker`: read the processing queue only |
| migrate | `zero_to_prod_migrator` | — |
| bootstrap | `zero_to_prod_admin` (container initialization and identity re-application) | none; no default or administrative user is defined |

| Principal | Keycloak (`work-items` realm) |
| --- | --- |
| Browser gateway | `work-items-gateway`: confidential client, authorization code with PKCE S256 |
| Machine client | `work-items-automation`: confidential client, client credentials only; `items.read`, `items.process` |
| Permission roles | `work-items-api`: no flows; client roles `items.read`, `items.create`, `items.process`, composites `editor` and `viewer` |
| People | `lab-user` (`editor`), `lab-viewer` (`viewer`), `lab-outsider` (no Work Items role; permission names only as a realm role and a gateway-client role) |
| Role management | `work-items-lab-admin`: service account with `realm-management` roles `view-clients` and `manage-users`, used only by `verify-realm` and `verify-authorization`; admin API reachable only from the `idp` network |

The gateway and machine clients add the `work-items-api` audience to their
access tokens. The API enforces the route permissions; the edge does not
authorize.

`tools/security-lab-local init` generates random passwords and derives the
connection URLs and RabbitMQ definitions from them in a state directory
outside the repository (by default
`~/.local/state/zero-to-prod/zero-to-prod-security-lab/`). Each service
receives only its own files, as read-only bind mounts. The files are not on
tmpfs, not encrypted at rest, and not managed by a secret manager; host
protection is the mode-700 state directory. Container environments contain
only `*_FILE` paths.

The state directory carries an ownership marker and must resolve below the XDG
state directory or the temporary directory, outside the repository and not
containing the home directory. Destructive cleanup requires that marker.

## Certificate lifecycle

| Item | Owner | Lifetime | Renewal |
| --- | --- | --- | --- |
| Lab CA | Lab operator (`init`) | 90 days; name-constrained to `work-items.localhost`; may not issue intermediates | Not automated; a new CA requires `purge-state` and `init` |
| Ingress certificate | Lab operator (`issue-certificate`, or `init` near expiry) | 14 days | Replaces `ingress-tls/tls.pem` by rename, reloads nginx, and waits for consecutive new connections with the new serial |
| Internal CA | Lab operator (`init`) | 90 days; name constraints permit exactly `postgres` and `rabbitmq` (`DNS:postgres` and `DNS:rabbitmq` permitted, `DNS:.postgres` and `DNS:.rabbitmq` excluded); may not issue intermediates | Not automated. A new CA requires `purge-state` and `init`, then `start`: pgx reads the CA when its pool is created, so the API and worker must restart. `init` also replaces an internal CA whose constraints still permit subdomains |
| PostgreSQL and RabbitMQ certificates | Lab operator (`issue-internal-certificate <service>`, or `init` near expiry or when the name does not verify) | 14 days | Replaces `internal-tls/<service>/tls.pem` by rename; PostgreSQL: copy to its tmpfs and `pg_ctl reload`; RabbitMQ: `ssl:clear_pem_cache()`. Succeeds only after five consecutive verified new connections with the new serial, within 60 attempts; otherwise it fails. Established connections keep their session |
| Identity-provider CA | Lab operator (`init`) | 90 days; name constraints permit exactly `keycloak` (`DNS:keycloak` permitted, `DNS:.keycloak` and IP addresses excluded); may not issue intermediates; trusted only by the ingress and the tool's probes | Not automated. A new CA requires `purge-state` and `init`, then `start` (the ingress mounts it as a single file) |
| Keycloak certificate | Lab operator (`issue-keycloak-certificate`, or `init` near expiry or when the name does not verify) | 14 days | Replaces `idp-tls/tls.pem` by rename; Keycloak rereads it every 35 s, so neither Keycloak nor the ingress restarts. Succeeds only after five consecutive verified new connections with the new serial, within 100 attempts a second apart |

There is no revocation (CRL or OCSP) and no public-CA issuance. Clients trust
the lab CA only where it is explicitly configured: `--cacert` for the
automated checks, or an operator-managed browser trust store. The internal CA
is mounted only into the API, worker, and migrator, and the identity-provider
CA only into the ingress; neither is ever needed by a browser.

## Lifecycle

| Operation | Effect |
| --- | --- |
| `init` | Create or complete the state directory, CA, certificate, and secrets |
| `start` | Check secret files, build images, recreate containers, apply identities and migrations, wait for health |
| `verify`, `verify-failures`, `verify-renewal` | Reachability, missing-secret, and renewal checks with optional JSON evidence |
| `verify-authentication` | Sign-in, both API paths, token rejection, CSRF, sign-out, replay, and dependency outages; stops and restarts services |
| `verify-log-hygiene` | Search ingress and gateway logs for the codes, state, cookies, tokens, and secrets of a sign-in, failed callbacks, and a session refresh, with positive controls |
| `verify-realm`, `verify-authorization` | Realm readiness through the admin API; the permission matrix, claims, ordering, logs, role removal, outage, and a control against the pinned pre-authorization API |
| `verify-admin-permissions` | Minimum role set of the role-management client, in a throwaway `*-probe` project |
| `issue-internal-certificate`, `verify-internal-tls` | Renew a PostgreSQL or RabbitMQ certificate; exercise plaintext refusal, untrusted issuer, hostname mismatch, expiry, recovery, and renewal; restarts the API and worker |
| `issue-keycloak-certificate`, `check-keycloak-upstream`, `capture-idp-hop`, `verify-keycloak-tls` | Renew Keycloak's certificate; show nginx's TLS verdict for Keycloak; capture the ingress → Keycloak hop passively (counts only); exercise the hop's baseline, failure cases, session reuse, established connections, recovery, and renewal without restarts |
| `stop` | Stop containers, keep volumes and state |
| `destroy` | Remove this project's containers, networks, and volumes |
| `purge-state --yes` | Delete the marked state directory |

## Relationship to Work Items

Work Items behavior is unchanged: the security lab adds the `*_FILE` settings,
a worker image, and authentication in front of every Work Items route, but no
new API behavior, schema, or processing semantics. The development lab runs
the API with `AUTHENTICATION=disabled`.
ADR 0003 state rules and the open restore limitations
[#130](https://github.com/ZakariaAitAli/zero-to-prod/issues/130) and
[#131](https://github.com/ZakariaAitAli/zero-to-prod/issues/131) apply here as
in the development lab. See the [Work Items architecture](work-items.md).

## Not covered

- Per-item or ownership authorization, and rate limiting.
- Revocation of access tokens before expiry (validation is local).
- Encryption of the ingress → API and oauth2-proxy hops, oauth2-proxy →
  Valkey, and Keycloak → its database (ADR 0006).
- Client certificates (mTLS), certificate revocation, automated renewal, and
  expiry monitoring. Any member of `idp` can still reach every Keycloak path
  over TLS, and Keycloak's cache-cluster listeners remain on `idp`.
- Access from other containers on the same Docker engine: in the measured
  environment the published ingress port was reachable from them.
- Secret rotation, revocation, CSP, and HSTS.
- Production deployment, public certificates, and multi-host networking.
