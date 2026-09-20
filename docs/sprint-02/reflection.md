# Sprint 02 — Reflection

## Sprint theme

Sprint 02 focused on:

> Recoverable, Observable, and Cost-Controlled Deployments

The goal was not to add more infrastructure.

The goal was to make the existing development deployment path answer three operational questions with evidence:

```text
Can a fresh runner recover temporary infrastructure?

Can an operator diagnose why a deployment failed?

Can the pipeline prove which immutable release is eligible for rollback?
```

while keeping the personal AWS cost deliberately low.

The work remained development-only and does not claim production readiness.

## What Sprint 02 delivered

Sprint 02 extended the Sprint 01 deployment path with:

```text
remote Terraform state
+
native S3 state locking
+
fresh-runner recovery
+
CloudWatch application logging
+
failed-deployment diagnostics
+
durable verified-deployment records
+
machine-verifiable rollback eligibility
+
runtime-configuration compatibility checks
+
change-aware CI
```

The resulting deployment/recovery model is:

```text
change
    ↓
classify
    ↓
validate
    ↓
build once
    ↓
publish immutable SHA image
    ↓
deploy
    ↓
observe
    ↓
externally verify
    ↓
record verified evidence
    ↓
clean up
```

For hard interruption:

```text
runner disappears
    ↓
durable Terraform state remains
    ↓
fresh runner initializes backend
    ↓
reviews exact destroy plan
    ↓
removes only owned temporary resources
    ↓
independently verifies AWS cleanup
```

For rollback:

```text
operator selects SHA
    ↓
verify immutable ECR artifact
    ↓
retrieve durable deployment evidence
    ↓
verify historical success
    ↓
verify runtime-config compatibility
    ↓
eligible or rejected
```

Only an eligible candidate proceeds to runtime mutation.

## Major architectural lessons

### Recovery is broader than rollback

The sprint started with rollback as one of the visible deployment-safety problems.

The experiments showed that recovery contains several independent concerns:

```text
infrastructure recovery
deployment diagnosis
release evidence
rollback selection
runtime compatibility
cleanup
```

A system can support artifact rollback and still fail to recover infrastructure after a terminated runner.

Likewise, a system can clean up normally after a failed command while still lacking a recovery path when cleanup never executes.

The important distinction became:

```text
cleanup
!=
recovery architecture
```

### Durable state must outlive the runner

Sprint 01 relied on runner-local Terraform state for temporary verification infrastructure.

Sprint 02 moved the authoritative state to versioned S3 and tested recovery from a fresh execution with no previous:

```text
.terraform
terraform.tfstate
terraform.tfstate.backup
verification.tfplan
recovery-destroy.tfplan
```

The fresh runner recovered the exact Terraform-owned ALB and listener from remote state, generated a saved destroy plan, allowlisted the destructive changes, removed the resources, and independently verified their absence through AWS.

The resulting recovery model is:

```text
durable remote state
+
state locking
+
fresh-runner initialization
+
reviewed destructive plan
+
independent AWS verification
```

### A known image is not a known-good release

The rollback experiments proved:

```text
immutable image exists
!=
verified rollback candidate
```

An ECR image can exist even when it has never completed:

```text
ECS stabilization
+
external /health
+
exact /version
```

Successful deployment evidence therefore became durable machine-readable data rather than operator memory.

### A known-good image is not a complete rollback-safe release

Issue #42 exposed a deeper problem.

Version A was healthy with:

```text
RUNTIME_CONTRACT=A
```

Version B was healthy with:

```text
RUNTIME_CONTRACT=B
```

The older rollback model allowed:

```text
historical image A
+
current runtime configuration B
```

because it verified the artifact and historical deployment evidence but not the runtime configuration being applied now.

That hybrid failed at runtime with:

```text
RUNTIME_CONTRACT mismatch: expected "A", got "B"
```

The release/rollback identity therefore evolved from:

```text
environment
+
image
```

to:

```text
environment
+
immutable image digest
+
runtime configuration digest
```

The historical task-definition ARN remains useful provenance, but it is not semantic configuration identity because the rollback workflow does not restore that historical task definition.

## Mistakes and corrections

### The planning document became implementation documentation

The first Sprint 02 planning draft grew to roughly 3,800 lines.

Requirements were repeated across architecture, failure handling, risks, Definition of Done, issue sequencing, and implementation details.

That made the document harder to reason about instead of safer.

It was reduced to a much smaller architectural contract, while implementation-specific acceptance criteria remained in their individual issues.

Lesson:

```text
planning should define boundaries and guarantees
implementation documents should hold implementation detail
```

### I initially created a circular deployment-evidence model

An early model implied that verification evidence should exist before deployment.

That is impossible for a new release:

```text
cannot deploy without verification evidence
        ↓
but
        ↓
cannot create verification evidence until after deployment
```

The model was corrected:

```text
new candidate
    ↓
may deploy without historical evidence
    ↓
successful stabilization + /health + /version
    ↓
create verified deployment record
    ↓
that evidence may later authorize rollback
```

This clarified the difference between deploying a new candidate and selecting a historical candidate for rollback.

### I treated `if: always()` as stronger recovery protection than it is

Normal workflow cleanup protects failures only while GitHub still has a runner available to execute the cleanup step.

It cannot guarantee cleanup after force-cancellation or other cases where the cleanup step never executes.

The fresh-runner experiment changed the design from:

```text
cleanup step exists
therefore cleanup is guaranteed
```

to:

```text
normal cleanup when possible
+
durable external recovery when cleanup never runs
```

### I used the Cost Explorer API during one cost investigation

Issue #37 incurred approximately:

```text
$0.03
```

for the temporary Elastic Load Balancing experiment.

An additional approximately:

```text
$0.03
```

came from Cost Explorer API requests used to investigate that charge.

The API investigation therefore cost roughly as much as the infrastructure experiment being measured.

The corrected policy is to use the AWS Billing/Cost Explorer console for ordinary manual cost review rather than paid Cost Explorer API queries.

### I initially selected the wrong ECS task during a deployment transition

During the diagnostic experiments, selecting the first running task was not sufficient because ECS could temporarily contain multiple deployment sets.

The collector was corrected to correlate evidence with the intended task definition rather than assuming the first running task represented the failing deployment.

Lesson:

```text
service-level counts
!=
identity of the deployment being diagnosed
```

### I treated a verified image as the complete rollback unit

The runtime-contract experiment showed that historical application success does not prove compatibility with current task configuration.

This was corrected by introducing deployment-record schema version 2 and:

```text
runtime_config_digest
```

Legacy schema-v1 records remain historical evidence but are no longer rollback-eligible under the current policy.

## Observability lessons

No single signal explains every deployment failure.

The experiments produced this evidence map:

| Failure                        | Most useful evidence             |
| ------------------------------ | -------------------------------- |
| Application exits at startup   | stopped-task/container metadata  |
| Container health failure       | ECS service events + task health |
| Stabilization timeout          | ECS service events               |
| External `/health` failure     | verifier evidence                |
| Exact `/version` mismatch      | verifier evidence                |
| Historical application context | CloudWatch Logs                  |

The deployment system therefore correlates evidence instead of treating a waiter result, ECS count, log message, or HTTP response as authoritative on its own.

CloudWatch application-log retention is bounded to:

```text
7 days
```

so diagnostics remain useful without creating indefinite log retention.

## Security and IAM lessons

Sprint 02 preserved temporary AWS credentials through GitHub Actions OIDC rather than adding long-lived AWS access keys.

Least privilege was preferred over making every diagnostic check convenient.

One example occurred during fresh-runner recovery: the GitHub Actions role lacked:

```text
ecs:ListTasks
```

The permission was not added merely to make a final evidence assertion green.

The recovery itself had already succeeded, and an independent administrator-profile check confirmed the runtime baseline.

Another example occurred with deployment records.

The GitHub Actions role intentionally had deterministic object access without broad:

```text
s3:ListBucket
```

permission.

A missing deployment record could therefore surface as `AccessDenied` instead of exposing key existence.

The validator was changed to fail closed on unavailable or inaccessible evidence instead of broadening S3 permissions.

The security model remains:

```text
temporary credentials
+
narrow required permissions
+
fail closed when required evidence cannot be proven
```

## Cost-control decisions

Sprint 02 used the following personal guardrails:

```text
target total Sprint 02 AWS cost: <= $3
hard personal ceiling:          $5
existing account AWS Budget:    $20/month
```

The Cost Explorer console snapshot supplied during Issue #44 showed approximately:

```text
September MTD:              $0.54
September 19:               $0.18

September 19 breakdown:
Elastic Load Balancing:     $0.16
VPC:                        $0.01
ECS:                        $0.01
```

The observed September MTD amount used for Sprint 02 cost evidence is therefore approximately:

```text
$0.54
```

This is well below both:

```text
$3 target
$5 hard ceiling
```

The largest visible cost driver was:

```text
Elastic Load Balancing
```

which is consistent with the repeated temporary verification-ALB experiments.

The experiments therefore reinforced the design choice to:

```text
create verification ALB only when needed
reuse it across related checks when practical
destroy it immediately afterward
```

No new always-on compute or load balancer was introduced.

## Storage-retention decisions

### ECR

At Issue #44 review time:

```text
repository: zero-to-prod-demo-api
images:     16
size:       94.83 MiB
tags:       immutable full-SHA
```

No ECR lifecycle policy is enabled.

A blind age- or count-based rule was deliberately rejected because native ECR lifecycle rules cannot determine whether an image is still referenced by retained verified-deployment evidence.

The safe deletion rule is therefore evidence-aware:

```text
protect current ECS image
+
protect images required by retained rollback evidence
+
only then consider explicit deletion
```

At the current storage size, preserving an uncertain image is preferable to deleting required rollback evidence for negligible savings.

### Terraform state

The remote state bucket is versioned.

Issue #44 observed approximately:

```text
current state object: 182 bytes
historical versions:  86
delete markers:        49
version storage:       269108 bytes
```

That is roughly 263 KiB of version data.

The cost is negligible at the current project scale, while the history remains useful operational evidence.

### Verified deployment records

The deployment-record bucket is also versioned.

Issue #44 observed:

```text
objects:         4
versions:        4
delete markers:  0
version bytes:   3166
```

These records are intentionally retained because they are small and represent durable release evidence.

## Final environment state

Issue #44 independently inspected the AWS and Terraform baseline after the Sprint 02 experiments.

ECS:

```text
Desired = 0
Running = 0
Pending = 0
```

Running task discovery:

```text
[]
```

Load balancer discovery:

```text
[]
```

Remote Terraform state:

```text
serial = 37
lineage = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
managed_resources = []
```

The retained target group remains intentionally present as baseline infrastructure and has no attached load balancer.

The final resting model is therefore:

```text
ECS desired = 0
ECS running = 0
ECS pending = 0
running Fargate tasks = none
temporary verification ALB = absent
temporary Terraform managed resources = none
```

## Focused time

Recorded focused time through Issues #34–#43:

```text
#34   ~3h 00m
#35   ~3h 00m
#36   ~1h 50m
#37   ~0h 30m
#38   ~1h 25m
#39   ~2h 30m
#40   ~2h 00m
#41   ~1h 15m
#42   ~2h 00m
#43   ~1h 55m
----------------
       19h 25m
```

Issue #44 focused time:

```text
~3h
```

Final recorded Sprint 02 focused-time total:

```text
~22h 25m
```

This is an approximate focused-work total across Issues #34 through #44 and excludes unrelated idle time.

## What Sprint 02 does not prove

Sprint 02 does not establish:

```text
production readiness
automatic rollback
zero-downtime deployment
blue/green or canary delivery
multi-account promotion
multi-region recovery
EKS/Kubernetes operations
full observability platform
full historical configuration restoration
database/schema rollback
secret-value rollback
external API compatibility
recovery from every possible Terraform corruption
```

The runtime configuration digest covers the ECS task-definition registration document represented by this project.

It does not identify mutable external state behind unchanged references.

## Remaining knowledge gap

The clearest remaining compatibility boundary is:

```text
artifact identity matches
+
runtime_config_digest matches
+
external dependency changed
```

Examples include:

```text
secret value rotation behind the same secret ARN
database/schema changes
external API contract changes
mutable external service configuration
```

The current release identity cannot detect those changes.

## Sprint 03 next capability

The next capability should test **external dependency compatibility as part of release and rollback safety**.

A focused experiment should keep both:

```text
immutable image identity
runtime_config_digest
```

unchanged while mutating one external dependency.

A low-cost example is a development-only mutable configuration value referenced through the same identifier.

The experiment should answer:

```text
Can a rollback candidate pass all current eligibility checks
while an external dependency has become incompatible?
```

If yes, Sprint 03 should determine what additional evidence belongs in release identity.

Possible mechanisms to investigate include:

```text
external configuration version identity
schema/migration compatibility metadata
secret/config version identifiers
dependency-contract versioning
```

The experiment should remain bounded and should not attempt to solve every external dependency category at once.

## Final Sprint 02 result

Sprint 02 moved the project from:

```text
deployment with cleanup and manual rollback
```

toward:

```text
recoverable infrastructure state
+
observable deployment failure
+
durable verified-release evidence
+
fail-closed rollback selection
+
runtime-configuration-aware compatibility
+
explicit low-cost baseline
```

The most important conceptual change is that deployment safety is no longer represented only by whether a workflow succeeds.

The project now separates:

```text
artifact identity
runtime configuration identity
verification evidence
infrastructure ownership
runtime observation
recovery state
```

and tests those boundaries independently.

The environment finishes Sprint 02 in its intended development-only, low-cost resting state.
