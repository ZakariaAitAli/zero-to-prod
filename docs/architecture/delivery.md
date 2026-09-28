# Delivery architecture

This document describes the delivery architecture established by Sprint 01 and deepened by Sprint 02.

The AWS runtime and delivery infrastructure were later deliberately retired during the v2 rebaseline. The architecture remains useful as the concrete system through which the delivery and recovery capabilities were learned.

## Delivery flow

The core path was:

```text
Git commit
    ↓
GitHub Actions
    ↓
tests + Docker build
    ↓
exact built image transferred between jobs
    ↓
GitHub OIDC → AWS STS
    ↓
immutable ECR image
    ↓
ECS task-definition revision
    ↓
ECS Fargate deployment
    ↓
control-plane stability
    ↓
external /health + /version verification
    ↓
verified deployment record
```

Pull requests validated relevant application and infrastructure changes without publishing or deploying an application artifact to AWS.

## Identity boundary

GitHub Actions authenticated to AWS through OIDC and temporary STS credentials.

Trust and permissions were separate:

- the IAM trust policy constrained which GitHub identities could assume the role;
- IAM permission policies constrained what the resulting session could do.

The architecture did not depend on permanent AWS access keys stored in GitHub.

## Artifact boundary

The application image was built once and identified by the full Git commit SHA.

The exact built image was transferred between CI jobs instead of being rebuilt for publication.

ECR tag immutability prevented an existing SHA tag from being reassigned to different image content.

## Runtime boundary

The development runtime used ECS Fargate.

The delivery path rendered the selected ECR image into a task-definition revision and updated the ECS service.

The architecture distinguished several identities, including the GitHub deployment identity and the ECS task execution identity.

## Verification boundary

ECS service stability was treated as a platform signal, not proof of application success.

External verification checked both:

- application health;
- exact expected version.

Temporary verification ingress was created for the experiment and cleaned up afterward.

## Infrastructure ownership and state

Terraform was first introduced narrowly for temporary verification infrastructure.

Sprint 01 used runner-local Terraform state and identified the resulting recovery limitation.

Sprint 02 introduced remote, versioned state and state locking, then demonstrated recovery of orphaned verification infrastructure from a fresh runner.

Sprint 02 also made infrastructure ownership boundaries more explicit and brought the retained development runtime under Terraform ownership.

## Observability and diagnostics

Sprint 02 added ECS application logs in CloudWatch and bounded failed-deployment diagnostic collection.

The architecture treated these as different signal layers:

```text
application logs
ECS service/task state
container health
load-balancer/target state
external HTTP verification
exact version verification
```

## Release record

Sprint 02 added machine-readable verified deployment metadata.

This separated:

```text
image exists
     from
release was successfully deployed and verified
```

Rollback eligibility could then require the appropriate release evidence and current artifact identity instead of treating every ECR image as known-good.

## Rollback path

Rollback selected an existing immutable image rather than rebuilding an old commit.

The path remained:

```text
select eligible existing release
      ↓
validate artifact/release identity
      ↓
render deployment configuration
      ↓
deploy
      ↓
wait for platform convergence
      ↓
externally verify again
```

Sprint 02 also tested runtime-configuration compatibility because an old application artifact is not automatically compatible with current configuration.

## Concurrency and cleanup

Deployment and rollback shared a GitHub Actions concurrency boundary because both mutated the same environment.

Temporary verification resources were cleaned up after success or failure during normal execution.

Remote Terraform state provided the stronger recovery path when same-run cleanup did not execute.

## Current status

This AWS architecture is not the current active Work Items runtime.

The v2 rebaseline deliberately retired the pre-rebaseline AWS runtime and implementation while preserving the demonstrated learning.

For the current system, see [Work Items architecture](work-items.md).

## Related concepts and detailed records

- [Identity and access](../concepts/identity-and-access.md)
- [Artifacts and delivery](../concepts/artifacts-and-delivery.md)
- [Health and deployment verification](../concepts/health-and-deployment-verification.md)
- [Infrastructure state and recovery](../concepts/infrastructure-state-and-recovery.md)
- [Sprint 01 architecture](../sprint-01/architecture.md)
- [Sprint 02 architecture](../sprint-02/architecture.md)
- [Sprint 02 reproducible demonstration](../sprint-02/demonstration.md)
