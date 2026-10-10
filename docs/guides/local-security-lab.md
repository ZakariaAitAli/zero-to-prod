# Local security lab

The security lab is an opt-in, local-first environment for Issue #121
(identity, secrets, TLS, and ingress boundary). It runs Work Items behind a
single HTTPS ingress on host loopback, with private backend services and its
own runtime secrets.

**Status: intermediate.** This first slice provides the ingress, private
networks, a private lab CA, and file-based runtime secrets. It has **no
authentication or authorization**: anyone who can reach the ingress can use
the API. Datastore TLS, rate limiting, and secret rotation are also not
implemented yet. It does not complete Issue #121.

The development lab ([local Work Items guide](local-work-items.md)) is
unchanged and keeps its labelled development credentials. The security lab
uses separate Compose project names, networks, and volumes, and refuses to
start without its own secret files.

## Layout

```text
Windows browser / WSL client
        │ https://work-items.localhost:9443   (127.0.0.1 only; lab CA)
        ▼
  ingress (nginx) ── edge network
        │  serves the Web UI; forwards /api/items, /api/items/{id}/process,
        │  /api/processing-jobs/{id}; everything else under /api is 404
        │  plain HTTP on the internal app network
        ▼
  API ───────────── app network (internal)
        │
        ├── data network (internal) ── PostgreSQL, RabbitMQ, worker
```

| Component | Networks | Published | Runtime secrets (files) |
| --- | --- | --- | --- |
| ingress | edge, app | `127.0.0.1:9443 → 8443` | TLS certificate and key |
| api | app, data | none | database URL, RabbitMQ publisher URL |
| worker | data | none | database URL, RabbitMQ worker URL |
| postgres | data | none | admin, migrator, app, worker passwords |
| rabbitmq | data | none | definitions (users with password hashes, permissions, queue) |
| migrate (on demand) | data | none | migrator URL |

`app` and `data` are Docker `internal` networks. RabbitMQ runs without the
management plugin and without a default or administrative user; operators use
`rabbitmqctl` through `docker compose exec`.

### Connections in this slice

| Connection | Encryption | Verification |
| --- | --- | --- |
| Client → ingress | TLS 1.2/1.3 | Lab CA; certificate name `work-items.localhost`; other names and IP addresses fail the handshake |
| Ingress → API | none (internal `app` network) | — |
| API/worker/migrator → PostgreSQL | none (`sslmode=disable`, internal `data` network) | — |
| API/worker → RabbitMQ | none (internal `data` network) | — |

The unencrypted internal hops are deliberate for this slice and are listed
under [limitations](#limitations).

## Prerequisites

- Docker with Compose v2 (verified with Docker Desktop 29.6.2, Compose 5.3.1);
- `openssl` and `curl` in WSL;
- network access for the first image build (Go modules, pnpm packages, pinned
  base images).

Port 9443 on host loopback must be free; override it with
`ZTP_SECURITY_LAB_HTTPS_PORT`.

## Create the lab CA and secrets

```bash
./tools/security-lab-local init
```

`init` creates, outside the repository:

```text
~/.local/state/zero-to-prod/zero-to-prod-security-lab/   (mode 700)
├── pki/       lab CA key (600) and certificate; issued certificates
└── secrets/   runtime secret files (directory mode 700)
```

It never overwrites existing values. Override the location with
`ZTP_SECURITY_LAB_STATE_DIR`; the tool refuses a path inside the repository.

- The lab CA is valid for 90 days, may not issue intermediate CAs, and is
  name-constrained to `work-items.localhost` (and its subdomains) with IP
  addresses excluded.
- The ingress certificate is valid for 14 days. `init` reissues it when fewer
  than two days remain.
- Passwords are random per lab. Connection URLs and the RabbitMQ definitions
  are derived from them.

### Secret file permissions

Docker Desktop bind mounts keep the host owner and mode, and each image runs
as its own unprivileged user (PostgreSQL 70, RabbitMQ 999, ingress 101, API
and worker `app`). A mode-600 file owned by your WSL user is therefore not
readable inside those containers. Mounted secret files are mode 644 inside the
mode-700 `secrets/` directory:

- other WSL users cannot traverse the directory;
- each container receives only its own files;
- any process inside a container can read that container's files.

The files are **not** on tmpfs, **not** encrypted at rest, and **not** managed
by a secret manager. Compose `secrets:` outside Swarm are plain read-only bind
mounts. The CA key and the two RabbitMQ source passwords are never mounted and
stay mode 600.

## Start

```bash
./tools/security-lab-local start
```

`start`:

1. checks every required secret file (exists, is a regular non-empty file;
   certificate verifies against the lab CA, is unexpired, and matches its key);
2. builds the API, worker, ingress, and migration images;
3. starts PostgreSQL and RabbitMQ and waits until healthy;
4. re-applies the PostgreSQL identities from the secret files;
5. applies migrations with the migrator identity;
6. starts the API, worker, and ingress and waits until healthy.

> Compose alone does not fail when a secret file is missing: Docker creates an
> empty directory at the mount point. Always start through the tool, which
> checks first. If you see a directory where a secret file should be, remove it
> and run `init`.

## Verify

```bash
./tools/security-lab-local verify
./tools/security-lab-local verify-failures
```

`verify` checks, with positive controls:

- container health and that only the ingress publishes a port, on 127.0.0.1;
- HTTPS from WSL with the lab CA (UI and API), failure without the lab CA, and
  handshake rejection for other names;
- that `/api/health` and `/api/version` are not routed;
- no listener on common backend ports on WSL or Windows loopback;
- HTTPS from Windows with `curl.exe` and the lab CA (through WSL interop), and
  no connection to the ingress port on Windows' non-loopback addresses;
- that a container on the ingress's client network, a container on an
  unrelated network, and a container on the default bridge cannot resolve or
  connect to the API, PostgreSQL, or RabbitMQ;
- that the same probes **do** reach each backend from its own network, and the
  ingress reaches the API.

`verify-failures` checks that each service fails clearly, without printing
secret values, when a required secret is missing.

Both accept `--evidence <file>` to write machine-readable results.

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

3. Open `https://work-items.localhost:9443/`. Check that the certificate is
   issued by "Zero-to-Prod security lab CA", then list, create, and process a
   Work Item and wait for `succeeded` and its result.

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

This issues a new ingress certificate from the lab CA and reloads nginx if the
ingress is running. There is no revocation (no CRL or OCSP). Windows `curl.exe`
therefore runs with `--ssl-revoke-best-effort`; chain and name checks stay on.

## Stop and remove

```bash
./tools/security-lab-local stop        # stop containers, keep volumes
./tools/security-lab-local destroy     # remove this lab's containers, networks, volumes
./tools/security-lab-local purge-state --yes   # delete this lab's CA and secrets
```

`destroy` acts only on the `zero-to-prod-security-lab` Compose project (the
tool rejects other project names) and its probe network. It never touches the
development lab projects or their volumes. `purge-state` deletes only the
security lab's state directory; remove any browser trust first.

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `secret file missing` or `is a directory` from `check-secrets` | `init` not run, or Docker created a directory for a missing file | Remove the directory; run `init` |
| `port is already allocated` | Something else uses host port 9443 | Set `ZTP_SECURITY_LAB_HTTPS_PORT` |
| Browser certificate error after trusting | Certificate expired, or the browser does not use the Windows store | `issue-certificate`; check which store the browser uses |
| API or worker exits with `read DATABASE_URL_FILE` | A secret file is missing in the container | `check-secrets`; recreate with `init` |

## Limitations

- No authentication, authorization, CSRF protection, or rate limiting.
- Ingress → API, and API/worker → PostgreSQL and RabbitMQ, are unencrypted on
  internal networks.
- Docker forwards the published ingress port to the ingress container from any
  network on the same Docker engine, and Docker Desktop also exposes it
  through `host.docker.internal`. "Host loopback only" therefore limits host
  listeners, not access from other local containers. Backends are not
  affected.
- The ingress sees every host client as the edge network gateway address, so
  per-address limits would treat all host clients as one.
- No secret rotation procedure yet. The migrator URL is visible in the migrate
  container's process arguments while it runs.
- Single-tier lab CA without revocation; no public-CA operations.
- The ingress image is not built in CI; CI validates the Compose model and
  scripts only.
- No content security policy or HSTS yet.
- The development API still listens on all interfaces when run natively; see
  the [experiment record](../experiments/issue-121-local-https-ingress.md).

See the [experiment record](../experiments/issue-121-local-https-ingress.md)
for what was verified.
