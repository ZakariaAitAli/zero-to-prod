# ECS application logging to CloudWatch

## Objective

Issue #38 adds a bounded durable application-log path for the development
`demo-api`.

Before this change, ECS exposed task and service state, but application
stdout/stderr disappeared with the task.

The target path is:

~~~
demo-api stdout/stderr
        |
        v
ECS awslogs driver
        |
        v
/zero-to-prod/development/demo-api
        |
        v
7-day CloudWatch Logs retention
~~~

This is intentionally not a full observability platform.

Issue #38 provides bounded application logs that later deployment-diagnostic
work can correlate with ECS and external-verification evidence.

## Starting state

Repository baseline:

~~~
main = 27b03e3
~~~

AWS account and region:

~~~
account = 333534066371
region  = eu-west-3
~~~

Initial ECS service state:

~~~
task definition = zero-to-prod-demo-api:11
desired         = 0
running         = 0
pending         = 0
~~~

No `/zero-to-prod/` CloudWatch Logs log group existed.

Task definition revision `11` had:

~~~
logConfiguration = null
~~~

The ECS execution role was:

~~~
zero-to-prod-ecs-task-execution
~~~

and initially had only one inline policy:

~~~
zero-to-prod-ecr-pull
~~~

No AWS-managed policies were attached.

## Architecture

### Log group

The dedicated development log group is:

~~~
/zero-to-prod/development/demo-api
~~~

The name intentionally identifies both:

- environment: `development`
- service: `demo-api`

Retention is explicitly configured to:

~~~
7 days
~~~

Observed configuration after creation:

~~~
class         = STANDARD
retentionDays = 7
storedBytes   = 0
~~~

The log group is durable across individual ECS tasks but intentionally retains
events for only seven days.

### Terraform ownership boundary

The existing `infra/terraform/development-verification` state continues to own
only temporary verification-lifecycle infrastructure.

The CloudWatch log group is not added to that Terraform state.

This preserves the Sprint 02 architecture boundary established before Issue
#38 rather than broadening the verification-infrastructure state simply because
Terraform is available.

### ECS task definition

The `demo-api` task definition uses:

~~~json
{
  "logDriver": "awslogs",
  "options": {
    "awslogs-group": "/zero-to-prod/development/demo-api",
    "awslogs-region": "eu-west-3",
    "awslogs-stream-prefix": "ecs"
  }
}
~~~

For a container named `demo-api`, the resulting stream shape is:

~~~
ecs/demo-api/<task-id>
~~~

`awslogs-create-group` is deliberately not enabled.

The log group must therefore exist before a task starts.

This prevents the execution role from needing permission to create arbitrary
CloudWatch log groups.

## IAM design

The ECS task execution role is responsible for the `awslogs` integration.

The application does not need CloudWatch Logs API credentials.

The new inline policy grants only:

~~~
logs:CreateLogStream
logs:PutLogEvents
~~~

against:

~~~
arn:aws:logs:eu-west-3:333534066371:log-group:/zero-to-prod/development/demo-api:*
~~~

The role is not allowed to create log groups.

The existing ECR pull policy remains separate and unchanged.

The execution role therefore has two independent inline policies:

~~~
zero-to-prod-ecr-pull
zero-to-prod-cloudwatch-logs
~~~

No AWS-managed policy was added.

IAM Access Analyzer validation returned:

~~~json
[]
~~~

## Successful logging experiment

A logging-enabled task definition was registered as:

~~~
zero-to-prod-demo-api:12
~~~

It reused the exact immutable image already referenced by revision `11`:

~~~
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:13bd2369f083c7a5bef7bc2b4eba7a4a1f1d5848
~~~

The retained ECS service itself was not modified and remained on revision `11`
with desired count `0`.

A single standalone Fargate task was launched with revision `12`.

Task:

~~~
beedb3cc10684833ae1403e5fbf86c18
~~~

The task reached:

~~~
RUNNING
~~~

CloudWatch created:

~~~
ecs/demo-api/beedb3cc10684833ae1403e5fbf86c18
~~~

The application startup message was retained:

~~~
2026/09/17 21:34:09 demo-api version=13bd2369f083c7a5bef7bc2b4eba7a4a1f1d5848 listening on :8080
~~~

The task was then explicitly stopped to keep runtime bounded.

The service itself remained unchanged.

This proves that the real Fargate `awslogs` path can create a stream and retain
application stderr/stdout using the least-privilege execution-role policy.

A final deployment-workflow proof remains required after the implementation is
merged.

## Failure experiment 1: missing CloudWatch Logs permission

### Expectation

A task whose execution role can pull the ECR image but cannot create or write
CloudWatch log streams should fail before application startup.

### Isolation

A temporary execution role was created:

~~~
zero-to-prod-issue-38-no-logs
~~~

It had only:

~~~
zero-to-prod-ecr-pull
~~~

It had no CloudWatch Logs permissions and no attached managed policies.

A temporary task definition was registered as revision `13`.

The retained ECS service remained on revision `11` with desired count `0`.

### Observation

Task:

~~~
8963d636389c404a81c07ef34f1a306b
~~~

Result:

~~~
stopCode = TaskFailedToStart
~~~

The stopped reason contained:

~~~
ResourceInitializationError
AccessDeniedException
logs:CreateLogStream
~~~

AWS reported that the assumed temporary execution role was not authorized to
create:

~~~
arn:aws:logs:eu-west-3:333534066371:
log-group:/zero-to-prod/development/demo-api:
log-stream:ecs/demo-api/8963d636389c404a81c07ef34f1a306b
~~~

No log stream was created:

~~~json
[]
~~~

This distinguishes an execution-role authorization failure from an application
failure.

### Cleanup

The temporary inline policy and role were deleted.

Task definition revision `13` was deregistered and became:

~~~
INACTIVE
~~~

## Failure experiment 2: incorrect log group

### Expectation

If `awslogs` references a nonexistent log group while the execution role has
the required stream permissions, task startup should fail because the logging
resource does not exist.

The task should not silently create the missing group because
`awslogs-create-group` is not enabled.

### Isolation

The deliberately incorrect group was:

~~~
/zero-to-prod/development/demo-api-missing
~~~

It was independently confirmed absent before the experiment.

A temporary execution role was created:

~~~
zero-to-prod-issue-38-wrong-log-group
~~~

It had:

~~~
zero-to-prod-ecr-pull
issue-38-wrong-log-group
~~~

The temporary logging policy granted stream writes only under the nonexistent
experiment group.

Task definition revision `14` referenced that group.

The retained ECS service remained unchanged.

### Observation

Task:

~~~
97ddb043b8f945aba164f7d029be0df0
~~~

Result:

~~~
stopCode = TaskFailedToStart
~~~

The stopped reason contained:

~~~
ResourceInitializationError
CreateLogStream
ResourceNotFoundException
The specified log group does not exist.
~~~

After the experiment, the wrong group was still absent:

~~~json
[]
~~~

This proves that missing logging infrastructure produces a distinct
configuration failure and that ECS did not auto-create the group.

### Cleanup

Both temporary inline policies and the temporary role were deleted.

Task definition revision `14` was deregistered and became:

~~~
INACTIVE
~~~

## Failure experiment 3: application emits an error before exiting

### Expectation

With logging infrastructure and IAM working normally, an application failure
should be distinguishable from an ECS logging initialization failure and the
application error should survive task termination.

### Method

No application code was modified.

Revision `12` was used with a one-task environment override:

~~~
PORT=not-a-port
~~~

The existing application logs its attempted listen address and terminates with
an error when `http.ListenAndServe` cannot use the invalid port.

### Observation

Task:

~~~
8b5177f913ab455bbe78828d5c05ad35
~~~

The task reached `RUNNING` and later stopped with:

~~~
stopCode      = EssentialContainerExited
stoppedReason = Essential container in task exited
exitCode      = 1
~~~

CloudWatch retained:

~~~
2026/09/17 21:59:06 demo-api version=13bd2369f083c7a5bef7bc2b4eba7a4a1f1d5848 listening on :not-a-port
~~~

followed immediately by:

~~~
2026/09/17 21:59:06 server stopped: listen tcp: lookup tcp/not-a-port: unknown port
~~~

This is application-layer evidence rather than ECS resource-initialization
evidence.

## Failure-layer distinction

| Scenario | ECS result | Durable application log |
| --- | --- | --- |
| Missing Logs permission | `TaskFailedToStart` / `AccessDeniedException` | No |
| Missing log group | `TaskFailedToStart` / `ResourceNotFoundException` | No |
| Application startup/runtime error | `EssentialContainerExited`, exit `1` | Yes |

This demonstrates why ECS service/task state and application logs answer
different diagnostic questions.

## Application logs versus other signals

### Application logs

Application stdout/stderr describes what the process itself observed.

Examples from this issue include:

~~~
demo-api ... listening on :8080
server stopped: ... unknown port
~~~

### ECS service events and task state

ECS control-plane state explains whether tasks were placed, started, stopped,
or failed initialization.

Examples include:

~~~
TaskFailedToStart
ResourceInitializationError
EssentialContainerExited
~~~

ECS task state does not replace application logs.

### Container health

Container health evaluates the configured health command inside the task.

For this service, health checks `/health`.

A healthy container establishes a narrow local application-health property; it
does not establish external reachability or exact artifact identity.

### External verification

External verification tests the deployed application through the temporary
verification path.

The project separately checks:

~~~
/health
/version
~~~

from outside the task.

Application logging, ECS state, container health, and external verification
therefore provide distinct evidence.

## IAM simulator knowledge gap

During implementation, IAM simulation was initially used as a pre-runtime
permission check.

`simulate-principal-policy` unexpectedly returned:

~~~
implicitDeny
~~~

for the intended CloudWatch actions.

A `simulate-custom-policy` control using:

~~~json
"Resource": "*"
~~~

also returned:

~~~
implicitDeny
~~~

with no matched statements.

This disproved the assumption that the simulator result could be treated as
authoritative evidence for this CloudWatch Logs integration.

The policy itself passed IAM Access Analyzer validation, and the definitive
runtime experiment then proved that the real ECS/Fargate execution role could
create the intended stream and publish log events.

The correction was:

> Do not broaden a least-privilege policy merely to satisfy a misleading
> simulation result. Validate the policy statically, then use a bounded real
> service experiment when runtime behavior is the property being tested.

## Cost controls

Cost controls for Issue #38 are:

- no external observability platform;
- no always-running compute;
- ECS resting desired count remains `0`;
- one dedicated log group;
- 7-day retention;
- only bounded standalone Fargate experiments;
- no automatic log-group creation;
- no high-volume request or logging workload.

Observed CloudWatch log volume is extremely small:

- successful experiment: one retained event;
- application-error experiment: two retained events;
- initialization failures produced no application streams.

Immediately after the experiments CloudWatch reported:

~~~
storedBytes = 0
~~~

for this very small and recently ingested volume.

That value is recorded as an observed API field, not treated as a precise
real-time byte measurement for the individual events.

Expected recurring/usage cost is therefore very small for this development
environment, but the final billed amount must be checked through the AWS Cost
Explorer console after billing data has ingested.

The Cost Explorer API is not used for project cost checks.

## Final AWS state after experiments

Retained ECS service:

~~~
task definition = zero-to-prod-demo-api:11
desired         = 0
running         = 0
pending         = 0
~~~

Running tasks:

~~~json
[]
~~~

Intentional durable resource:

~~~
/zero-to-prod/development/demo-api
retention = 7 days
~~~

Retained streams:

~~~
ecs/demo-api/beedb3cc10684833ae1403e5fbf86c18
ecs/demo-api/8b5177f913ab455bbe78828d5c05ad35
~~~

Temporary experiment IAM roles were deleted.

Temporary failure task-definition revisions `13` and `14` were deregistered.

Revision `12` remains available as the logging-enabled task-definition
experiment revision.

## Acceptance status before merge

| Requirement | Evidence | Status |
| --- | --- | --- |
| Task definition uses `awslogs` | Repository template and revision `12` | Pass |
| Dedicated log group exists | `/zero-to-prod/development/demo-api` | Pass |
| Retention explicitly configured | `7` days | Pass |
| Execution role has only required logging permissions | `CreateLogStream` + `PutLogEvents`, scoped to one group | Pass |
| Application stdout/stderr reaches CloudWatch | Standalone revision `12` experiment | Pass |
| Environment/service naming is understandable | group + `ecs/demo-api/<task-id>` streams | Pass |
| Logging architecture and IAM documented | This document | Pass |
| Expected recurring/usage cost recorded | Bounded-volume expectation documented | Pass |
| Missing logging permission experiment | `AccessDeniedException` | Pass |
| Incorrect log group experiment | `ResourceNotFoundException` | Pass |
| Application error before exit | exit `1` plus retained fatal log | Pass |
| Actual deployment workflow proves committed logging path | Post-merge experiment still required | Pending |
| Final billed log/runtime cost reviewed in Cost Explorer console | Billing ingestion/review still required | Pending |

## Next experiment

Issue #39 should build on these bounded durable logs and correlate them with
ECS task/service evidence and external verifier evidence so a failed deployment
reports the failed layer without allowing diagnostics to block cleanup.

## Focused time

To be recorded when Issue #38 implementation and post-merge validation are
complete.
