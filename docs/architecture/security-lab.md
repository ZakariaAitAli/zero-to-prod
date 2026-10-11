# Security lab architecture

The security lab is an opt-in, local-first environment for Issue #121
(identity, secrets, TLS, and ingress boundary). It runs the unchanged Work
Items reference workload behind one HTTPS entry point with private backends.

**Status:** intermediate. Ingress, network isolation, a private lab CA,
file-based runtime secrets, and authentication (Keycloak, oauth2-proxy with a
Valkey session store, and token validation in the API;
[ADR 0004](../adr/0004-security-lab-authentication.md), Proposed) exist.
Authorization, datastore TLS, rate limiting, and secret rotation do not. The
lab must stay on host loopback.

Operating procedures are in the [security lab guide](../guides/local-security-lab.md);
measurements are in the [first-slice experiment record](../experiments/issue-121-local-https-ingress.md),
the [authentication comparison](../experiments/issue-121-authentication-comparison.md),
and the [authentication integration record](../experiments/issue-121-authentication-integration.md).

## Structure

```text
host loopback 127.0.0.1:9443
  │  HTTPS (lab CA, work-items.localhost, auth.work-items.localhost)
  ▼
edge network
  ingress — nginx, UID 101, read-only: TLS, static Web UI, API allow-list,
            auth_request, CSRF checks, identity-provider routes
  │  HTTP
  ├──────────────────────────────┐
  ▼                              ▼
app network (internal)         idp network (internal)
  api — Work Items API,          keycloak ── idp-data network ── keycloak-postgres
        validates tokens
  oauth2-proxy ── session network (internal) ── valkey
  │
  ▼
data network (internal)
  api, worker (user app, read-only), PostgreSQL, RabbitMQ,
  migrate (on demand)
```

| Component | Image | Networks | Published | Runtime inputs |
| --- | --- | --- | --- | --- |
| ingress | `infra/security-lab/ingress/Dockerfile` (nginx 1.30.5 with the built Web UI) | edge, app (aliases for both lab names), idp | `127.0.0.1:9443` → 8443 | Directory mount `ingress-tls/` (key and certificate in `tls.pem`) |
| api | `apps/work-items/api/Dockerfile` | app, data | none | `DATABASE_URL_FILE`, `RABBITMQ_PUBLISHER_URL_FILE`, `AUTHENTICATION=oidc`, `OIDC_*` settings, lab CA certificate |
| keycloak | Keycloak 26.8.0 (digest-pinned), production mode | idp, idp-data | none | Generated `keycloak.conf` and realm file |
| keycloak-postgres | PostgreSQL 18.6 (digest-pinned) | idp-data | none | Database password file |
| oauth2-proxy | oauth2-proxy 7.15.5 (digest-pinned), read-only | app, session | none | Generated configuration file; lab CA certificate |
| valkey | Valkey 9.1.2 (digest-pinned), no persistence | session | none | Generated configuration file |
| worker | `apps/work-items/worker/Dockerfile` | data | none | `DATABASE_URL_FILE`, `RABBITMQ_WORKER_URL_FILE` |
| postgres | PostgreSQL 18.6 (digest-pinned) | data | none | `POSTGRES_PASSWORD_FILE` and the migrator, app, and worker password files |
| rabbitmq | RabbitMQ 4.3.6 (digest-pinned), plugins disabled | data | none | Generated definitions file loaded at boot |
| migrate | `tools/migrate/Dockerfile` | data | none | Migrator connection-URL file |

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
| Ingress → API | edge/app | None | Access token in `Authorization`, validated by the API (signature, algorithm, issuer, audience, expiry) |
| API, oauth2-proxy → issuer | app, through the ingress | TLS 1.2/1.3 | Lab CA certificate file |
| Ingress → oauth2-proxy, ingress → Keycloak | app, idp | None | None |
| oauth2-proxy → Valkey | session | None | Valkey password |
| Keycloak → keycloak-postgres | idp-data | None | Database password |
| API, worker, migrate → PostgreSQL | data | None (`sslmode=disable`) | Per-identity passwords |
| API publisher, worker → RabbitMQ | data | None | Per-identity passwords and vhost permissions |
| Operator → containers | Docker socket | — | Docker access is root-equivalent and outside the model |

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
| Machine client | `work-items-automation`: confidential client, client credentials only |
| Lab user | `lab-user`; no administrative user exists |

Both clients add the `work-items-api` audience to their access tokens.

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

There is no revocation (CRL or OCSP) and no public-CA issuance. Clients trust
the lab CA only where it is explicitly configured: `--cacert` for the
automated checks, or an operator-managed browser trust store.

## Lifecycle

| Operation | Effect |
| --- | --- |
| `init` | Create or complete the state directory, CA, certificate, and secrets |
| `start` | Check secret files, build images, recreate containers, apply identities and migrations, wait for health |
| `verify`, `verify-failures`, `verify-renewal` | Reachability, missing-secret, and renewal checks with optional JSON evidence |
| `verify-authentication` | Sign-in, both API paths, token rejection, CSRF, sign-out, replay, and dependency outages; stops and restarts services |
| `verify-log-hygiene` | Search ingress and gateway logs for the codes, state, cookie, tokens, and secrets of a sign-in and failed callbacks, with positive controls |
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

- Authorization and rate limiting.
- Revocation of access tokens before expiry (validation is local).
- Encryption of internal hops.
- Access from other containers on the same Docker engine: in the measured
  environment the published ingress port was reachable from them.
- Secret rotation, revocation, CSP, and HSTS.
- Production deployment, public certificates, and multi-host networking.
