# Sprint 01 — Deploy the Service to Development

## Purpose

Issue #8 extends the delivery path from immutable image publication into an actual development deployment on Amazon ECS Fargate.

Issue #7 established:

```text
GitHub main commit
      ↓
tests + Docker build
      ↓
immutable SHA-tagged image
      ↓
Amazon ECR
````

Issue #8 adds:

```text
existing immutable ECR image
      ↓
ECS task definition
      ↓
ECS Fargate service
      ↓
Application Load Balancer
      ↓
stable development deployment
```

The important design rule is:

```text
build once
publish once
deploy the exact published artifact
```

The deployment job does not rebuild the Docker image.

---

## Target capability

The completed flow is:

```text
push → main
    ↓
test and build
    ↓
exact Docker image transferred between jobs
    ↓
publish SHA-tagged image to ECR
    ↓
GitHub OIDC
    ↓
AWS STS temporary credentials
    ↓
render task definition with the same GITHUB_SHA
    ↓
register new ECS task-definition revision
    ↓
update existing development service
    ↓
wait for ECS service stability
    ↓
verify deployed task definition
    ↓
report environment + version
```

Development environment:

```text
AWS account: 333534066371
Region:      eu-west-3
Cluster:     zero-to-prod-dev
Service:     demo-api
```

---

## Authentication and authorization

Authentication remains unchanged from Issue #6:

```text
GitHub Actions
      ↓
GitHub OIDC token
      ↓
AWS STS
      ↓
zero-to-prod-github-actions
      ↓
temporary AWS credentials
```

Issue #8 adds ECS deployment authorization separately.

The GitHub role is not given broad ECS administrative access.

It receives only the permissions required to:

```text
register a task definition
update one ECS service
describe that service
pass one ECS execution role to ECS tasks
```

Policy source:

```text
infra/aws/iam/github-actions-ecs-deploy-policy.json
```

Key authorization boundaries:

```text
ecs:RegisterTaskDefinition
Resource: *

ecs:UpdateService
ecs:DescribeServices
Resource:
arn:aws:ecs:eu-west-3:333534066371:service/zero-to-prod-dev/demo-api

iam:PassRole
Resource:
arn:aws:iam::333534066371:role/zero-to-prod-ecs-task-execution

Condition:
iam:PassedToService = ecs-tasks.amazonaws.com
```

`ecs:RegisterTaskDefinition` requires `Resource: "*"`, because task-definition registration does not support useful resource-level restriction at registration time.

The service mutation permissions remain restricted to exactly one development service.

---

## ECS task execution identity

The application itself does not need AWS API access.

Therefore no application task role was introduced.

A separate ECS task execution role was created:

```text
zero-to-prod-ecs-task-execution
```

Its trust relationship allows:

```text
ecs-tasks.amazonaws.com
```

to assume the role.

Trust policy source:

```text
infra/aws/iam/ecs-task-execution-trust-policy.json
```

The trust policy also restricts the source account to:

```text
333534066371
```

and ECS resources in:

```text
eu-west-3
```

---

## Least-privilege ECR pull access

Fargate needs permission to pull the private image from ECR.

Instead of attaching the broader AWS-managed ECS task execution policy, a small custom inline policy was created.

Source:

```text
infra/aws/iam/ecs-task-execution-ecr-pull-policy.json
```

Permissions:

```text
ecr:GetAuthorizationToken
Resource: *

ecr:BatchGetImage
ecr:GetDownloadUrlForLayer
Resource:
arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api
```

The role cannot pull images from arbitrary ECR repositories.

A deliberate IAM simulation verified:

```text
target repository      → allowed
unauthorized repository → implicitDeny
```

The running Fargate task successfully pulling the image also provided runtime evidence that these permissions were sufficient.

---

## Development network design

The sandbox uses the default VPC:

```text
vpc-0de74a3a8146ba655
```

To avoid NAT Gateway cost, the development Fargate task runs in public subnets with:

```text
assignPublicIp = ENABLED
```

The public subnets already have a route through the VPC Internet Gateway.

The architecture is:

```text
Internet
   ↓
Application Load Balancer
   ↓ TCP 8080
Fargate task
   ↓
ECR over Internet Gateway
```

No NAT Gateway was created.

---

## Security groups

Two security groups separate public traffic from workload traffic.

ALB security group:

```text
zero-to-prod-dev-alb
```

Inbound:

```text
TCP 80
source: 0.0.0.0/0
```

Outbound:

```text
TCP 8080
destination: demo-api task security group
```

Task security group:

```text
zero-to-prod-dev-demo-api
```

Inbound:

```text
TCP 8080
source: ALB security group only
```

The task therefore does not expose port 8080 directly to the Internet.

---

## Load balancer and target health

The development Application Load Balancer used HTTP on port 80.

TLS and a custom domain are intentionally outside Sprint 01 scope.

The target group uses:

```text
protocol:    HTTP
port:        8080
target type: ip
```

`ip` target mode is required for Fargate tasks using `awsvpc` networking.

Health check:

```text
GET /health
expected status: 200
```

During the deployment experiment, the registered target reached:

```text
healthy
```

---

## ECS task definition

Task-definition template:

```text
infra/aws/ecs/demo-api-task-definition.json
```

The template intentionally does not contain a real image SHA.

Instead it contains:

```text
IMAGE_URI_TO_BE_RENDERED
```

The GitHub workflow verifies that this sentinel is present and replaces it with:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:${GITHUB_SHA}
```

This prevents an old image SHA from accidentally being committed into the reusable template.

Runtime configuration:

```text
Fargate
CPU:    256
Memory: 512 MiB
Port:   8080
```

---

## ECS container health check

The Dockerfile already contains a Docker `HEALTHCHECK`.

An important discovery during this issue was that an ECS task definition must explicitly define its own container health check for ECS to monitor it as part of task health.

The task definition therefore includes:

```text
wget -q --spider "http://127.0.0.1:${PORT}/health" || exit 1
```

This is separate from the ALB target-group health check.

The resulting model is:

```text
ECS container health
        +
ALB target health
```

Both observe the application's `/health` endpoint from different layers.

---

## Manual deployment baseline

Before automating deployment, the infrastructure path was proven manually.

The first task definition was:

```text
zero-to-prod-demo-api:1
```

It referenced the existing immutable image:

```text
114b45f54aea153f89602e974975e28328bb0bc4
```

The ECS service reached:

```text
Desired: 1
Running: 1
Pending: 0
Deployment: COMPLETED
```

The ALB target also reported:

```text
healthy
```

This proved that networking, IAM, ECR pulling, container startup, and load-balancer health were working before GitHub Actions automation was introduced.

---

## GitHub Actions deployment job

Workflow:

```text
.github/workflows/demo-api-ci.yml
```

The AWS publishing job was renamed to:

```text
Publish immutable image to ECR
```

A new dependent job was added:

```text
Deploy to development
```

Dependency:

```text
test-and-build
      ↓
publish-image
      ↓
deploy-development
```

The deployment job runs only when:

```text
event = push
branch = main
```

Pull requests therefore cannot publish or deploy.

This was verified in PR #21:

```text
Test and build demo API          success
Publish immutable image to ECR  skipped
Deploy to development           skipped
```

---

## GitHub Actions supply-chain hardening

All third-party GitHub Actions used by the workflow are pinned to commit SHAs rather than mutable version tags.

Examples include:

```text
actions/checkout
actions/setup-go
actions/upload-artifact
actions/download-artifact
aws-actions/configure-aws-credentials
```

Version comments remain beside the SHAs for readability.

---

## Build-once deployment

The deployment job contains no Docker build.

The image lifecycle is:

```text
test-and-build job
      ↓
docker build
      ↓
docker save
      ↓
GitHub artifact
      ↓
publish-image job
      ↓
docker load
      ↓
docker push to ECR
      ↓
deploy-development job
      ↓
reference existing ECR image only
```

The deployment stage therefore cannot silently produce a different container from the one tested and published earlier in the workflow.

---

## Successful automated deployment

PR #21 was merged into `main`.

Merge commit:

```text
104cf701de02e36e5345df6b9bab3eec8dfbf909
```

Workflow run:

```text
31828687774
```

Result:

```text
Test and build demo API          ✅
Publish immutable image to ECR  ✅
Deploy to development           ✅
```

The deployment job rendered:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:104cf701de02e36e5345df6b9bab3eec8dfbf909
```

It registered:

```text
zero-to-prod-demo-api:2
```

Then updated:

```text
cluster: zero-to-prod-dev
service: demo-api
```

The workflow waited until ECS reported the service stable.

Deployment report:

```text
Environment:
development

Version:
104cf701de02e36e5345df6b9bab3eec8dfbf909

Task definition:
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:2
```

---

## Independent AWS-side verification

GitHub reporting success was not treated as sufficient evidence.

The ECS service was independently inspected from AWS:

```json
{
  "Status": "ACTIVE",
  "Desired": 1,
  "Running": 1,
  "Pending": 0,
  "TaskDefinition": "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:2",
  "DeploymentStatus": "COMPLETED"
}
```

ECR contained the exact merge-commit tag:

```text
104cf701de02e36e5345df6b9bab3eec8dfbf909
```

Digest:

```text
sha256:a2065396c50bda71ee9a119f812575117a86e3f0fb1c4b293fffae843cc34e77
```

Task definition `:2` independently reported the exact image:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:104cf701de02e36e5345df6b9bab3eec8dfbf909
```

The evidence chain is therefore:

```text
main commit
    ↓
ECR SHA tag
    ↓
task definition :2
    ↓
ECS service
    ↓
stable running task
```

---

## Deliberate deployment failure experiment

A required property of the deployment pipeline is:

```text
deployment failure
      ↓
workflow failure
```

To test this safely, the application itself was not broken.

Instead, the live GitHub Actions IAM policy was backed up and temporarily modified by removing only:

```text
ecs:UpdateService
```

The rest of the deployment permissions remained unchanged.

The deployment job was then rerun.

Observed sequence:

```text
OIDC authentication                ✅
Render task definition             ✅
Register task definition           ✅
Update ECS service                 ❌
Wait for service stability         skipped
Verify and report deployment       skipped
```

The rerun registered:

```text
zero-to-prod-demo-api:3
```

but failed with:

```text
AccessDeniedException
```

because the GitHub role was not authorized to execute:

```text
ecs:UpdateService
```

The process exited non-zero, so GitHub correctly marked the deployment as failed.

This proved that deployment errors are not hidden or converted into false success.

---

## Runtime state after failed deployment

After the deliberate failure, AWS was inspected independently.

The service remained:

```text
Status: ACTIVE
Desired: 1
Running: 1
Pending: 0
Task definition: zero-to-prod-demo-api:2
Deployment: COMPLETED
```

Task definition `:3` existed but was never attached to the service.

Therefore the failed authorization attempt did not modify the healthy runtime state.

The original IAM permission policy was then restored and verified against the repository copy with no diff.

---

## Cost-conscious cleanup

The development resources were created only for the deployment experiment.

The two significant continuous billers were:

```text
Fargate task
Application Load Balancer
```

After verification:

```text
demo-api desired count → 0
```

Final ECS state:

```json
{
  "Desired": 0,
  "Running": 0,
  "Pending": 0
}
```

The Application Load Balancer was then deleted.

A final lookup returned:

```text
LoadBalancerNotFound
```

This provided explicit cleanup evidence.

Reusable low/no-idle-cost resources were retained, including:

```text
ECS cluster
ECS service configuration at desired count 0
task definitions
IAM roles and policies
security groups
target group
ECR repository and images
```

---

## What I learned

### 1. Authentication is not deployment authorization

OIDC only establishes who the GitHub workload is.

The role still needs explicit permissions for each deployment operation.

```text
OIDC / STS
    ≠
permission to deploy
```

---

### 2. Deployment should consume the tested artifact

Building again inside the deploy job would create a second artifact and weaken traceability.

The stronger model is:

```text
build once
identify by SHA
publish once
deploy by SHA
```

---

### 3. ECS deployment has multiple identities

The final architecture uses separate responsibilities:

```text
local SSO administrator
    → provisions infrastructure

GitHub Actions role
    → performs deployment

ECS task execution role
    → pulls the private image

application container
    → no AWS permissions required
```

This separation makes each trust boundary easier to reason about.

---

### 4. ECS and ALB health are different layers

Container health and target-group health are separate mechanisms.

Both are useful:

```text
ECS health
→ is the container healthy?

ALB health
→ can the load balancer successfully reach the application?
```

---

### 5. A registered task definition is not a deployment

The failure experiment created task definition `:3`, but because `UpdateService` failed, runtime stayed on `:2`.

Therefore:

```text
RegisterTaskDefinition
    ≠
deploy workload
```

The actual service mutation occurs at:

```text
UpdateService
```

---

## Mistakes and knowledge gaps

### IAM simulator output

When testing multiple resources with the IAM simulator, the useful decisions were under:

```text
ResourceSpecificResults
```

Reading only the top-level aggregate result initially produced misleading output.

---

### Docker HEALTHCHECK vs ECS health check

I initially expected the Dockerfile `HEALTHCHECK` to automatically become the ECS container health check.

For ECS task health monitoring, the health check needed to be explicitly represented in the task definition.

---

### GitHub CLI rerun syntax

The first attempt used both:

```text
run ID
and
--job
```

with `gh run rerun`.

The CLI requires only one selector.

The correct command was:

```text
gh run rerun --job <job-id>
```

---

## Acceptance criteria

Issue #8 required:

```text
Workflow deploys the requested image.
```

Verified using the exact merge SHA from ECR through task definition `:2`.

```text
Deployment waits for service stability.
```

Verified with:

```text
aws ecs wait services-stable
```

inside GitHub Actions.

```text
Deployment reports the environment and version.
```

Verified:

```text
Environment: development
Version: 104cf701de02e36e5345df6b9bab3eec8dfbf909
```

```text
A failed deployment returns a failed workflow result.
```

Verified through the deliberate `ecs:UpdateService` authorization failure.

```text
No build runs on your laptop.
```

All application builds occurred on GitHub-hosted runners.

Issue #8 acceptance criteria are therefore satisfied.

---

## Session record

Focused time:

```text
~1h 55m
```

Measured approximately from the first recorded AWS provisioning work through deployment verification, deliberate failure testing, cleanup, and documentation review.

Next focused experiment:

```text
verify the deployed application from outside the ECS control plane
```

The next issue should independently test:

```text
/health
/version
```

and confirm that the externally observed application version matches the immutable commit deployed by ECS.

---

## Result

Sprint 01 now has the following delivery path:

```text
source commit
    ↓
CI
    ↓
tested Docker image
    ↓
immutable ECR image
    ↓
ECS task definition
    ↓
development ECS service
    ↓
stable deployment
```

The next step is to verify the deployed application itself from outside the ECS control plane, including application endpoints such as:

```text
/health
/version
```

That belongs to the next issue rather than this deployment issue.
