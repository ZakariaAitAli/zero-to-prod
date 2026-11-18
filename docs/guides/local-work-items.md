# Run Work Items locally

## Requirements

Docker with Compose, Bash, curl, Go 1.27.4, Node.js 22 and pnpm 12.8.1.
Run the commands below from the repository root unless stated otherwise.
The shell scripts require Bash; the command invocations work in Bash and Fish.
No cloud credentials are needed.

## 1. Start dependencies

For a fresh local database:

```sh
./tools/postgres-local start
./tools/postgres-local build-migrate
./tools/postgres-local migrate-up
./tools/rabbitmq-local start
```

Migrations are explicit; application startup never applies them. Migration 5
requires empty application tables. If an existing database fails that check,
stop and follow the [recovery runbook](../runbooks/work-items-recovery.md).
Do not delete a volume or force a migration version to make startup pass.

Default development ports are PostgreSQL 55432, RabbitMQ 5672 (management
15672), API 8080 and UI 5173. The local tools supply development-only database
and broker credentials. These defaults are unsuitable for exposed deployments.

## 2. Start the API and worker

In separate terminals:

```sh
./tools/work-items-api-local run
```

```sh
./tools/work-items-worker-local run
```

Each helper builds its executable first and uses the appropriate runtime
identity. The API publishes accepted jobs through the outbox; the worker
consumes them and writes results. Both need PostgreSQL and RabbitMQ.

Verify the running API in another terminal:

```sh
./tools/work-items-api-local verify
```

The checks require exact health, readiness and version responses. Liveness is
`GET /health`; readiness is `GET /ready`; build identity is `GET /version`.
Readiness checks dependencies, not just whether the process is running.

## 3. Start the UI

```sh
cd apps/work-items/web
corepack enable
pnpm install --frozen-lockfile
pnpm dev
```

Open [localhost:5173](http://localhost:5173). The Vite development proxy sends
`/api/*` to the API on port 8080. The UI lists and creates Work Items, submits
processing, and polls jobs until they succeed or fail. A successful job leaves
a persisted title-analysis result and a done item; a failed job leaves the item
pending and available for another attempt.

The development API has authentication explicitly disabled. For authentication,
TLS and ingress, use the separate [security lab](local-security-lab.md).

## Checks

Backend, from the repository root:

```sh
./tools/work-items-api-local test
./tools/work-items-worker-local test
```

Frontend, from `apps/work-items/web`:

```sh
pnpm lint
pnpm test
pnpm build
```

Integration tests need explicit database and broker fixture settings. The
[CI workflow](../../.github/workflows/work-items-ci.yml) defines the automated
setup and rejects skipped integration tests. A dependency-free test run does
not establish integration behavior.

## Configuration

| Setting | Purpose |
|---|---|
| `DATABASE_URL` | API or worker database connection; use the matching identity. |
| `RABBITMQ_PUBLISHER_URL` / `RABBITMQ_WORKER_URL` | Role-specific broker connection. |
| `RABBITMQ_QUEUE` | Processing queue; default `work_item_processing`. |
| `PORT`, `VERSION`, `EXPECTED_VERSION`, `BASE_URL` | API listener, build identity and verification target. |
| `ZTP_COMPOSE_PROJECT_NAME`, `ZTP_POSTGRES_PORT`, `ZTP_POSTGRES_DB` | PostgreSQL project and target. |
| `ZTP_RABBITMQ_COMPOSE_PROJECT_NAME`, `ZTP_RABBITMQ_AMQP_PORT`, `ZTP_RABBITMQ_MANAGEMENT_PORT` | Separate broker project and ports. |

For an additional environment, choose unused project names and ports for both
dependencies and set every runtime URL to match. For example:

```sh
env ZTP_COMPOSE_PROJECT_NAME=zero-to-prod-extra ZTP_POSTGRES_PORT=55435 ./tools/postgres-local start
```

Repeat those overrides for every operation on that project, including migrations,
status, backup and stop. The API must use port 55435 in its database URL; changing
the Compose port alone does not change runtime connections. See the
[PostgreSQL guide](local-postgresql.md) for database roles and lifecycle details.

## Stop

Stop API, worker and UI with Ctrl+C in their terminals, then:

```sh
./tools/rabbitmq-local stop
./tools/postgres-local stop
```

Stop preserves data. The tools' `destroy` operations remove volumes and are
separate, destructive actions; do not use them as routine shutdown.
