# Sprint 01 — Security Decisions

## Purpose

This document records the security decisions implemented and verified during Sprint 01.

## Authentication

GitHub Actions authenticates to AWS through OpenID Connect rather than stored long-lived AWS access keys.

The AWS IAM role is:

```text
arn:aws:iam::333534066371:role/zero-to-prod-github-actions
```

AWS STS issues temporary credentials only when the GitHub OIDC token satisfies the role trust policy.

The trust policy restricts the audience to:

```text
sts.amazonaws.com
```

and accepts exactly these GitHub OIDC subjects:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
repo:ZakariaAitAli/zero-to-prod:environment:development
```

The `id-token: write` GitHub permission is granted only to jobs that need AWS authentication. The normal test-and-build job does not receive OIDC token permission.

A real failure during Issue #10 demonstrated why the subject restriction matters. After `environment: development` was added to the deployment job, GitHub changed the OIDC subject from the branch form to the environment form. AWS rejected the role assumption with:

```text
Not authorized to perform sts:AssumeRoleWithWebIdentity
```

No Terraform or ECS mutation occurred before that authentication failure. The trust policy was then corrected to allow the verified environment-bound subject.

## Authorization

The GitHub Actions role uses separate policies for image publication, ECS deployment, and temporary verification infrastructure.

Permissions are resource-scoped where the AWS API supports useful resource scoping rather than claiming that every action can be restricted to one ARN.

### Amazon ECR

Image operations are restricted to:

```text
arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api
```

The role can upload the deployment image and use `ecr:BatchGetImage` to verify that a requested rollback image exists.

`ecr:GetAuthorizationToken` uses `Resource: "*"`, because that API is not repository-scoped.

### Amazon ECS

`ecs:UpdateService` and `ecs:DescribeServices` are restricted to:

```text
arn:aws:ecs:eu-west-3:333534066371:service/zero-to-prod-dev/demo-api
```

`ecs:RegisterTaskDefinition` currently uses `Resource: "*"`, so the policy should not be described as fully resource-scoped.

The GitHub role can pass only:

```text
arn:aws:iam::333534066371:role/zero-to-prod-ecs-task-execution
```

and only to:

```text
ecs-tasks.amazonaws.com
```

### ECS task execution identity

The Fargate task uses a separate execution role rather than the GitHub deployment role.

That execution role can pull images from `zero-to-prod-demo-api`, but it does not have image-push permissions.

Its trust policy allows assumption by `ecs-tasks.amazonaws.com` with the source account restricted to `333534066371` and the source ARN restricted to ECS resources in `eu-west-3` for that account.

### Temporary verification infrastructure

Read-only EC2 and load-balancing discovery actions use `Resource: "*"` where required by the APIs.

Creation and deletion permissions for the temporary verification load balancer are constrained to the `zero-to-prod-dev-alb` ARN namespace, and listener deletion is constrained to listeners belonging to that load balancer namespace.

## Artifact integrity

Deployment images use the full Git commit SHA as the image tag.

The Docker image is built once in the `test-and-build` job:

```text
docker build
    ↓
docker save
    ↓
GitHub Actions artifact
    ↓
docker load
    ↓
push to Amazon ECR
```

The publishing job does not rebuild the application image. It receives the exact Docker artifact produced by the build job and pushes that artifact to ECR.

The ECR repository:

```text
zero-to-prod-demo-api
```

is configured with:

```text
image tag mutability: IMMUTABLE
```

Immutability was tested rather than assumed. A different image manifest digest was submitted using an already-existing full-SHA tag, and ECR rejected the reassignment with:

```text
ImageTagAlreadyExistsException
```

The repository was queried again afterward and the original tag-to-digest mapping remained unchanged.

These are separate controls:

```text
full Git SHA tag
    → identifies the source version

build once
    → preserves the tested artifact across jobs

immutable ECR tag
    → prevents that tag from being reassigned to another image
```

Rollback follows the same artifact-integrity model: it selects an already-published immutable SHA image from ECR rather than rebuilding old source.

## Deployment exposure

The development Fargate task runs in public subnets with:

```text
assignPublicIp = ENABLED
```

This avoids introducing a NAT Gateway for the Sprint 01 sandbox, but a public IP does not mean the application port is directly open to the Internet.

The task security group permits inbound application traffic on:

```text
TCP 8080
```

only from the ALB security group.

The ALB security group accepts:

```text
TCP 80
source: 0.0.0.0/0
```

and forwards application traffic to the task security group.

Therefore the intended inbound path is:

```text
Internet
    ↓
Application Load Balancer
    ↓
target group
    ↓
Fargate task :8080
```

The application task does not accept arbitrary Internet traffic directly on port `8080`.

For automated deployment verification, the internet-facing ALB and its HTTP listener are temporary. Terraform creates only that verification ingress while reusing the existing target group, security group, subnets, and ECS infrastructure.

After verification, the workflow attempts to destroy the temporary ALB and listener.

## Deployment safety

Normal development deployment and rollback share the same GitHub Actions concurrency group:

```text
demo-api-development-deployment
```

with:

```text
cancel-in-progress: false
```

This prevents the deployment and rollback workflows from intentionally running as separate concurrent mutations of the same development service, while allowing an active deployment to finish rather than being cancelled by a newer one.

Both workflows use the GitHub environment:

```text
development
```

Normal deployment occurs only for a push to:

```text
refs/heads/main
```

Pull requests run validation but do not publish an image or deploy to AWS.

Rollback is explicit and workflow-assisted rather than automatic.

The rollback workflow requires:

```text
target_sha = full 40-character Git SHA
confirmation = ROLLBACK
Git ref = main
```

Before ECS is changed, the workflow verifies that the requested immutable image already exists in ECR.

Rollback does not rebuild old source. It registers a new task-definition revision using the current task-definition template with the selected existing immutable image.

This confirmation model is an operator safeguard, not independent approval. Sprint 01 does not implement mandatory second-person review or production-grade deployment approval protection.

## Supply-chain controls

Every external GitHub Action dependency currently referenced by the Sprint 01 workflows is pinned to a full commit SHA rather than a mutable version tag.

This includes actions from:

```text
actions/checkout
actions/setup-go
actions/upload-artifact
actions/download-artifact
aws-actions/configure-aws-credentials
hashicorp/setup-terraform
```

For example:

```yaml
uses: aws-actions/configure-aws-credentials@e6de054238d6b7531b4efff3b6587d9aade6a06c
```

The human-readable release version remains as a comment where useful, but workflow execution resolves the immutable commit reference.

Pinning reduces the risk of an upstream mutable tag changing the code executed by the CI/CD workflow without a repository change.

## Cleanup and known limitations

The development runtime is intentionally temporary during verification.

After a normal deployment or rollback verification, the workflow:

```text
scales demo-api to desired count 0
    ↓
waits for ECS stability
    ↓
destroys the temporary verification ALB and listener
```

Cleanup steps use:

```yaml
if: always()
```

with step-outcome guards so cleanup is still attempted when deployment verification fails after temporary resources have been created.

This behavior was tested during the controlled failed-candidate deployment. After verification failed, both the ECS scale-down and Terraform destroy steps completed.

Cleanup was also verified independently from AWS rather than trusting workflow status alone. The final Sprint 01 baseline was observed as:

```text
ECS desired = 0
ECS running = 0
ECS pending = 0
temporary verification ALB = absent
```

### Runner-local Terraform state

Terraform state for the temporary verification infrastructure is local to the GitHub Actions runner.

The normal lifecycle works because `plan`, `apply`, verification, and `destroy` execute on the same runner.

However, `if: always()` cannot protect against every hard runner termination or hard workflow cancellation.

If the runner disappears after:

```text
terraform apply
```

but before:

```text
terraform destroy
```

a temporary ALB could remain orphaned. A fresh runner would not automatically have the previous local Terraform state needed to resume that cleanup.

Remote Terraform state or another orphan-resource recovery mechanism is deliberately outside Sprint 01 and remains a future improvement.
