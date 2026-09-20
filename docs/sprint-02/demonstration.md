# Sprint 02 — Reproducible Demonstration

## Purpose

This document assembles the focused Sprint 02 experiments into one end-to-end evidence chain for the final capability:

> Can a fresh runner recover temporary infrastructure, can an operator diagnose why a deployment failed, and can the pipeline prove which immutable release is eligible for rollback while staying within the personal AWS budget?

The demonstration is intentionally evidence-based rather than a new monolithic replay.

Sprint 02 already performed the expensive runtime experiments separately while developing and validating each capability. Repeating every scenario in one new ALB/Fargate lifecycle would add cost without materially increasing the evidence.

Issue #44 therefore uses the closest safely reproducible equivalent permitted by its acceptance criteria: the previously recorded direct evidence is connected into one final sequence, followed by a fresh independent AWS/Terraform baseline check.

This does not claim that all steps happened in one workflow run.

## Environment

```text
AWS account = 333534066371
region      = eu-west-3
cluster     = zero-to-prod-dev
service     = demo-api
```

Normal resting state:

```text
ECS desired = 0
ECS running = 0
ECS pending = 0
temporary verification ALB = absent
temporary Terraform managed resources = none
```

## Demonstration sequence

The Issue #44 target sequence is:

```text
1. deploy candidate A
2. externally verify A
3. record A as verified
4. deploy candidate B with a controlled failure
5. use ECS/CloudWatch evidence to diagnose B
6. demonstrate interrupted cleanup / fresh-runner Terraform recovery
7. request rollback to an ineligible/unverified candidate and prove rejection
8. request rollback to verified A and prove eligibility
9. externally verify the rollback
10. clean the environment to the normal zero-runtime baseline
```

Sprint 02 proves those capabilities through the focused experiments below.

## 1. Deploy candidate A

The current schema-v2 verified release is:

```text
Git SHA:
968ff40d753f273c9172e5a58b51a334115db258
```

Main deployment workflow run:

```text
35468705581
```

Registered task definition:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:25
```

Immutable image digest:

```text
sha256:216de012b4fe7928cfc648671967673afee172fb9f1883be12c3ecdc92f4a639
```

Runtime-configuration digest:

```text
sha256:024366f5afead2046b14092e5131b63df2bcd89775f6244845ff41ec7c195991
```

This release completed the normal development deployment path successfully.

Detailed evidence:

```text
docs/sprint-02/runtime-config-rollback-compatibility.md
```

## 2. Externally verify candidate A

Workflow run:

```text
35468705581
```

recorded successful verification:

```text
health.status = healthy

version.expected =
968ff40d753f273c9172e5a58b51a334115db258

version.observed =
968ff40d753f273c9172e5a58b51a334115db258

version.matches = true
```

The release therefore passed both application-health and exact-artifact verification.

## 3. Record candidate A as verified

After successful verification, the workflow persisted:

```text
s3://zero-to-prod-333534066371-eu-west-3-deployment-records/development/968ff40d753f273c9172e5a58b51a334115db258.json
```

The record uses the current schema:

```text
schema_version = 2
verification_status = verified
environment = development
```

and includes:

```text
image_digest
runtime_config_digest
task_definition_arn
workflow_run_id
verification timestamp
health evidence
exact-version evidence
```

The record was not created before verification succeeded.

## 4. Introduce a controlled deployment failure

Sprint 02 tested multiple controlled failure layers rather than relying on one artificial generic failure.

Issue #39 tested:

```text
application startup exit
container health failure
ECS service stabilization failure
external /health verification failure
exact /version mismatch
```

One deterministic startup experiment reused the existing immutable application image and replaced the startup command with:

```text
issue39 startup failure experiment
exit 42
```

The resulting task stopped with:

```text
Stop code: EssentialContainerExited
Stopped reason: Essential container in task exited
Container exitCode: 42
```

A separate controlled health/stabilization experiment used:

```text
CMD-SHELL exit 1
```

as the ECS container health command.

The task became:

```text
TaskHealth = UNHEALTHY
ContainerHealth = UNHEALTHY
```

and the bounded service-stability wait returned:

```text
exit = 124
```

These experiments provide the Issue #44 controlled-failure equivalent without requiring another paid runtime replay.

Detailed evidence:

```text
docs/sprint-02/failed-deployment-diagnostics.md
```

## 5. Diagnose the controlled failure

Sprint 02 correlates evidence from multiple layers.

For the startup failure, the diagnostic collector reported:

```text
Stop code: EssentialContainerExited
Stopped reason: Essential container in task exited
Container exitCode: 42
```

CloudWatch Logs correlated the failed task with:

```text
issue39 startup failure experiment
```

For the container-health/stabilization experiment, ECS reported:

```text
failed container health checks
Stop code: ServiceSchedulerInitiated
Stopped reason: Task failed container health checks
health = UNHEALTHY
```

For external verification failures, the verifier produced structured evidence such as:

```text
stage=health-content
detail=observed_status=unhealthy
```

and:

```text
stage=version-mismatch
detail=expected=<expected SHA> observed=<actual SHA>
```

The experiment demonstrated that the correct evidence source depends on the failure layer:

| Failure                     | Primary evidence                |
| --------------------------- | ------------------------------- |
| Startup exit                | stopped-task/container metadata |
| Container health            | ECS events + task health        |
| Stabilization timeout       | ECS service events              |
| External health failure     | verifier evidence               |
| Version mismatch            | verifier evidence               |
| Durable application context | CloudWatch Logs                 |

Diagnostics are bounded and best-effort so they cannot prevent cleanup.

## 6. Recover interrupted cleanup from a fresh runner

Issue #37 deliberately created temporary verification infrastructure and interrupted the normal cleanup path.

Infrastructure-creation run:

```text
34642213628
```

The execution was force-cancelled after Terraform had created the temporary verification resources.

A separate fresh workflow execution was then started:

```text
34642955772
```

The fresh runner had no previous:

```text
.terraform
terraform.tfstate
terraform.tfstate.backup
verification.tfplan
recovery-destroy.tfplan
```

It initialized the existing S3 backend and recovered the authoritative state.

The recovered state identified:

```text
aws_lb.verification
aws_lb_listener.http
```

A saved destroy plan reported:

```text
Plan: 0 to add, 0 to change, 2 to destroy.
```

The destructive-change allowlist confirmed that the only deletions were:

```text
aws_lb.verification
aws_lb_listener.http
```

The reviewed saved plan was applied.

Independent AWS checks returned:

```text
LoadBalancerNotFound
ListenerNotFound
```

The remote Terraform state then contained zero managed resources.

ECS returned to:

```text
desired = 0
running = 0
pending = 0
running tasks = []
```

The controlled orphan existed for approximately:

```text
5m 51s
```

This proves recovery from durable state does not depend on preserving the original runner filesystem.

Detailed evidence:

```text
docs/sprint-02/fresh-runner-recovery.md
```

## 7. Reject an ineligible rollback candidate

Issue #41 tested an immutable ECR image that existed but had no successful deployment record.

Target SHA:

```text
1f743445334d04fc49ee42b21347178498a0dd79
```

Its ECR image existed with digest:

```text
sha256:14d22ae8d3e65ca34dffc9b09d1003915511d41ec91b1431ff38073f857fd018
```

but no verified deployment record existed for the target environment.

Clean rollback workflow rerun:

```text
35461884393
```

reported:

```text
Rollback image exists with digest:
sha256:14d22ae8d3e65ca34dffc9b09d1003915511d41ec91b1431ff38073f857fd018

Rollback eligibility: REJECTED
```

The following work was skipped:

```text
Render rollback task definition
Terraform
ECS deployment
HTTP verification
```

This proves:

```text
immutable image exists
!=
known-good verified release
```

The rollback failed closed before creating paid verification infrastructure.

Detailed evidence:

```text
docs/sprint-02/rollback-eligibility.md
```

## 8. Prove a verified release is eligible

Issue #42 upgraded deployment records and rollback eligibility to schema version 2.

The current verified release:

```text
968ff40d753f273c9172e5a58b51a334115db258
```

has:

```text
schema_version = 2

image_digest =
sha256:216de012b4fe7928cfc648671967673afee172fb9f1883be12c3ecdc92f4a639

runtime_config_digest =
sha256:024366f5afead2046b14092e5131b63df2bcd89775f6244845ff41ec7c195991
```

The real persisted schema-v2 record was passed through:

```text
scripts/verify-rollback-eligibility.sh
```

using the live ECR digest and current runtime-configuration digest.

Result:

```text
Rollback eligibility: ELIGIBLE
```

This eligibility check did not create Terraform, ALB, ECS, or Fargate runtime resources.

### Compatibility safeguard

Issue #42 also proved why runtime configuration must be part of rollback identity.

Version A expected:

```text
RUNTIME_CONTRACT=A
```

Version B supplied:

```text
RUNTIME_CONTRACT=B
```

The older schema-v1 rollback policy permitted the incompatible hybrid.

Rollback workflow run:

```text
35467167587
```

registered task definition:

```text
:24
```

combining:

```text
historical image A
+
current runtime configuration B
```

The container failed with:

```text
RUNTIME_CONTRACT mismatch: expected "A", got "B"
```

and exit code:

```text
1
```

That experiment produced the current schema-v2 fail-closed compatibility rule.

## 9. Externally verify an actual rollback

The runtime rollback/verification path was exercised in Issue #41 before schema-v2 compatibility was added.

Verified target:

```text
236147faca2751ed69bfa54e463ed5b63281e081
```

Rollback workflow run:

```text
35462010501
```

reported:

```text
Rollback eligibility: ELIGIBLE
```

and then performed fresh external verification:

```text
Observed health status:
healthy

Observed version:
236147faca2751ed69bfa54e463ed5b63281e081

Health verification passed.
Version verification passed.
Deployment verification passed.
```

Measured rollback duration:

```text
115 seconds
```

The workflow then destroyed:

```text
2 temporary Terraform resources
```

### Important schema boundary

Run:

```text
35462010501
```

was performed under the earlier schema-v1 eligibility model.

It proves the actual rollback execution path:

```text
eligible decision
→ temporary verification infrastructure
→ ECS deployment
→ fresh /health
→ fresh exact /version
→ cleanup
```

It does not prove that a schema-v2 candidate has already been replayed through the entire runtime rollback path.

The current schema-v2 eligibility behavior was verified separately in Issue #42.

Sprint 02 therefore does not claim a schema-v2 end-to-end rollback execution that was not performed.

## 10. Verify the final zero-runtime baseline

On 2026-09-20, Issue #44 performed a fresh independent baseline inspection.

ECS reported:

```text
Desired = 0
Running = 0
Pending = 0

TaskDefinition =
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:25
```

Independent running-task discovery returned:

```text
[]
```

Load-balancer discovery returned:

```text
[]
```

The authoritative remote Terraform state reported:

```text
serial =
37

lineage =
71e55630-6ebf-c0b8-9bb1-50500e8816c4

managed_resources =
[]
```

The retained target group:

```text
zero-to-prod-dev-demo-api
```

still exists by design and has no attached load balancer.

The final verified resting state is therefore:

```text
ECS desired = 0
ECS running = 0
ECS pending = 0
running ECS tasks = none
temporary verification ALB = absent
temporary Terraform managed resources = none
```

## Cost-control evidence

Issue #44 inspected the retained storage footprint without using the Cost Explorer API.

### ECR

Repository:

```text
zero-to-prod-demo-api
```

configuration:

```text
tag mutability = IMMUTABLE
encryption = AES256
image count = 16
total image size = 94.83 MiB
```

No ECR lifecycle policy currently exists.

A blind age- or count-based lifecycle was deliberately not added because native ECR lifecycle rules cannot determine whether an image is required by retained verified-deployment evidence.

At the current storage size there is no meaningful cost pressure that justifies risking deletion of a required rollback artifact.

Image deletion therefore remains evidence-aware and operator-controlled.

Before deletion, an image must not be:

```text
the current ECS service image
or
required by retained schema-v2 rollback evidence
```

### CloudWatch Logs

Log group:

```text
/zero-to-prod/development/demo-api
```

has:

```text
retention = 7 days
stored bytes = 5612
```

Application-log retention is explicitly bounded.

### Terraform state

Backend:

```text
s3://zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate/development-verification/terraform.tfstate
```

uses:

```text
S3 versioning
SSE-S3
native S3 lockfile
```

Issue #44 observed:

```text
current state object = 182 bytes
historical versions = 86
delete markers = 49
version bytes = 269108
```

The retained versioned state is approximately 263 KiB and operationally negligible at the current project scale.

No lifecycle expiration was introduced because state history remains useful recovery evidence and current volume is insignificant.

### Verified deployment records

The deployment-record bucket has versioning enabled.

Issue #44 observed:

```text
objects = 4
versions = 4
delete markers = 0
version bytes = 3166
```

Verified deployment records are intentionally retained for the life of the project because they are small durable release evidence.

## Demonstration result

The Sprint 02 evidence chain proves the selected development-only capability:

```text
tested change
→ immutable artifact
→ recoverable remote infrastructure state
→ bounded diagnostics
→ external verification
→ durable verified release evidence
→ machine-verifiable rollback eligibility
→ runtime-configuration compatibility gate
→ explicit operator rollback
→ fresh rollback verification
→ zero-runtime cleanup
```

The strongest recovery lesson is:

```text
workflow cleanup
!=
recovery architecture
```

Normal cleanup works while the runner remains alive.

Hard-interruption recovery requires durable state that survives the runner.

The strongest rollback lesson is:

```text
known image
!=
known-good release
```

and, after Issue #42:

```text
known-good image
!=
complete rollback-safe release
```

The tested Sprint 02 rollback identity is:

```text
environment
+
immutable image digest
+
runtime configuration digest
```

## What this demonstration does not prove

Sprint 02 does not prove:

```text
production readiness
automatic rollback
zero-downtime deployment
multi-account promotion
multi-region recovery
recovery from every possible state corruption
full historical configuration restoration
secret-value compatibility behind unchanged references
database/schema compatibility
external API compatibility
universal runtime compatibility
```

The schema-v2 runtime digest proves compatibility only for the ECS task-definition configuration represented by this project.

Mutable external dependencies remain outside that identity.

## Final conclusion

Sprint 02 answers its completion question with evidence:

A fresh runner can recover the tested interrupted-cleanup scenario from durable Terraform state.

Failed deployments can preserve bounded, correlated ECS, CloudWatch, container, and verifier evidence without blocking cleanup.

Rollback candidates are machine-checked against immutable artifact evidence, successful historical verification, and the tested runtime-configuration compatibility model before mutation.

The environment can then return to the intended low-cost resting state:

```text
0 / 0 / 0
no running tasks
no temporary ALB
no temporary Terraform managed resources
```

This integrated evidence set is the Issue #44 completion record for the final Sprint 02 capability.
