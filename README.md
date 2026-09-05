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
operator selects a full SHA backed by prior successful deployment/verification evidence
    ↓
validate full target SHA + ROLLBACK confirmation
    ↓
verify that the immutable image already exists in ECR
    ↓
register a new task-definition revision using that existing image
    ↓
deploy + externally verify the selected version again
    ↓
return to the same zero-runtime baseline
```

Pull requests stop after validation. Image publication and development deployment run only for pushes to `main`.

Rollback is workflow-assisted but not automatic. A failed deployment is cleaned up first; an operator decides whether to dispatch the rollback workflow and is responsible for selecting a target that has prior successful deployment and external-verification evidence. The workflow itself proves only that the requested SHA is well formed, that the immutable ECR image exists, and that the selected version passes fresh post-deployment verification.

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
CI is not change-aware; non-app changes still rebuild the demo API
```

Sprint 02 has moved development verification state to a durable, versioned S3 backend and proved normal recovery from a separate fresh Terraform execution.

End-to-end GitHub Actions OIDC access to the S3 backend is now verified by `main` workflow run `33983112817`, which successfully initialized, planned, applied, verified, and destroyed the development verification infrastructure through remote state.

Before the next remote-state reliability experiment, the CI workflow will be made change-aware so non-app changes do not unnecessarily rebuild the demo API.

See [Sprint 02 remote Terraform state evidence](docs/sprint-02/remote-terraform-state.md) and [Sprint 01 final reflection](docs/sprint-01/reflection.md).
