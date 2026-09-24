# Local demo API development

The demo API can be developed and verified locally with the repository-owned PostgreSQL lab and without AWS credentials or cloud infrastructure.

## Prerequisites

Required for the local workflow:

- Go 1.27.0 or newer (Go 1.27.1 preferred by `go.mod`)
- Bash
- curl
- Docker with Docker Compose

`test` and `build` remain native Go operations.

`run` and dependency-aware `verify` require the local PostgreSQL lab because the application now has a critical runtime datastore dependency.

## Local interface

Run commands from the repository root.

### Test

```bash
./tools/demo-api-local test
```

### Build

```bash
./tools/demo-api-local build
```

The default binary is written outside the repository:

```text
/tmp/zero-to-prod-demo-api
```

### Run

Start PostgreSQL and apply migrations explicitly before starting the API:

```bash
./tools/postgres-local start
./tools/postgres-local build-migrate
./tools/postgres-local migrate-up
./tools/demo-api-local run
```

Defaults:

```text
PORT=8080
VERSION=local
DATABASE_URL=postgres://zero_to_prod_app:zero-to-prod-local-app@127.0.0.1:55432/zero_to_prod?sslmode=disable
```

The default database credentials are development-only credentials from the local lab.

The run command builds the application before starting it. It does not run migrations.

### Verify

With the API running in another terminal:

```bash
./tools/demo-api-local verify
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

The AWS development target group is Terraform-owned and uses `/ready` for its load balancer health check.

The ECS container health check remains on `/health`, while load-balancer routing uses `/ready` so traffic acceptance reflects application readiness.

Deployment verification also requires `/ready` to report `ready` before a deployment can be considered verified.

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

## Overrides

The local interface allows explicit overrides for experiments.

Example:

```bash
PORT=18080 VERSION=experiment-1 ./tools/demo-api-local run
```

A different PostgreSQL connection can be supplied explicitly:

```bash
DATABASE_URL='postgres://user:password@127.0.0.1:5432/database?sslmode=disable' \
  ./tools/demo-api-local run
```

Then verify the same instance:

```bash
PORT=18080 EXPECTED_VERSION=experiment-1 \
  ./tools/demo-api-local verify
```

A different base URL can also be supplied directly:

```bash
BASE_URL=http://127.0.0.1:18080 \
EXPECTED_VERSION=experiment-1 \
  ./tools/demo-api-local verify
```

## Cloud independence

The local workflow requires no:

- AWS account
- AWS credentials
- ECR repository
- ECS service
- Terraform state
- load balancer

PostgreSQL runs only in the local Docker lab.

This workflow is intended for fast local application and dependency feedback. It does not emulate AWS or claim behavioral parity with the cloud deployment environment.

## Deployment boundary

The repository records the current application deployment compatibility in:

```text
apps/demo-api/deployment-mode
```

The current value is:

```text
local-only
```

This is deliberate. The application now requires PostgreSQL at runtime, but the existing AWS development task definition does not provide a PostgreSQL dependency or `DATABASE_URL`.

While the mode is `local-only`, CI still performs the required application and workflow validation but forces publish/deploy eligibility to `false`.

A future change that provisions and verifies the required cloud database/runtime configuration can explicitly move this mode to `cloud-ready`. Normal application changes then retain their existing publish/deploy behavior.

This issue does not provision AWS database infrastructure.

Expected additional cloud infrastructure cost for this local-first increment: `$0`.

## Container path

The existing Dockerfile remains the container packaging path.

CI continues to build the image independently of the local workflow. The local helper does not replace or modify the container build.
