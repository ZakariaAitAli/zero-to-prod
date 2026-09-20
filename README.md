# Zero-to-Prod Platform

Zero-to-Prod is a development-only learning platform for building and testing deployment, recovery, observability, rollback, and cost-control patterns on AWS.

The project currently includes two completed capability layers:

```text
Sprint 01
tested immutable deployment + external verification + manual rollback

Sprint 02
recoverable Terraform state + diagnostics + verified release evidence
+ machine-verifiable rollback eligibility + runtime-config compatibility
```

The AWS sandbox is:

```text
account = 333534066371
region  = eu-west-3
```

The project deliberately does **not** claim production readiness.

## Current capability — Sprint 02

Sprint 02 focuses on:

> Recoverable, Observable, and Cost-Controlled Deployments

Given a tested change on `main`, the current development path can:

```text
classify the change
    ↓
run only required validation
    ↓
build the application artifact once
    ↓
publish an immutable full-SHA image to Amazon ECR
    ↓
initialize durable remote Terraform state
    ↓
create temporary verification infrastructure
    ↓
deploy to Amazon ECS Fargate
    ↓
wait for ECS stability
    ↓
externally verify /health
    ↓
externally verify exact /version
    ↓
persist verified deployment evidence
    ↓
scale ECS back to zero
    ↓
destroy temporary verification infrastructure
```

If deployment fails while the runner remains available, bounded diagnostics correlate ECS state, stopped-task/container metadata, CloudWatch application logs, and external-verifier evidence before cleanup.

If cleanup never executes, a fresh runner can initialize the same remote Terraform backend, recover the Terraform-owned temporary resources, review an exact saved destroy plan, and remove only the expected resources.

## Rollback model

Rollback remains an explicit operator action through:

```text
Demo API Rollback
```

The operator supplies:

```text
full target Git SHA
+
explicit ROLLBACK confirmation
```

Before runtime mutation, the workflow requires machine-verifiable evidence that the target is eligible.

Current rollback eligibility requires:

```text
target environment
+
immutable ECR image
+
schema-v2 verified deployment record
+
matching image digest
+
matching runtime_config_digest
+
successful historical health verification
+
successful historical exact-version verification
```

Only then can the workflow:

```text
register a fresh task-definition revision
    ↓
deploy the historical image
    ↓
wait for ECS stability
    ↓
freshly verify /health
    ↓
freshly verify exact /version
    ↓
return to the zero-runtime baseline
```

Historical verification authorizes a rollback attempt. It does not replace fresh post-rollback verification.

## Release identity

Sprint 02 demonstrated that an immutable image alone is not a complete rollback-safe release.

The tested release/rollback identity is:

```text
environment
+
immutable image digest
+
runtime configuration digest
```

The runtime configuration digest represents the ECS task-definition registration document except for the application image.

Rollback does not restore the historical ECS task definition. It uses the selected historical image with the current task-definition configuration only when the historical and current runtime-configuration identities match.

The digest does not prove compatibility for mutable external state such as:

```text
secret values behind unchanged references
database contents or schema
external API contracts
mutable external service configuration
```

Those remain explicit limitations and future experiment boundaries.

## Recovery model

Terraform state for development verification is stored in versioned Amazon S3 and uses native S3 locking.

A tested hard-interruption recovery followed:

```text
runner A creates temporary ALB/listener
    ↓
runner A is force-cancelled before cleanup
    ↓
durable S3 Terraform state remains
    ↓
fresh runner B starts with no local Terraform state
    ↓
terraform init reconnects to remote state
    ↓
saved destroy plan is generated
    ↓
destructive changes are allowlisted
    ↓
only ALB + listener are destroyed
    ↓
AWS independently confirms both are absent
```

The recovery design does not disable Terraform locking.

## Observability

Application stdout/stderr is sent to:

```text
/zero-to-prod/development/demo-api
```

with:

```text
CloudWatch Logs retention = 7 days
```

Failed-deployment diagnostics correlate evidence from multiple layers.

| Failure                        | Primary evidence                |
| ------------------------------ | ------------------------------- |
| Application startup exit       | stopped-task/container metadata |
| Container-health failure       | ECS events + task health        |
| Service stabilization failure  | ECS service events              |
| External `/health` failure     | verifier evidence               |
| Exact `/version` mismatch      | verifier evidence               |
| Historical application context | CloudWatch Logs                 |

Diagnostics are bounded and best-effort so they do not prevent cleanup.

## Security and IAM

GitHub Actions uses OIDC-issued temporary AWS credentials instead of long-lived AWS access keys.

The design favors narrow permissions and fail-closed behavior.

Examples tested during Sprint 02 include:

```text
exact Terraform state/lock access
deterministic deployment-record object access
no requirement for broad S3 ListBucket just to test record existence
rollback rejection when required evidence is inaccessible
no IAM expansion merely to make a secondary evidence assertion pass
```

Immutable SHA-tagged ECR images, guarded rollback inputs, pinned GitHub Actions dependencies, deployment concurrency, Terraform locking, and temporary verification ingress remain part of the security boundary.

Sprint 01 security details:

[Sprint 01 security decisions](docs/sprint-01/security-decisions.md)

Sprint 02 security/IAM decisions and lessons:

[Sprint 02 architecture](docs/sprint-02/architecture.md)

[Sprint 02 reflection](docs/sprint-02/reflection.md)

## Cost controls

The Sprint 02 personal AWS guardrails are:

```text
target Sprint 02 AWS cost <= $3
hard personal ceiling       $5
existing AWS Budget         $20/month
```

The Issue #44 Cost Explorer console snapshot showed approximately:

```text
September MTD = $0.54
```

The largest visible cost driver was Elastic Load Balancing from temporary verification-ALB experiments.

No intentionally always-on compute or load balancer was added.

Current storage controls include:

```text
CloudWatch Logs retention = 7 days
ECR tags = immutable full SHA
Terraform state = versioned S3
deployment records = versioned S3
```

No blind age- or count-based ECR lifecycle rule is enabled because such a rule cannot determine whether an image is still required by retained rollback evidence.

Image cleanup must first protect:

```text
current ECS image
+
images required by retained rollback evidence
```

## Normal resting state

The intended development baseline is:

```text
ECS desired = 0
ECS running = 0
ECS pending = 0
running ECS tasks = none
temporary verification ALB = absent
temporary Terraform managed resources = none
```

Issue #44 independently verified this baseline after the Sprint 02 experiments.

The retained target group is intentional baseline infrastructure and is not owned by the temporary verification Terraform state.

## Sprint 02 documentation

Architecture and system boundaries:

[Sprint 02 architecture](docs/sprint-02/architecture.md)

Operational deployment, recovery, rollback, and cleanup procedures:

[Sprint 02 operations runbook](docs/sprint-02/runbook.md)

Integrated evidence chain for the final Sprint 02 capability:

[Sprint 02 reproducible demonstration](docs/sprint-02/demonstration.md)

Mistakes, corrections, cost decisions, knowledge gaps, focused time, and next experiment:

[Sprint 02 reflection](docs/sprint-02/reflection.md)

Focused experiment evidence:

* [Target capability and failure model](docs/sprint-02/target-capability.md)
* [Remote Terraform state](docs/sprint-02/remote-terraform-state.md)
* [Change-aware CI](docs/sprint-02/change-aware-ci.md)
* [Terraform state locking](docs/sprint-02/terraform-state-locking.md)
* [Fresh-runner recovery](docs/sprint-02/fresh-runner-recovery.md)
* [ECS CloudWatch logging](docs/sprint-02/ecs-cloudwatch-logging.md)
* [Failed-deployment diagnostics](docs/sprint-02/failed-deployment-diagnostics.md)
* [Verified deployment records](docs/sprint-02/verified-deployment-records.md)
* [Rollback eligibility](docs/sprint-02/rollback-eligibility.md)
* [Runtime-configuration rollback compatibility](docs/sprint-02/runtime-config-rollback-compatibility.md)

## Sprint 01 foundation

Sprint 01 established the original development delivery path:

```text
test
→ build immutable artifact
→ publish to ECR
→ deploy to ECS Fargate
→ externally verify
→ clean up
→ operator-assisted rollback
```

Its documentation remains as the historical foundation for Sprint 02:

* [Sprint 01 architecture](docs/sprint-01/architecture.md)
* [Sprint 01 security decisions](docs/sprint-01/security-decisions.md)
* [Sprint 01 operations runbook](docs/sprint-01/runbook.md)
* [Sprint 01 reproducible demonstration](docs/sprint-01/demonstration.md)
* [Sprint 01 final reflection](docs/sprint-01/reflection.md)

## Known limitations

Zero-to-Prod remains a development learning environment.

Current known limitations include:

```text
no production-readiness claim
no automatic rollback
no zero-downtime deployment guarantee
operator-triggered rollback
temporary public HTTP verification ingress
desired ECS count returns to zero after verification
no multi-account promotion
no multi-region recovery
no EKS/Kubernetes or GitOps deployment model
no full historical task-definition restoration
no secret-value rollback
no database/schema rollback
no external API compatibility guarantee
no recovery guarantee for every possible Terraform/state failure
```

The next compatibility boundary is mutable external state that can change while both the immutable image identity and `runtime_config_digest` remain unchanged.
