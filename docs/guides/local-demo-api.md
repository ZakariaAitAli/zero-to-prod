# Local demo API development

The demo API can be developed and verified locally without AWS credentials or cloud infrastructure.

## Prerequisites

Required for the native workflow:

- Go 1.24.2
- Bash
- curl

Docker remains available for the container build path, but it is not required for the native development loop.

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

```bash
./tools/demo-api-local run
```

Defaults:

```text
PORT=8080
RUNTIME_CONTRACT=B
VERSION=local
```

The run command builds the application before starting it.

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

`GET /ready` is the traffic-acceptance signal. Readiness starts false, becomes true after the HTTP listener has successfully bound, and becomes false before graceful shutdown begins.

When the application is not ready, `/ready` returns:

```text
HTTP 503
{"status":"not_ready"}
```

The current AWS development target group still uses `/health` for its load balancer health check. That target group is not yet managed by Terraform in this repository, so this guide does not claim that `/ready` currently controls AWS traffic routing.

Deployment verification does require `/ready` to report `ready` before a deployment can be considered verified.

## Overrides

The local interface allows explicit overrides for experiments.

Example:

```bash
PORT=18080 VERSION=experiment-1 ./tools/demo-api-local run
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

The runtime contract can be overridden deliberately for failure experiments:

```bash
RUNTIME_CONTRACT=A ./tools/demo-api-local run
```

The current application is expected to reject that configuration.

## Cloud independence

The native local workflow requires no:

- AWS account
- AWS credentials
- ECR repository
- ECS service
- Terraform state
- load balancer

This workflow is intended for fast local application feedback. It does not emulate AWS or claim behavioral parity with the cloud deployment environment.

## Container path

The existing Dockerfile remains the container packaging path.

CI continues to build the image independently of the native local workflow. The local helper does not replace or modify the container build.
