# Sprint 01 — Reproducible Demonstration

## Purpose

This document describes a reproducible evidence-backed demonstration of the completed Sprint 01 delivery capability.

## Preconditions

The demonstration uses the Sprint 01 development environment:

```text
Repository: ZakariaAitAli/zero-to-prod
AWS account: 333534066371
Region: eu-west-3
ECS cluster: zero-to-prod-dev
ECS service: demo-api
ECR repository: zero-to-prod-demo-api
GitHub environment: development
```

AWS-facing GitHub Actions jobs authenticate through:

```text
arn:aws:iam::333534066371:role/zero-to-prod-github-actions
```

using GitHub OIDC.

Before starting a deployment or rollback experiment, verify the retained runtime baseline:

```bash
aws ecs describe-services \
  --cluster zero-to-prod-dev \
  --services demo-api \
  --region eu-west-3 \
  --profile sandbox \
  --query 'services[0].{Desired:desiredCount,Running:runningCount,Pending:pendingCount,TaskDefinition:taskDefinition}' \
  --output json
```

Expected idle runtime counts:

```text
Desired = 0
Running = 0
Pending = 0
```

Also verify that the temporary verification ALB is absent:

```bash
aws elbv2 describe-load-balancers \
  --names zero-to-prod-dev-alb \
  --region eu-west-3 \
  --profile sandbox
```

Expected result:

```text
LoadBalancerNotFound
```

The Issue #11 re-verification confirmed this baseline before final documentation.

## Normal deployment

The normal deployment path is implemented in:

```text
.github/workflows/demo-api-ci.yml
```

For a push to `main`, the workflow:

```text
tests and validates the application
    ↓
builds the Docker image once
    ↓
transfers the exact build artifact between jobs
    ↓
authenticates to AWS through GitHub OIDC
    ↓
pushes the full-SHA image to immutable Amazon ECR
    ↓
creates the temporary verification ALB
    ↓
registers and deploys a new ECS task-definition revision
    ↓
waits for ECS service stability
    ↓
verifies /health and exact /version externally
    ↓
scales ECS back to zero
    ↓
destroys the temporary ALB
```

The final successful normal deployment recorded during Issue #10 was GitHub Actions run:

```text
32510301964
```

Its jobs completed successfully:

```text
Test and build demo API        success
Publish immutable image        success
Deploy to development          success
```

The deployment included successful OIDC authentication, temporary ALB creation, ECS deployment, service stability, external HTTP verification, ECS scale-down, and Terraform destroy.

The final registered task definition after that deployment was:

```text
zero-to-prod-demo-api:8
```

This run demonstrated that the normal pipeline still worked after the controlled failure and rollback experiments.

## Controlled verification failure

Sprint 01 deliberately reproduced a deployment in which the application was healthy but the externally observed version did not match the version the workflow expected.

Controlled candidate:

```text
e7eec825bd16634a19e31a82a7140083e3360bd9
```

Known-good version intentionally expected by the verifier:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

GitHub Actions run:

```text
32506584207
```

The workflow successfully reached:

```text
temporary verification ALB created
    ↓
task definition registered
    ↓
ECS service updated
    ↓
ECS reached a stable state
    ↓
external verification started
```

The application was reachable, but `/version` reported the controlled candidate rather than the intentionally expected known-good SHA.

The verifier failed with exit code:

```text
1
```

The deployment job was therefore marked as failed.

This demonstrated that:

```text
ECS stability
    +
healthy application
```

is not sufficient evidence that the intended artifact was deployed.

The exact externally observed version must also match the expected Git SHA.

## Cleanup after failure

Although external version verification failed, the normal cleanup path still executed.

GitHub Actions ran:

```text
Stop verification task
Destroy verification infrastructure
```

Both cleanup steps completed successfully.

The cleanup steps use:

```yaml
if: always()
```

together with step-outcome guards so a verification failure does not skip cleanup after temporary resources have already been created.

Cleanup was then verified independently from AWS rather than relying only on the GitHub Actions result.

ECS state after the failed candidate:

```text
Desired: 0
Running: 0
Pending: 0
Task definition: zero-to-prod-demo-api:6
```

The temporary verification ALB was queried by name and AWS returned:

```text
LoadBalancerNotFound
```

Therefore the controlled failed candidate left no active verification runtime before rollback began.

## Manual rollback

Rollback is implemented in:

```text
.github/workflows/demo-api-rollback.yml
```

It is manually dispatched and requires:

```text
target_sha = full 40-character Git SHA
confirmation = ROLLBACK
Git ref = main
```

The known-good rollback target used in the Sprint 01 demonstration was:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

Before changing ECS, the workflow verified that this SHA already existed as an immutable image in:

```text
zero-to-prod-demo-api
```

The rollback then:

```text
selected the existing ECR image
    ↓
rendered the current task-definition template with that image
    ↓
registered a new task-definition revision
    ↓
updated demo-api to desired count 1
    ↓
waited for ECS stability
    ↓
verified the task definition
    ↓
verified /health and exact /version externally
```

The successful rollback was GitHub Actions run:

```text
32507641720
```

The rollback registered:

```text
zero-to-prod-demo-api:7
```

No old source code was rebuilt. Recovery reused the already-published immutable image.

## Rollback verification and recovery time

The rollback used the same external verifier as normal deployment.

It required:

```text
GET /health
    → healthy

GET /version
    → exactly 1b6a631f0db11289b641fdd3b364282c87cf457b
```

The rollback workflow succeeded only after both checks passed through the temporary verification ALB.

This proved the complete recovery path:

```text
selected immutable ECR image
    ↓
new ECS task-definition revision
    ↓
running Fargate task
    ↓
externally reachable application
    ↓
exact known-good version observed
```

Rollback timing was measured from immediately before rollback task-definition registration and deployment to successful external verification.

Recorded start:

```text
2026-08-21T17:24:05Z
```

Recorded completion:

```text
2026-08-21T17:26:15Z
```

Measured recovery duration:

```text
130 seconds
```

Equivalent:

```text
2 minutes 10 seconds
```

Authentication, rollback-image validation, Terraform initialization, temporary ALB creation, and final cleanup were outside this defined recovery metric.

After successful rollback verification, ECS was scaled back to zero and the temporary verification ALB was destroyed.

## Restore normal deployment

After the controlled failure and successful rollback, the temporary verifier modification used for the experiment was removed and the normal deployment path was restored.

The next normal `main` deployment was GitHub Actions run:

```text
32510301964
```

Result:

```text
Test and build demo API        success
Publish immutable image        success
Deploy to development          success
```

The workflow again completed:

```text
OIDC authentication
    ↓
temporary verification ALB creation
    ↓
task-definition registration
    ↓
ECS deployment
    ↓
service stability
    ↓
external /health + /version verification
    ↓
ECS scale-down
    ↓
Terraform destroy
```

The resulting ECS task definition was:

```text
zero-to-prod-demo-api:8
```

This confirmed that the controlled failure and rollback experiment did not leave the normal deployment pipeline in a modified or broken state.

## Final runtime baseline

After the final normal deployment, AWS was checked independently rather than relying only on the GitHub Actions result.

The final Issue #10 baseline was:

```text
ECS desired = 0
ECS running = 0
ECS pending = 0
task definition = zero-to-prod-demo-api:8
temporary verification ALB = absent
```

During Issue #11, this baseline was checked again.

Observed ECS state:

```text
Service: demo-api
Desired: 0
Running: 0
Pending: 0
TaskDefinition: zero-to-prod-demo-api:8
```

The verification ALB lookup returned:

```text
LoadBalancerNotFound
```

This confirms that the completed Sprint 01 system currently retains no active Fargate verification runtime and no temporary verification ALB.

The retained ECS, ECR, IAM, networking, target-group, and repository resources remain available for future experiments, so this should be described as a zero-runtime or low/no-idle-cost baseline rather than zero total AWS cost.

## What this demonstration proves

This demonstration provides evidence that Sprint 01 can:

```text
take a tested commit
    ↓
build one Docker artifact
    ↓
identify it with the full Git SHA
    ↓
publish it to immutable Amazon ECR
    ↓
authenticate to AWS through GitHub OIDC
    ↓
deploy the exact image to ECS Fargate
    ↓
verify application health externally
    ↓
verify the exact deployed version
    ↓
reject a healthy but incorrect version
    ↓
clean up the failed verification runtime
    ↓
roll back to an existing known-good immutable image
    ↓
verify that rollback externally
    ↓
return to the zero-runtime baseline
```

The controlled failure and rollback sequence also demonstrates that:

- ECS stability alone is not application verification.
- Application health alone is not artifact verification.
- Rollback reuses an existing artifact rather than rebuilding history.
- Deployment and rollback verification use the same external health and version checks.
- Cleanup is part of the deployment lifecycle and is verified independently.
- The current rollback confirmation model is operator-controlled, not independent second-person approval.
- Runner-local Terraform state remains a known cleanup-recovery limitation after hard runner termination.

The written demonstration uses real workflow runs, Git SHAs, task-definition revisions, measured rollback time, and independent AWS checks. No additional video recording is required to reproduce or evaluate the Sprint 01 capability.
