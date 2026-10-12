# Local security lab

The security lab is an opt-in, local-first environment for Issue #121
(identity, secrets, TLS, and ingress boundary). It runs Work Items behind a
single HTTPS ingress on host loopback, with private backend services and its
own runtime secrets.

**Status: intermediate.** The lab provides the ingress, private networks, a
private lab CA, file-based runtime secrets, and **authentication**: browsers
sign in through oauth2-proxy and Keycloak with server-side sessions, machine
clients use the client-credentials grant, and the API validates every access
token itself ([ADR 0004](../adr/0004-security-lab-authentication.md),
Proposed). The API also enforces **authorization**: per-route permissions
from Keycloak client roles, deny by default
([ADR 0005](../adr/0005-security-lab-authorization.md), Proposed). Datastore
TLS, rate limiting, and secret rotation are not implemented yet. It does not
complete Issue #121.

The development lab ([local Work Items guide](local-work-items.md)) is
unchanged and keeps its labelled development credentials. The security lab
uses separate Compose project names, networks, and volumes, and refuses to
start without its own secret files.

## Layout

```text
Windows browser / WSL client / machine client
        │ https://work-items.localhost:9443        (127.0.0.1 only; lab CA)
        │ https://auth.work-items.localhost:9443   (Keycloak, same ingress)
        ▼
  ingress (nginx) ── edge network
        │  serves the Web UI; forwards /api/items, /api/items/{id}/process,
        │  /api/processing-jobs/{id}; everything else under /api is 404
        │
        ├── app network (internal) ── API; oauth2-proxy (auth_request)
        │                               │
        │                               └── session network ── Valkey
        └── idp network (internal) ─── Keycloak ── idp-data network ── Keycloak PostgreSQL

  API ── data network (internal) ── PostgreSQL, RabbitMQ, worker
```

| Component | Networks | Published | Runtime secrets (files) |
| --- | --- | --- | --- |
| ingress | edge, app, idp | `127.0.0.1:9443 → 8443` | TLS key and certificate (one file in a mounted directory) |
| api | app, data | none | database URL, RabbitMQ publisher URL; lab CA certificate (public) |
| worker | data | none | database URL, RabbitMQ worker URL |
| postgres | data | none | admin, migrator, app, worker passwords |
| rabbitmq | data | none | definitions (users with password hashes, permissions, queue) |
| migrate (on demand) | data | none | migrator URL |
| keycloak | idp, idp-data | none | `keycloak.conf` (database password), realm file (client secrets, lab-user password) |
| keycloak-postgres | idp-data | none | Keycloak database password |
| oauth2-proxy | app, session | none | `oauth2-proxy.cfg` (client secret, cookie secret, Valkey password); lab CA certificate |
| valkey | session | none | `valkey.conf` (password) |

All networks except `edge` are Docker `internal` networks. The ingress has the
aliases `work-items.localhost` and `auth.work-items.localhost` on the `app`
network, so the API and oauth2-proxy reach the issuer URL through the ingress
with verified TLS. The API's network cannot reach Keycloak, its database, or
Valkey directly. RabbitMQ runs without the management plugin and without a
default or administrative user; operators use `rabbitmqctl` through
`docker compose exec`. Keycloak has no administrative user, and the ingress
does not route its administration console or master realm.

### Connections in this slice

| Connection | Encryption | Verification |
| --- | --- | --- |
| Client → ingress | TLS 1.2/1.3 | Lab CA; certificate names `work-items.localhost` and `auth.work-items.localhost`; other names and IP addresses fail the handshake |
| API, oauth2-proxy → issuer (through the ingress) | TLS 1.2/1.3 | Lab CA, mounted as a public certificate file |
| Ingress → API, ingress → oauth2-proxy | none (internal `app` network) | — |
| Ingress → Keycloak | none (internal `idp` network) | — |
| oauth2-proxy → Valkey | none (internal `session` network) | Valkey password |
| Keycloak → its PostgreSQL | none (internal `idp-data` network) | Database password |
| API/worker/migrator → PostgreSQL | none (`sslmode=disable`, internal `data` network) | — |
| API/worker → RabbitMQ | none (internal `data` network) | — |

The unencrypted internal hops are deliberate for this slice and are listed
under [limitations](#limitations).

## Prerequisites

- Docker with Compose v2 (verified with Docker Desktop on the WSL 2 backend,
  Docker Engine 29.6.2, Compose 5.3.1, WSL NAT networking mode);
- `openssl` and `curl` in WSL;
- network access for the first image build (Go modules, pnpm packages, pinned
  base images).

Port 9443 on host loopback must be free. The port is fixed: the issuer URL,
the client registrations, and the ingress's internal listener all carry it,
so `ZTP_SECURITY_LAB_HTTPS_PORT` is no longer supported and the tool refuses
any other value.

## Create the lab CA and secrets

```bash
./tools/security-lab-local init
```

`init` creates, outside the repository:

```text
~/.local/state/zero-to-prod/zero-to-prod-security-lab/   (mode 700)
├── .zero-to-prod-security-lab   ownership marker
├── pki/          lab CA key (600) and certificate; issued certificates
├── secrets/      runtime secret files (directory mode 700)
└── ingress-tls/  tls.pem: ingress key and certificate (directory mode 711)
```

Rerunning `init` keeps the existing lab CA and generated passwords, and leaves
derived files whose content would not change in place. It does replace or
remove:

- the ingress certificate and key, when missing, within two days of expiry, or
  lacking the name `auth.work-items.localhost`;
- the Keycloak, oauth2-proxy, and Valkey configuration files, if their content
  would change;
- the RabbitMQ definitions, when missing or when a RabbitMQ password was just
  generated (its password hashes are salted randomly);
- derived connection-URL files, if their content would change;
- ingress key and certificate files left in `secrets/` by earlier versions of
  the tool.

Running containers keep using the files they were created with; `start`
recreates them.

Override the location with `ZTP_SECURITY_LAB_STATE_DIR`. Because
`purge-state` deletes this directory, the tool validates its canonical path
(symlinks and `..` resolved): it must lie strictly below
`${XDG_STATE_HOME:-~/.local/state}` or `${TMPDIR:-/tmp}`, must not be or
contain your home directory, and must not overlap the repository.
`./tools/security-lab-local state-dir` prints the validated path. `init`
refuses an existing directory that is not this lab's state.

- The lab CA is valid for 90 days, may not issue intermediate CAs, and is
  name-constrained to `work-items.localhost` (and its subdomains) with IP
  addresses excluded.
- The ingress certificate is valid for 14 days. `init` reissues it when fewer
  than two days remain.
- Passwords and client secrets are random per lab. Connection URLs, the
  RabbitMQ definitions, and the Keycloak, oauth2-proxy, and Valkey
  configuration files are derived from them.
- Keycloak imports the realm file only when the realm does not exist yet. A
  realm created earlier keeps its client secrets and lab-user password even if
  the files change; `destroy` removes it with the Keycloak database volume.

### Secret file permissions

With Docker Desktop on WSL 2 (as verified), bind mounts keep the host owner
and mode, and each image runs as its own unprivileged user (PostgreSQL 70, RabbitMQ 999, ingress 101, API
and worker `app`, Keycloak 1000, oauth2-proxy 65532, Valkey 999). A mode-600 file owned by your WSL user is therefore not
readable inside those containers. Mounted secret files are mode 644 inside the
mode-700 `secrets/` directory:

- other WSL users cannot traverse the directory;
- each container receives only its own files;
- any process inside a container can read that container's files.

Modes of mounted directories are kept too. The ingress mounts `ingress-tls/`
itself, so that directory is mode 711 (enter, not list) for the ingress user;
on the host it is protected by the mode-700 state directory.

The files are **not** on tmpfs, **not** encrypted at rest, and **not** managed
by a secret manager. Compose `secrets:` outside Swarm are plain read-only bind
mounts. The CA key and the source passwords and client secrets (RabbitMQ,
client secrets, lab-user password, Valkey password, cookie secret) are never
mounted and stay mode 600; the mounted configuration files embed them.

## Start

```bash
./tools/security-lab-local start
```

`start`:

1. checks every required secret file (exists, is a regular non-empty file;
   certificate verifies against the lab CA, is unexpired, and matches its key);
2. builds the API, worker, ingress, and migration images;
3. recreates and starts PostgreSQL, RabbitMQ, Keycloak with its database, and
   Valkey, and waits until healthy;
4. re-applies the PostgreSQL identities from the secret files;
5. applies migrations with the migrator identity;
6. recreates and starts the API, worker, and ingress and waits until healthy;
7. recreates and starts oauth2-proxy last, because it discovers the issuer
   through the ingress at startup and exits if that fails.

Containers are recreated on every start (volumes are kept) so their file
mounts always refer to the current secret files: a container created before a
file was replaced keeps the old file, or fails to start if it is gone.

> In the verified environment, Compose did not fail when a secret file was
> missing: Docker created an empty directory at the mount point. Always start through the tool, which
> checks first. If you see a directory where a secret file should be, remove it
> and run `init`.

## Authentication

The API requires an access token on every Work Items route
(`AUTHENTICATION=oidc`); `/health`, `/ready`, and `/version` stay public and
are not routed by the ingress anyway. Local development keeps authentication
explicitly disabled (`AUTHENTICATION=disabled`, set by
`tools/work-items-api-local`); the API refuses to start without one of the two.

The API validates each token locally against Keycloak's published signing
keys: exactly one signature, an allowed asymmetric algorithm (RS256), a known
key, issuer `https://auth.work-items.localhost:9443/realms/work-items`,
audience `work-items-api`, and an unexpired `exp` (30 s clock skew). It does
not call Keycloak per request. Missing or rejected tokens get 401; a token
that cannot be judged because the signing keys are unavailable gets 503
`authentication_unavailable`.

### Browser sign-in

Open `https://work-items.localhost:9443/` and choose **Sign in**. Keycloak's
login page is served by the same ingress at `auth.work-items.localhost`. The
lab user is `lab-user`; its password is in the state directory:

```bash
./tools/security-lab-local lab-user
```

oauth2-proxy keeps the session in Valkey and gives the browser only a ticket
cookie (`__Host-wi-session`: Secure, HttpOnly, SameSite=Lax, path `/`).
For each API request nginx asks oauth2-proxy (`auth_request`), replaces the
request's credentials with the session's access token, and drops the cookie
before the API. Tokens are never readable by browser JavaScript. When
oauth2-proxy refreshes the session it issues a new session cookie on its auth
response; nginx forwards that `Set-Cookie` to the browser unchanged, together
with the usual security headers.

**Sign out** (a same-origin `fetch()` POST from the UI) deletes the Valkey session and calls
Keycloak's end-session endpoint with the session's ID token, so the next
sign-in asks for the password again.

### CSRF protection

For requests that carry the session cookie, the ingress refuses state changes
(any method but GET and HEAD) unless the request has the header
`X-Requested-With: work-items-web`, an `Origin` that is absent or equal to the
site, and `Sec-Fetch-Site` absent or `same-origin`. The Web UI sends the
header. Sign-out is POST-only and needs the same header, `Origin`, and fetch
metadata. A plain HTML form cannot sign out: the site sends
`Referrer-Policy: no-referrer`, under which browsers send a form post's
`Origin` as `null`, and `null` is always refused.

### Machine clients

The `work-items-automation` client uses the client-credentials grant and
sends its token as a bearer token. The ingress passes such requests to the API
unchanged; the gateway is not involved. A request with both a session cookie
and a bearer token is refused (400 `ambiguous_credentials`).

The client secret and the token are passed to curl as a config on standard
input (`-K -`, written by the shell builtin `printf`), so neither appears in
any process's arguments or environment. Do not use `-u` or
`-H "Authorization: …"` with the values on the command line; other processes
of the same user can read command lines.

```bash
state="$(./tools/security-lab-local state-dir)"
token="$(printf 'user = "work-items-automation:%s"\n' "$(< "$state/secrets/automation-client-secret")" \
  | curl -sS --cacert "$state/pki/ca.crt" -K - -d grant_type=client_credentials \
      https://auth.work-items.localhost:9443/realms/work-items/protocol/openid-connect/token \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')"
printf 'header = "Authorization: Bearer %s"\n' "$token" \
  | curl -sS --cacert "$state/pki/ca.crt" -K - https://work-items.localhost:9443/api/items
```

`$(< file)` is read by the shell itself. The token stays in a shell variable;
do not `export` it.

### Authorization

The API checks a permission for every Work Items route after authenticating
the token. Permissions are Keycloak client roles of `work-items-api`; the API
reads only `resource_access["work-items-api"].roles`. Role data of any other
shape, roles of other clients, and realm roles grant nothing.

| Route | Permission | `lab-user` (editor) | `lab-viewer` | `lab-outsider` | `work-items-automation` |
| --- | --- | --- | --- | --- | --- |
| `GET /api/items` | `items.read` | ✓ | ✓ | — | ✓ |
| `POST /api/items` | `items.create` | ✓ | — | — | — |
| `POST /api/items/{id}/process` | `items.process` | ✓ | — | — | ✓ |
| `GET /api/processing-jobs/{id}` | `items.read` | ✓ | ✓ | — | ✓ |

`lab-outsider` is a test principal with no Work Items permission; it holds
the name `items.create` only as a realm role and a gateway-client role, which
grant nothing. `./tools/security-lab-local lab-user` lists the three people
and their password files.

| Response | Meaning |
| --- | --- |
| 401 `unauthenticated` | No token, or the token was rejected (signature, issuer, audience, expiry) |
| 403 `insufficient_permissions` (`WWW-Authenticate: Bearer …, error="insufficient_scope"`) | Valid token without the route's permission; checked before the handler, so before 404 or 409 |
| 403 `csrf_rejected` | The ingress refused a browser state change; the API never saw it |

The API logs each authorization denial (subject, client, permission, route;
never the token). CSRF rejections appear only in the ingress access log
(`upstream=-`), not in the API log.

**Role changes** reach only newly issued tokens. A token issued before a
removal keeps its roles until it expires (up to 300 s plus 30 s skew). A
browser session picks up the change at the gateway's next refresh.

**Realms created before authorization.** Keycloak imports the realm file only
when the realm does not exist, so a lab created before this change has no
`work-items-api` roles and refuses Work Items requests with 403. `start`
checks the machine client's token. If it lacks that client's expected
permissions, `start` names them and recommends `verify-realm`; it does not
conclude why they are missing. This change does not upgrade such a lab. Run
authorization in a separate project, which leaves the existing lab and its
data untouched:

```bash
ZTP_SECURITY_LAB_PROJECT=zero-to-prod-security-lab-authz ./tools/security-lab-local init
```

```bash
ZTP_SECURITY_LAB_PROJECT=zero-to-prod-security-lab-authz ./tools/security-lab-local start
```

The separate project has its own state directory, lab CA, realm, and volumes.
Only one lab can run at a time (both use port 9443 and the same names); stop
the other first. Images are shared, so the other lab's next `start` builds
the current API. A browser needs the separate project's CA trusted
separately (see [trusting the lab CA](#trusting-the-lab-ca); use that
project's `ca-certificate`).

**Role-management client.** `work-items-lab-admin` exists only to read the
authorization configuration (`verify-realm`) and to remove and restore role
mappings during `verify-authorization`. It holds the realm-management roles
`view-clients` and `manage-users`, the minimum `verify-admin-permissions`
measured. Its secret is a generated file passed on standard input, and it is
reachable only from the internal `idp` network; the ingress does not route
`/admin`. It is created with the realm and removed by `destroy` and
`purge-state`.

### Lifetimes and revocation

| Setting | Value |
| --- | --- |
| Access token lifetime | 300 s |
| Gateway session refresh | after 1 min, on the next request; the ingress forwards the new session cookie (verified with a scripted cookie-jar client and in a manual Brave check) |
| Gateway session cookie | 8 h |
| Keycloak SSO session | 30 min idle, 8 h maximum |

Sign-out ends the gateway session (a copied cookie stops working) and the
Keycloak session. An access token captured before sign-out stays valid at the
API until it expires, because validation is local.

### Dependency outages

| Unavailable | Browser session | Machine client | New sign-in |
| --- | --- | --- | --- |
| Keycloak | Continues until its access token expires, then 401 | Continues while the API has cached keys; a restarted API answers 503 | 503 |
| oauth2-proxy | 503 | Unaffected | 503 |
| Valkey | 503 (sessions are lost when it restarts) | Unaffected | 503 |

The Valkey row relies on a separate classification in nginx: oauth2-proxy
answers 401 both for an invalid session and when Valkey is unreachable, so for
requests with a session cookie nginx asks oauth2-proxy's `/ready` endpoint and
answers 503 when it reports the store unreachable. `/ready` describes the
store when asked, not the cause of the original 401, so a request that races a
store failure or recovery can be misclassified; during an outage an invalid
or forged cookie also gets 503.

### Logging of authentication material

The aim is to keep authorization codes, state, tokens, and session-cookie
values out of the ingress and gateway logs:

- the ingress access log records the path without the query string and no
  headers or cookies. It keeps the status, upstream status, upstream address,
  upstream connect time, total request time, and a request ID;
- nginx's error log writes the full request line and referrer and cannot be
  reformatted. In the two public servers, error-log entries below `crit` from
  request processing are suppressed. This includes the `error`-level entries
  for upstream connection failures and timeouts, so their reason (for
  example refused versus timed out) is no longer logged; the access-log
  fields above remain. Startup and configuration errors are logged as before;
- oauth2-proxy's request log omits the request URI; its auth and error logs
  are unchanged and still name the user;
- the request ID (`rid=` in the access log, `X-Request-Id` in the response) is
  also logged by oauth2-proxy, so a gateway line can be matched to its path.

`verify-log-hygiene` checks this for the flows it drives (a sign-in, the
failed callbacks described under [verify](#verify), and a session refresh)
with the actual values of a run. It does not cover other flows or debug-level
logging.

## Verify

```bash
./tools/security-lab-local verify
./tools/security-lab-local verify-failures
./tools/security-lab-local verify-authentication
./tools/security-lab-local verify-log-hygiene
./tools/security-lab-local verify-realm
./tools/security-lab-local verify-authorization
```

(For the separate authorization project, prefix each with
`ZTP_SECURITY_LAB_PROJECT=zero-to-prod-security-lab-authz`.)

`verify-realm` reads the realm through the admin API and compares it with the
authorization model:
- the client and its roles;
- the composite roles;
- every principal's effective roles;
- the misplaced-claim setup;
- the role-management client's own roles.

It does not infer readiness from one person's token.

`verify-authorization`:
- checks the matrix on the browser and machine paths with fixtures the editor creates; denied actions must leave no side effect;
- checks misplaced claims, wrong audience, and a token with an added role;
- checks that 403 precedes 404 and 409;
- checks the API's decision log line versus a CSRF rejection's absence;
- measures role removal, separately for a browser session, a browser token captured before removal, and a machine token issued before removal;
- checks behavior with Keycloak down and keys cached;
- runs the same unauthorized actions against an API built from the pinned pre-authorization commit, where they must succeed.

It stops and restarts services, temporarily removes and restores role mappings, and without `--skip-slow` waits up to seven minutes for tokens to expire.

`verify-admin-permissions` runs in a throwaway project
(`ZTP_SECURITY_LAB_PROJECT=zero-to-prod-security-lab-probe`). It imports the
realm repeatedly with different role sets for the role-management client,
reduces the candidate set to one in which every role is needed, and removes
the project and its state afterwards.

`verify` checks, with positive controls:

- that the reachability probe itself works: a listener that accepts but never
  responds must count as connected (from a container, WSL, and Windows) and a
  closed port as refused. Reachability is judged from curl's count of
  established connections, because exit 28 also follows a successful
  connection whose response times out;
- container health and that only the ingress publishes a port, on 127.0.0.1;
- HTTPS from WSL with the lab CA (UI, issuer discovery, and the API answering
  401 without credentials), failure without the lab CA, and handshake
  rejection for other names;
- that `/api/health`, `/api/version`, the gateway's `/oauth2/auth`, and
  Keycloak's administration console and master realm are not routed;
- no listener on common backend ports on WSL or Windows loopback;
- HTTPS from Windows with `curl.exe` and the lab CA (through WSL interop), and
  no connection to the ingress port on Windows' non-loopback addresses. The
  Windows "without lab CA" control is skipped, with the reason recorded, while
  Windows itself trusts the lab CA;
- that a container on the ingress's client network, a container on an
  unrelated network, and a container on the default bridge cannot resolve or
  connect to the API, PostgreSQL, RabbitMQ, Keycloak, oauth2-proxy, or Valkey,
  and that the API's network cannot reach Keycloak, its database, or Valkey;
- that the same probes **do** reach each backend from its own network, and the
  ingress reaches the API.

`verify-failures` checks that each service fails clearly, without printing
secret values, when a required secret or setting is missing, including an
unset `AUTHENTICATION` and a missing `OIDC_CA_FILE`.

`verify-authentication` signs in with a scripted browser (curl following
Keycloak's form), uses the machine client, and checks token rejection
(changed payload, `alg: none`, malformed, an issuer-signed ID token for
another audience), Work Items behavior on both paths, CSRF refusals, that the
scripted client's cookie jar receives and then uses the refreshed session
cookie, sign-out,
cookie replay, captured-token replay, and Keycloak, gateway, and store
outages. It **stops and restarts lab services**, and without `--skip-slow` it
waits for access tokens to expire (about 12 minutes in total). To compare a
captured browser token, it reads the session's token from oauth2-proxy with a
probe on the internal `app` network; no public route exposes it.

`verify-log-hygiene` drives a successful sign-in and three failed callbacks
(a reused code, Keycloak down during redemption, oauth2-proxy down when the
browser returns), an authorization request to a stopped Keycloak, and a
session refresh. It then
searches the logs written during the run for the authorization codes, state
values, session cookie, access tokens, and long-lived secrets it used, as
fixed strings and in their percent-encoded forms. Positive controls show that
the scanner finds each string and that each log was captured. It stops and
restarts Keycloak and oauth2-proxy, and never prints or records the values.

`verify-renewal` renews the ingress certificate on the running lab and
records the served and file serials before and after, whether the container
sees the host's file, and a control showing that a single-file bind mount
would have kept the old file.

All five accept `--evidence <file>` to write machine-readable results.

## Use it from the Windows browser (manual)

Chromium-based browsers and Firefox resolve `*.localhost` to loopback
themselves, so no hosts-file change is needed.

Until the lab CA is trusted, the browser must reject the site
(`NET::ERR_CERT_AUTHORITY_INVALID` or similar). That is the expected negative
result. Do not click through the warning.

### Trusting the lab CA

Trusting a root CA lets whoever holds its private key create certificates that
your browser accepts. The lab CA is limited by name constraints to
`work-items.localhost`, its key never leaves the WSL state directory, and it
expires after 90 days. Remove it when you are done.

1. Show the CA details:

   ```bash
   ./tools/security-lab-local ca-certificate
   ```

2. Either trust it for your Windows user (Edge and Chrome use this store), in
   PowerShell:

   ```powershell
   Import-Certificate -FilePath '\\wsl.localhost\Ubuntu\home\<user>\.local\state\zero-to-prod\zero-to-prod-security-lab\pki\ca.crt' -CertStoreLocation Cert:\CurrentUser\Root
   ```

   Windows shows a security warning with a thumbprint. Continue only if it
   matches the **SHA-1** fingerprint from step 1 (without colons).

   Or, for a more contained trust, import it only into a dedicated Firefox
   profile: Settings → Privacy & Security → Certificates → View Certificates →
   Authorities → Import, and allow it to identify websites.

3. Follow the [manual browser check](#manual-browser-check-brave).

### Manual browser check (Brave)

The automated checks use scripted clients; this check is the browser
verification. Use a fresh private window (Ctrl+Shift+N) so no earlier cookies
interfere. Brave on Windows uses the Windows certificate store. Do not copy
cookie values or tokens into notes or screenshots.

1. In WSL, start the lab and locate the lab user's password:

   ```bash
   ./tools/security-lab-local start
   ./tools/security-lab-local lab-user
   ```

2. Open `https://work-items.localhost:9443/`. Expect no certificate warning;
   the site information shows a certificate issued by "Zero-to-Prod security
   lab CA". Expect the **Sign in required** notice.
3. Open DevTools (F12) on the **Network** tab with **Preserve log** enabled.
4. Choose **Sign in**. Expect Keycloak's login page at
   `https://auth.work-items.localhost:9443/realms/work-items/…`. Sign in as
   `lab-user`. Expect to return to the Work Items page, with the lab user's
   email and a **Sign out** button at the top.
5. In **Application → Storage → Cookies → https://work-items.localhost:9443**,
   check `__Host-wi-session`: Secure and HttpOnly set, SameSite `Lax`, path
   `/`. Note its expiry time.
6. Choose **New Work Item**, enter a title, and **Create**: the row appears
   as `pending`. Choose **Process**: the job goes from `accepted` to
   `succeeded`, and the result shows character and word counts.
7. Session refresh: wait at least 70 seconds, then reload the page (F5).
   - In **Network**, select the `items` request; its response headers include
     `set-cookie: __Host-wi-session=…; Path=/; Max-Age=28800; HttpOnly;
     Secure; SameSite=Lax`.
   - In **Application → Cookies**, the cookie's expiry time is later than in
     step 5.
   - Create and process a second Work Item: it still works, so the browser
     stored and sent the refreshed cookie.
8. Choose **Sign out**. In **Network**, the `sign_out` request is a `fetch`
   with `Origin: https://work-items.localhost:9443` and
   `X-Requested-With: work-items-web`, answered with a redirect. Expect the
   page to return to **Sign in required**, and `__Host-wi-session` to be gone
   from **Application → Cookies**. A 403 `csrf_rejected` here is the defect
   recorded in the integration record.
9. Choose **Sign in** again. Expect Keycloak's login form asking for the
   password, not a silent sign-in, which shows that the Keycloak session
   ended. Close the window without signing in.
10. Authorization (separate project): in a new private window, sign in as
    `lab-viewer`. Listing works. **New Work Item → Create** shows "Not
    allowed" inside the dialog and keeps the entered title; editing the title
    or closing the dialog clears the message. **Process** shows "Not allowed"
    on the page. The session stays signed in. In **Network**,
    the refused request has status 403 with `insufficient_permissions`.
11. In WSL, stop the lab: `./tools/security-lab-local stop`.

If the browser shows `NET::ERR_CERT_AUTHORITY_INVALID`, the lab CA is not
trusted. Do not click through; trust it as described above and restart
Brave.

### Removing the lab CA

Windows user store, in PowerShell (use the SHA-1 fingerprint without colons):

```powershell
Get-ChildItem Cert:\CurrentUser\Root | Where-Object Thumbprint -eq '<SHA1>' | Remove-Item
```

or `certmgr.msc` → Trusted Root Certification Authorities → Certificates →
"Zero-to-Prod security lab CA (…)" → Delete. In Firefox, delete it under
Authorities. Remove it before `purge-state`, which deletes the CA files.

## Certificate renewal

```bash
./tools/security-lab-local issue-certificate
```

This issues a new ingress certificate from the lab CA and writes key and
certificate as one file, replaced by a single rename in `ingress-tls/`. The
ingress mounts that directory, not the file, because a file bind mount keeps
the original file after it is replaced. If the ingress is running, the tool
reloads nginx and then requires five consecutive new verified connections to
receive the new serial: nginx starts the new worker before the old one stops
accepting, and in the verified environment new connections could still
receive the old certificate for about a second after a reload.

There is no revocation (no CRL or OCSP). Windows `curl.exe` therefore runs
with `--ssl-revoke-best-effort`; chain and name checks stay on.

## Stop and remove

```bash
./tools/security-lab-local stop        # stop containers, keep volumes
./tools/security-lab-local destroy     # remove this lab's containers, networks, volumes
./tools/security-lab-local purge-state --yes   # delete this lab's CA and secrets
```

`destroy` acts only on the `zero-to-prod-security-lab` Compose project (the
tool rejects other project names) and its probe network. It never touches the
development lab projects or their volumes.

`purge-state` deletes the state directory only if it passes the location
checks above, carries this lab's ownership marker, belongs to your user, and
contains nothing but the lab's own entries; otherwise it refuses and deletes
nothing. Remove any browser trust first.

**Pending cleanup from the authentication comparison.** The comparison left
the Docker volume `zero-to-prod-security-lab_keycloak_postgres_data` and the
state directory
`~/.local/state/zero-to-prod/zero-to-prod-security-lab-auth-experiment/`.
Neither `destroy` nor `purge-state` removes them. They hold that experiment's
realm and generated secrets and are not used by the lab; remove them
manually when no longer needed.

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `secret file missing` or `is a directory` from `check-secrets` | `init` not run, or Docker created a directory for a missing file | Remove the directory; run `init` |
| `port is already allocated` | Something else uses host port 9443 | Stop it; the lab's port is fixed |
| Sign-in fails with an invalid client or the lab-user password is refused | The Keycloak realm was imported with earlier secrets (for example after `purge-state` without `destroy`) | `destroy`, then `start` (re-imports the realm) |
| `start` warns that the work-items-automation access token lacks permissions, or Work Items requests get 403 `insufficient_permissions` | The principal's token lacks the route's permission: for example a realm created before authorization, a changed role mapping, or a failed token request | Run `verify-realm` to compare the realm with the authorization model. For a realm created before authorization, use the separate authorization project (see [authorization](#authorization)); this change does not upgrade existing realms |
| API answers 503 `authentication_unavailable` | The API cannot fetch Keycloak's signing keys; the log names the cause | Check Keycloak's health and `logs api` |
| oauth2-proxy exits at startup | Issuer discovery through the ingress failed | Check that Keycloak and the ingress are healthy; `start` again |
| Browser certificate error after trusting | Certificate expired, or the browser does not use the Windows store | `issue-certificate`; check which store the browser uses |
| API or worker exits with `read DATABASE_URL_FILE` | A secret file is missing in the container | `check-secrets`; recreate with `init` |
| `refusing to use a directory that is not security lab state` or `no security lab marker` | `ZTP_SECURITY_LAB_STATE_DIR` points at a directory the lab did not create | Choose an empty or new path below an allowed base |
| `ingress TLS directory missing` or `must have mode 711` | State created by an earlier version of the tool | Run `init` |

## Limitations

- Coarse role-based authorization: no per-item or ownership rules. No rate
  limiting.
- Role removal reaches tokens already issued only when they expire (up to
  300 s plus 30 s skew).
- Labs created before authorization are not upgraded; use a separate project.
- `work-items-lab-admin` is a standing privileged credential in the lab realm
  (internal network only).
- The UI does not know permissions in advance; it explains a 403 afterwards.
- An access token captured before sign-out is accepted until it expires (up
  to 300 s plus 30 s skew). With Keycloak down, sessions continue until their
  access token expires.
- Sessions are not persisted: a Valkey restart signs everyone out.
- The store-outage classification is racy (see
  [dependency outages](#dependency-outages)).
- In the public servers, nginx error-log entries below `crit` from request
  processing are suppressed, so upstream failure reasons are not logged; the
  access log keeps status, upstream status and address, upstream connect time,
  request time, and request IDs.
- Log hygiene is verified only for the tested sign-in, failed-callback, and
  session-refresh flows.
- One lab user and one machine client, defined in the realm file; no
  self-service, MFA, or Keycloak administration access.
- The HTTPS port is fixed at 9443.
- Ingress → API, ingress → Keycloak and oauth2-proxy, oauth2-proxy → Valkey,
  Keycloak → its database, and API/worker → PostgreSQL and RabbitMQ are
  unencrypted on internal networks.
- In the verified environment, Docker forwarded the published ingress port to
  the ingress container from any network on the same Docker engine, and
  Docker Desktop also exposed it through `host.docker.internal`. "Host
  loopback only" there limits host listeners, not access from other local
  containers. Backends were not affected.
- In the verified environment, the ingress saw every host client as the edge
  network gateway address, so per-address limits would treat all host clients
  as one.
- No secret rotation procedure yet. The migrator URL is visible in the migrate
  container's process arguments while it runs; it is the one known secret in
  a process argument. The tool passes passwords, client secrets, tokens,
  cookies, and codes to curl, docker, grep, and Python on standard input, and
  a sampler found none in host process arguments or environments during the
  checks. Secrets are still held in the tool's shell variables and in the
  test cookie jars (in a mode-700 temporary directory) while it runs.
- Single-tier lab CA without revocation; no public-CA operations.
- The ingress image is not built in CI; CI validates the Compose model and
  scripts only.
- No content security policy or HSTS yet.
- The development API still listens on all interfaces when run natively; see
  the [experiment record](../experiments/issue-121-local-https-ingress.md).

See the [experiment record](../experiments/issue-121-local-https-ingress.md)
for what was verified.
