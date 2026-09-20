# Zero-to-Prod Platform

Zero-to-Prod is an evidence-driven DevOps and Cloud Engineering laboratory for learning how to design, build, secure, deploy, observe, operate, compare, recover, and evolve real software systems across local, open-source, and multi-cloud environments.

The project is technology- and provider-neutral. AWS is currently the deepest implemented cloud environment, but it is an implementation context rather than the permanent project boundary.

Zero-to-Prod develops production-oriented reference implementations and deliberately does **not** claim production readiness without evidence.

The long-term mission, engineering principles, learning model, environment strategy, and capability roadmap are defined in the:

[Zero-to-Prod v2 rebaseline specification](docs/rebaseline/zero-to-prod-v2-specification.md)

## Current implemented baseline

The repository currently contains two completed, evidence-backed capability layers centered on a small Go API and an AWS ECS Fargate development environment:

```text
Sprint 01
tested immutable deployment + external verification + manual rollback

Sprint 02
recoverable Terraform state + diagnostics + verified release evidence
+ machine-verifiable rollback eligibility + runtime-config compatibility
```

The AWS sandbox used for those experiments is:

```text
account = 333534066371
region  = eu-west-3
```

These sprints remain the historical implementation baseline. They do not define the permanent technology or provider scope of Zero-to-Prod v2.

## Zero-to-Prod v2 direction

Future work starts from engineering problems and capabilities rather than from a predetermined tool or cloud provider.

The project will progressively expand across local environments, open-source systems, and real cloud providers where provider-specific behavior is part of the learning objective. Technologies such as AWS, Azure, GCP, Kubernetes, Terraform, databases, messaging systems, and observability platforms are implementation options to evaluate rather than default answers.

Future work follows these principles:

```text
problem before technology
concept before provider
alternatives before decisions
evidence before claims
failure as part of design
measure before optimizing
local-first where behavior is portable
real providers where provider semantics matter
L5 breadth with selected L6 operational depth
```

## Implemented capability — Sprint 02

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

The currently implemented Sprint 01 and Sprint 02 baseline remains a development learning environment.

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

These limitations describe the currently implemented Sprint 01 and Sprint 02 baseline. They do not define the long-term scope or the next experiment. Future work is selected according to the Zero-to-Prod v2 rebaseline specification and its capability, learning, evidence, and technology-selection principles.
