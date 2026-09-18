# Failed deployment diagnostics

## Issue

GitHub Issue #39 — Improve failed-deployment diagnostics in CI.

## Goal

Make failed development deployments explain what happened without requiring
manual reconstruction after ECS task metadata has disappeared.

The deployment workflow should correlate:

- the expected Git SHA;
- the expected ECS task definition;
- ECS service events;
- stopped-task metadata;
- container status, health, exit code, and reason;
- recent application logs from CloudWatch;
- external `/health` and `/version` verification evidence.

Diagnostics must remain bounded and best-effort so they never prevent cleanup.

## Implementation

Issue #39 adds `scripts/collect-deployment-diagnostics.sh` and wires it into the
development deployment workflow before the existing cleanup steps.

The collector records a concise Markdown summary containing:

- failure stage;
- expected Git SHA;
- expected task-definition ARN;
- ECS service desired/running/pending state;
- up to 10 recent ECS service events;
- up to 5 correlated ECS tasks;
- stopped-task and container diagnostics;
- bounded external verifier evidence;
- up to 20 CloudWatch application log events per correlated task.

The collector always exits successfully because diagnostic collection must not
replace or block deployment cleanup.

The workflow also records structured evidence from
`scripts/verify-deployment.sh` for external verification failures.

Examples:

```text
stage=health-content
detail=observed_status=unhealthy
```

```text
stage=version-mismatch
detail=expected=<expected-sha> observed=<observed-sha>
```

Arbitrary HTTP response bodies are not copied into the durable diagnostic
summary.

## Bounded collection

AWS diagnostic calls are bounded with:

- outer command timeout: 15 seconds;
- AWS CLI connection timeout: 3 seconds;
- AWS CLI read timeout: 5 seconds.

The GitHub Actions diagnostic step also has:

```yaml
continue-on-error: true
timeout-minutes: 3
```

Task, event, and log collection are explicitly capped.

This prevents a failed diagnostic API call from turning a deployment failure
into a stuck cleanup path.

## Least-privilege IAM

The GitHub Actions deployment role received only the additional reads required
for diagnostics:

- `ecs:ListTasks` constrained to the development cluster;
- `ecs:DescribeTasks` constrained to tasks in the development cluster;
- `logs:FilterLogEvents` constrained to
  `/zero-to-prod/development/demo-api`.

AWS IAM simulation verified the intended resources are allowed and unrelated
clusters/log groups remain implicitly denied.

`FilterLogEvents` was selected instead of `GetLogEvents` after IAM simulation
showed that the attempted stream-level `GetLogEvents` resource model did not
produce the intended authorization boundary.

## Sensitive-output handling

The diagnostics collector deliberately selects only the ECS fields required for
failure analysis.

It does not print:

- task-definition environment variables;
- task-definition secrets;
- AWS CLI stderr collected during failed diagnostic calls;
- arbitrary HTTP verifier response bodies.

Application log messages are included because application logs are one of the
required evidence sources. They therefore remain subject to the application's
normal no-secrets logging policy.

## Failure experiments

### 1. Application exits during startup

A temporary task-definition revision (`:16`) reused the existing immutable
application image but replaced the entrypoint with a controlled command:

```text
issue39 startup failure experiment
exit 42
```

Observed diagnostics:

```text
Stop code: EssentialContainerExited
Stopped reason: Essential container in task exited
Container exitCode: 42
```

CloudWatch correlated the exact failed task stream and returned:

```text
issue39 startup failure experiment
```

Best evidence source:

**Stopped-task/container metadata**, supported by CloudWatch application logs.

This experiment proved that startup failures can be correlated before ECS task
metadata expires.

### 2. Container health check fails

Temporary revision `:17` reused the same immutable image and changed only the
ECS container health command to:

```text
CMD-SHELL exit 1
```

The exact `:17` task was observed as:

```text
TaskHealth: UNHEALTHY
ContainerHealth: UNHEALTHY
```

The collector also found a stopped task with:

```text
Stop code: ServiceSchedulerInitiated
Stopped reason: Task failed container health checks
```

The application itself continued to run and emit its normal startup log, which
separated application process health from ECS container-health evaluation.

Best evidence source:

**ECS service events and task/container health metadata**.

### 3. ECS service fails stabilization

Revision `:17` was deployed again and the ECS `services-stable` waiter was
bounded with an external timeout.

Observed:

```text
Waiter exit: 124
```

The collector immediately showed why stabilization could not complete:

```text
failed container health checks
Stop code: ServiceSchedulerInitiated
Stopped reason: Task failed container health checks
health=UNHEALTHY
```

Best evidence source:

**ECS service events**, supported by stopped-task/container metadata.

This also demonstrated why the waiter result alone is insufficient: the
diagnostic evidence explains the reason the service did not stabilize.

### 4. External `/health` verification fails

The known-good revision `:15` was running and ECS had reached steady state.

A temporary ALB listener rule intercepted only `/health` and returned:

```json
{"status":"unhealthy"}
```

with HTTP `200`.

`/version` continued to return the real immutable application version.

The verifier failed with:

```text
stage=health-content
detail=observed_status=unhealthy
```

At the same time the collector showed:

```text
Desired: 1
Running: 1
Pending: 0
Container health: HEALTHY
```

Best evidence source:

**External verifier evidence**.

This experiment isolated application-level response validation from ECS,
container, and ALB target health.

### 5. Exact `/version` verification fails

After removing the temporary `/health` listener rule, the baseline returned to:

```text
/health  -> {"status":"healthy"} HTTP 200
/version -> 26949137bb283b5e382fabbf044a62496bf3b43e HTTP 200
```

The verifier was intentionally given:

```text
0000000000000000000000000000000000000000
```

as the expected SHA.

Health verification passed first, then exact version verification failed:

```text
stage=version-mismatch
detail=expected=0000000000000000000000000000000000000000 observed=26949137bb283b5e382fabbf044a62496bf3b43e
```

The collector simultaneously showed a healthy running `:15` ECS task.

Best evidence source:

**External verifier evidence**.

## Mistakes and knowledge gaps found during the experiments

### Selecting the wrong task during an ECS deployment transition

The first container-health experiment selected the first running service task.

During a deployment transition that task was still revision `:15`, so it
correctly remained healthy even though revision `:17` had a failing health
check.

The experiment was corrected to correlate tasks by exact
`taskDefinitionArn`, not merely by service and running state.

### Assuming BusyBox provided `httpd`

An attempted external-health experiment used a temporary revision (`:18`) that
tried to start:

```text
busybox httpd
```

The runtime image did not provide that applet.

The actual evidence was:

```text
Stop code: EssentialContainerExited
Exit code: 127
CloudWatch: httpd: applet not found
```

This was not counted as the external `/health` experiment.

The final experiment instead injected the external response at the temporary
ALB listener, which isolated the verifier without modifying the application
container.

### ECS waiter state includes deployment reconciliation

A service can already have desired/running/pending counts at zero while ECS is
still reconciling older deployment sets and draining targets.

A bounded waiter timed out during one intermediate attempt even though the
later restored service eventually settled successfully.

This reinforced that service counts alone are not equivalent to ECS
`services-stable`.

### Temporary ALB lifecycle

The first experiments repeatedly created and destroyed the verification ALB.

That was safe but unnecessarily repeated partial-hour ALB usage.

Later experiments reused one temporary ALB for multiple checks before destroying
it once.

Future failure experiments should batch verification scenarios during one
temporary-infrastructure lifecycle when practical.

## Cleanup verification

After the final experiment:

```text
TargetGroup.LoadBalancerArns = []
```

The ECS service returned to:

```text
TaskDefinition = zero-to-prod-demo-api:15
Desired        = 0
Running        = 0
Pending        = 0
```

All temporary verification ALBs/listeners were destroyed.

Temporary task-definition revisions remain harmless metadata and do not run
compute unless referenced by a running task/service.

## Acceptance criteria

- [x] ECS service events collected.
- [x] Stopped-task reason collected.
- [x] Container exit code/reason collected.
- [x] Recent CloudWatch logs correlated to failed tasks.
- [x] Expected Git SHA recorded.
- [x] Expected task-definition ARN recorded.
- [x] Diagnostic collection is bounded.
- [x] Diagnostic failures cannot prevent cleanup.
- [x] Structured external-verification evidence collected.
- [x] Arbitrary verifier response bodies excluded from durable summaries.
- [x] Diagnostic summary written to `GITHUB_STEP_SUMMARY`.
- [x] Startup-exit experiment completed.
- [x] ECS stabilization-failure experiment completed.
- [x] Container-health experiment completed.
- [x] External `/health` experiment completed.
- [x] Exact `/version` experiment completed.
- [x] Final AWS resting state verified.

## Learning summary

The most useful diagnostic source depends on the failure layer:

| Failure | Best evidence |
| --- | --- |
| Application exits at startup | stopped-task/container metadata |
| Container health check fails | ECS events + task health |
| Service fails stabilization | ECS service events |
| External `/health` fails | verifier evidence |
| Exact `/version` fails | verifier evidence |

CloudWatch application logs are the durable supporting layer when ECS task
metadata later expires.

The important design lesson is that deployment diagnostics should correlate
multiple layers rather than treating a single signal as authoritative.

## Focused time

Approximately `~2h 30m`, including implementation review, IAM validation,
controlled AWS failure experiments, troubleshooting, and cleanup.

## Next experiment

Use these diagnostics during a real CI-triggered deployment failure and confirm
the GitHub Actions job summary contains the same correlated evidence before the
existing cleanup steps execute.
