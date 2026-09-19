# Zero-to-Prod Platform

## Sprint 01

Sprint 01 proves a complete development delivery path for a small containerized service.

Given a tested commit on `main`, GitHub Actions can build one Docker image, identify it with the full Git commit SHA, publish that immutable artifact to Amazon ECR, deploy it to Amazon ECS Fargate using GitHub OIDC authentication, verify the running application externally, and perform an operator-assisted rollback to a previously verified immutable image when necessary.

The deployment uses a temporary internet-facing Application Load Balancer only for external verification. After deployment or rollback verification, the ECS service is scaled back to zero and the temporary load balancer is destroyed.

The sandbox environment is development-only in AWS account `333534066371`, region `eu-west-3`.

## Deployment flow

```text
Pull request
    ↓
format + vet + tests + Docker build validation
    ↓
merge / push to main
    ↓
build Docker image once as zero-to-prod-demo-api:<full Git SHA>
    ↓
export and transfer the exact image artifact
    ↓
GitHub OIDC → temporary AWS credentials
    ↓
push SHA-tagged image to immutable Amazon ECR
    ↓
GitHub OIDC → temporary AWS credentials
    ↓
create temporary verification ALB + listener
    ↓
render and register ECS task definition with the exact ECR image
    ↓
scale demo-api from 0 → 1
    ↓
wait for ECS service stability
    ↓
external /health + exact /version verification
    ↓
success or verification failure
    ↓
scale demo-api from 1 → 0
    ↓
destroy temporary verification ALB

On failure, rollback is an explicit operator action:

manual rollback dispatch
    ↓
operator supplies a full SHA + explicit ROLLBACK confirmation
    ↓
validate the full target SHA
    ↓
verify that the immutable image exists in ECR
    ↓
retrieve the verified deployment record for the target environment + SHA
    ↓
compute the current runtime-configuration digest
    ↓
require schema-v2 evidence matching environment, SHA, image URI, and current ECR digest
    ↓
require the historical runtime-config digest to match the current task-definition configuration
    ↓
require prior successful health + exact-version verification evidence
    ↓
register a fresh task-definition revision using the historical image + current compatible configuration
    ↓
deploy + externally verify /health and exact /version again
    ↓
return to the same zero-runtime baseline
```

Pull requests stop after validation. Image publication and development deployment run only for pushes to `main`.

Rollback is workflow-assisted but not automatic. A failed deployment is cleaned up first, and an operator must explicitly dispatch the rollback workflow with the `ROLLBACK` confirmation. The workflow then makes rollback eligibility machine-verifiable: it requires a full SHA, confirms that the immutable ECR image exists, retrieves a schema-v2 durable verified deployment record for the target environment, validates the requested SHA and current image digest, and requires the historical runtime-configuration digest to exactly match the current task-definition configuration before deployment may proceed. Historical verification evidence establishes rollback eligibility but does not replace fresh post-rollback external `/health` and exact `/version` verification.

Rollback does not restore the historical ECS task definition. It registers a fresh revision using the selected historical application image and the current task-definition template, but only when their recorded/current runtime-configuration identities match. Secret values behind unchanged references, database/schema state, and other external dependencies are outside this compatibility digest.

## Architecture

The Sprint 01 architecture separates artifact delivery, AWS authentication, runtime deployment, external verification, rollback, and cleanup.

See [Sprint 01 architecture](docs/sprint-01/architecture.md) for the maintained Mermaid diagram and system boundaries.

## Security

Sprint 01 uses GitHub OIDC instead of stored AWS keys, immutable SHA-tagged images, constrained IAM policies, guarded deployment and rollback workflows, pinned Action dependencies, and temporary verification ingress.

See [Sprint 01 security decisions](docs/sprint-01/security-decisions.md) for the verified controls, IAM boundaries, and known limitations.

## Operations and failure recovery

Common authentication, authorization, ECS, verification, rollback, and cleanup failures are documented with evidence-backed checks and recovery guidance.

See [Sprint 01 operations runbook](docs/sprint-01/runbook.md).

## Demonstration

Sprint 01 includes a reproducible written demonstration covering normal deployment, a controlled wrong-version failure, cleanup, manual rollback to an existing immutable image, external rollback verification, measured recovery time, and restoration of the normal pipeline.

See [Sprint 01 reproducible demonstration](docs/sprint-01/demonstration.md).

## Sprint 01 evidence

The final Sprint 01 reflection records the delivered capability, mistakes and corrections, knowledge gaps, known limitations, focused time, and next experiment.

See [Sprint 01 final reflection](docs/sprint-01/reflection.md).

## Known limitations

Sprint 01 intentionally remains development-only.

Current limitations include:

```text
no tested hard-interruption recovery from remote Terraform state
manual rollback rather than automatic rollback
operator confirmation rather than independent approval
temporary public HTTP verification ingress
desired count returns to 0 after verification
```

Sprint 02 has moved development verification state to a durable, versioned S3 backend and proved normal recovery from a separate fresh Terraform execution.

End-to-end GitHub Actions OIDC access to the S3 backend was established by `main` workflow run `33983112817`, which successfully initialized, planned, applied, verified, and destroyed the development verification infrastructure through remote state.

CI is now change-aware with the stable fail-closed `CI required` merge gate. Development verification also uses native S3 state locking, exact lock-object IAM permissions, and bounded lock waits while retaining GitHub deployment concurrency. `main` rollback workflow run `34280243917` proved the GitHub OIDC role can initialize the locked backend, complete plan/apply/destroy, release every lock, and return the AWS environment to its low-cost baseline.

See [Sprint 02 remote Terraform state evidence](docs/sprint-02/remote-terraform-state.md), [change-aware CI evidence](docs/sprint-02/change-aware-ci.md), [Terraform state locking evidence](docs/sprint-02/terraform-state-locking.md), [machine-verifiable rollback eligibility evidence](docs/sprint-02/rollback-eligibility.md), [runtime-configuration rollback compatibility evidence](docs/sprint-02/runtime-config-rollback-compatibility.md), and [Sprint 01 final reflection](docs/sprint-01/reflection.md).
