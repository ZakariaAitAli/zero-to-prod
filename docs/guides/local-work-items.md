# Local Work Items development

The Work Items system can be developed and verified locally with the repository-owned PostgreSQL and RabbitMQ labs and without AWS credentials or cloud infrastructure.

The local system includes:

~~~text
Web UI
  ↓
Go API
  ↓
PostgreSQL
  ↓
transactional outbox
  ↓
RabbitMQ
  ↓
worker
~~~

## Prerequisites

Required for the local workflow:

- Go 1.27.0 or newer (Go 1.27.1 preferred by `go.mod`)
- Bash
- curl
- Docker with Docker Compose
- Node.js 22
- Corepack
- pnpm 12.8.1, pinned by the frontend `packageManager` field

`test` and `build` remain native Go operations.

`run` requires the local PostgreSQL and RabbitMQ labs. Dependency-aware `verify` checks the API's HTTP contract, including readiness backed by the critical PostgreSQL persistence dependency.

## Local interface

Run commands from the repository root.

### Test

```bash
./tools/work-items-api-local test
```

### Build

```bash
./tools/work-items-api-local build
```

The default binary is written outside the repository:

```text
/tmp/zero-to-prod-work-items-api
```

### Move to the result-processing schema

Migration 5 refuses to upgrade populated application tables. Preserve the
legacy database/volume and use a separate local project and port:

```bash
export ZTP_COMPOSE_PROJECT_NAME=zero-to-prod-results
export ZTP_POSTGRES_PORT=55433
./tools/postgres-local start
./tools/postgres-local migrate-up
```

Use port 55433 in the API and worker `DATABASE_URL` values. Keep these overrides
for backup/restore commands too. The default project and its data are retained;
no reset or truncation is part of migration. See the
[current recovery runbook](../runbooks/work-items-recovery.md).

### Isolated crash-consistency lab

Destructive experiments use their own PostgreSQL and RabbitMQ Compose projects
so existing lab volumes and backups are never touched.
`ZTP_RABBITMQ_COMPOSE_PROJECT_NAME` selects the RabbitMQ project (default
`zero-to-prod-rabbitmq`); bootstrapping a second queue on the shared broker is
not an alternative, because it rewrites the worker user's single-queue
permission.

```bash
export ZTP_COMPOSE_PROJECT_NAME=zero-to-prod-118 ZTP_POSTGRES_PORT=55434
export ZTP_RABBITMQ_COMPOSE_PROJECT_NAME=zero-to-prod-118-rabbitmq ZTP_RABBITMQ_AMQP_PORT=5673 ZTP_RABBITMQ_MANAGEMENT_PORT=15673
./tools/postgres-local start
./tools/postgres-local build-migrate
./tools/postgres-local migrate-up
./tools/rabbitmq-local start
```

Run `go test ./...` before exporting integration URLs: with them set, the API
and worker packages run in parallel against the same queue and the worker
suite's isolation guard fails. Then export the role-specific URLs:

```bash
pg=127.0.0.1:55434/zero_to_prod?sslmode=disable; mq=127.0.0.1:5673/zero_to_prod
export OUTBOX_INTEGRATION_DATABASE_URL="postgres://zero_to_prod_app:zero-to-prod-local-app@$pg" \
  ACCEPTANCE_FAILURE_MIGRATOR_DATABASE_URL="postgres://zero_to_prod_migrator:zero-to-prod-local-migrator@$pg" \
  WORKER_DATABASE_URL="postgres://zero_to_prod_worker:zero-to-prod-local-worker@$pg" \
  WORKER_FIXTURE_DATABASE_URL="postgres://zero_to_prod_migrator:zero-to-prod-local-migrator@$pg" \
  ISSUE118_ADMIN_DATABASE_URL="postgres://zero_to_prod_admin:zero-to-prod-local-admin@$pg" \
  RABBITMQ_PUBLISHER_URL="amqp://zero_to_prod_publisher:zero-to-prod-local-rabbitmq-publisher@$mq" \
  RABBITMQ_WORKER_URL="amqp://zero_to_prod_worker:zero-to-prod-local-rabbitmq-worker@$mq" \
  RABBITMQ_FIXTURE_URL="amqp://zero_to_prod:zero-to-prod-local-rabbitmq@$mq" \
  RABBITMQ_QUEUE=work_item_processing ISSUE118_EVIDENCE_DIR="$PWD/evidence/issue-118"
```

Run the API integration tests, purge the isolated queue, then the worker
integration tests; the `crashexperiment` build tag adds the process-crash
harness. The snapshot experiment destroys the isolated PostgreSQL volume and
runs only when `ISSUE118_ALLOW_DESTROY` names that project. The harness refuses
non-`zero-to-prod-118*` projects, protected ports, and endpoints that do not
match the validated containers. See the
[Issue #118 experiment](../experiments/issue-118-crash-consistency.md).

```bash
(cd apps/work-items && go test ./api -count=1 -v)
docker compose --project-name "$ZTP_RABBITMQ_COMPOSE_PROJECT_NAME" --file infra/local/rabbitmq/compose.yaml exec -T rabbitmq rabbitmqctl -p zero_to_prod purge_queue work_item_processing
(cd apps/work-items && go test ./worker -count=1 -v)
(cd apps/work-items && go test -tags crashexperiment ./worker -count=1 -v -timeout 15m -run TestIssue118)
(cd apps/work-items && ISSUE118_ALLOW_DESTROY=zero-to-prod-118 go test -tags crashexperiment ./worker -count=1 -v -timeout 15m -run TestIssue118E12)
```

With the same project overrides still exported, tear down only the isolated lab:

```bash
./tools/rabbitmq-local destroy
./tools/postgres-local destroy
```

### Run

Start PostgreSQL, apply migrations explicitly, and start RabbitMQ before starting the API:

```bash
./tools/postgres-local start
./tools/postgres-local build-migrate
./tools/postgres-local migrate-up
./tools/rabbitmq-local start
./tools/work-items-api-local run
```

Defaults:

```text
PORT=8080
VERSION=local
DATABASE_URL=postgres://zero_to_prod_app:zero-to-prod-local-app@127.0.0.1:55432/zero_to_prod?sslmode=disable
RABBITMQ_PUBLISHER_URL=amqp://zero_to_prod_publisher:zero-to-prod-local-rabbitmq-publisher@127.0.0.1:5672/zero_to_prod
RABBITMQ_QUEUE=work_item_processing
```

The default database credentials are development-only credentials from the local lab.

The run command builds the application before starting it. It does not run migrations.

### Verify

With the API running in another terminal:

```bash
./tools/work-items-api-local verify
```

Verification checks exact responses from:

```text
GET /health
GET /ready
GET /version
```

Expected responses for a running, ready instance are:

```text
/health   {"status":"healthy"}
/ready    {"status":"ready"}
/version  {"version":"local"}
```

## Health and readiness

The two status endpoints have different purposes.

`GET /health` is the process liveness signal. It reports whether the application process is alive enough to serve HTTP.

`GET /ready` is the traffic-acceptance signal. Readiness requires both application lifecycle readiness and usable PostgreSQL persistence.

The datastore check is a bounded, read-only query against the exact `work_items` columns required by the application:

```sql
SELECT id, title, status, created_at
FROM public.work_items
LIMIT 0;
```

This deliberately does not inspect migration-version metadata or mutate schema state.

As a result:

- PostgreSQL unavailable -> `/ready` returns `503`
- PostgreSQL reachable but required schema unavailable -> `/ready` returns `503`
- required persistence contract available -> `/ready` returns `200`
- graceful shutdown begins -> readiness becomes false before shutdown

When the application is not ready, `/ready` returns:

```text
HTTP 503
{"status":"not_ready"}
```

RabbitMQ availability is deliberately not part of the `/ready` request-time check. Accepted processing work is first persisted in PostgreSQL through the transactional outbox, while the background publisher independently retries broker publication.

## Persistence endpoints

The local API currently exposes the minimal Work Items persistence surface:

```text
POST /items
GET /items
```

`POST /items` accepts a JSON body containing a non-empty `title`.

It also accepts an optional `status`:

```json
{
  "title": "example",
  "status": "done"
}
```

Supported status values are:

```text
pending
done
```

If `status` is omitted, the application uses `pending`.

An unsupported status returns:

```text
HTTP 400
{"error":"invalid_status"}
```

Both endpoints use the `zero_to_prod_app` runtime identity. Database failures are returned as a generic:

```text
HTTP 503
{"error":"persistence_unavailable"}
```

Database connection details are not returned in the HTTP response.

## Asynchronous processing

The API can durably accept processing for an existing Work Item:

```text
POST /items/{id}/process
```

Acceptance creates the processing responsibility and transactional outbox state in PostgreSQL before returning:

```text
HTTP 202
```

RabbitMQ publication is performed asynchronously by the API-side outbox publisher.

This means broker availability is separated from durable request acceptance: an accepted processing request remains represented in PostgreSQL while the publisher retries delivery to RabbitMQ.

The worker is a separate process and can be run with:

```bash
./tools/work-items-worker-local run
```

Processing-job state can be read through:

~~~text
GET /processing-jobs/{id}
~~~

This endpoint is read-only. Repeated requests observe processing state without mutating it.

A typical asynchronous client flow is:

~~~text
POST /items/{id}/process
        ↓
HTTP 202 + processing job
        ↓
GET /processing-jobs/{job_id}
        ↓
accepted
        ↓
succeeded | failed
~~~

### Processing outcomes

Items begin pending. Explicit `done` creation returns `400 invalid_status`.
Successful processing persists title analysis and changes the item to done in
one transaction with the succeeded job. `GET /items` includes a `result` with
input title, analysis version, character/word counts, and producing job ID.

A second active request returns `409 processing_already_active` with
`processing_job_id`; completed items return `409 work_item_already_done`.
Neither creates another job/outbox row. The UI follows the active job on a
conflict and disables processing for done items. Failed jobs leave the item
pending and eligible for a new request. Accepted jobs have no automatic timeout;
restore delivery dependencies rather than create a replacement job.

## Web UI

The frontend lives under:

~~~text
apps/work-items/web/
~~~

With PostgreSQL, RabbitMQ, the API, and the worker running, start the frontend in another terminal:

~~~bash
cd apps/work-items/web
corepack enable
pnpm install --frozen-lockfile
pnpm dev
~~~

Open:

~~~text
http://localhost:5173
~~~

The UI supports:

- listing Work Items;
- creating a Work Item;
- starting asynchronous processing;
- observing processing-job state;
- polling until a job reaches `succeeded` or `failed`;
- loading, empty, and API/network failure states.

### Browser-to-API development boundary

The frontend calls API paths under:

~~~text
/api/*
~~~

During local development, Vite proxies those requests to:

~~~text
http://127.0.0.1:8080
~~~

For example:

~~~text
browser
  ↓
http://localhost:5173/api/items
  ↓
Vite development proxy
  ↓
http://127.0.0.1:8080/items
  ↓
Work Items API
~~~

A broad API CORS policy is deliberately not enabled solely for local development.

The Vite proxy keeps the browser-facing development flow behind the frontend origin while forwarding API traffic to the local Go process.

This is a local-development decision only. It does not define future production:

- ingress;
- TLS termination;
- DNS or hostname layout;
- CDN behavior;
- reverse-proxy topology;
- CORS policy.

The production browser/API boundary must be decided separately when a deployed environment requires it.

## Overrides

The local interface allows explicit overrides for experiments.

Example:

```bash
PORT=18080 VERSION=experiment-1 ./tools/work-items-api-local run
```

A different PostgreSQL connection can be supplied explicitly:

```bash
DATABASE_URL='postgres://user:password@127.0.0.1:5432/database?sslmode=disable' \
  ./tools/work-items-api-local run
```

Then verify the same instance:

```bash
PORT=18080 EXPECTED_VERSION=experiment-1 \
  ./tools/work-items-api-local verify
```

A different base URL can also be supplied directly:

```bash
BASE_URL=http://127.0.0.1:18080 \
EXPECTED_VERSION=experiment-1 \
  ./tools/work-items-api-local verify
```

## Cloud independence

The local workflow requires no:

- AWS account
- AWS credentials
- ECR repository
- ECS service
- Terraform state
- load balancer

PostgreSQL and RabbitMQ run in repository-owned local Docker labs.

This workflow is intentionally local-first. It does not claim parity with a cloud deployment environment that has not been implemented for the current Work Items architecture.

## Container path

The existing Dockerfile remains the container packaging path.

CI continues to build the image independently of the local workflow. The local helper does not replace or modify the container build.
