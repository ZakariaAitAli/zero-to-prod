# Sprint 01 — Add Automated Health Verification

## Purpose

Issue #9 extends the deployment pipeline beyond control-plane success.

Issue #8 proved that GitHub Actions could:

```text
build once
    ↓
publish an immutable image
    ↓
register an ECS task definition
    ↓
update the ECS service
    ↓
wait for service stability
````

That still did not prove that a user outside the ECS control plane could successfully reach the deployed application or that the running application matched the expected source revision.

Issue #9 adds that missing verification layer.

The deployment pipeline now proves:

```text
ECS reports stable
        ↓
application is reachable externally
        ↓
/health reports healthy
        ↓
/version matches the expected Git SHA
        ↓
deployment verification passes
```

---

## Target capability

The target capability was:

```text
After deployment, automatically verify the application over HTTP
and fail the workflow if health or version verification fails.
```

The verification needed to satisfy the following properties:

```text
external application check
bounded timeout
bounded retries
useful diagnostics
exact version verification
non-zero exit on failure
cost-conscious temporary infrastructure
cleanup after verification
```

---

## Why ECS stability is not enough

`aws ecs wait services-stable` proves ECS control-plane convergence.

It does not prove:

```text
DNS works
the load balancer is reachable
the target responds successfully
the application reports healthy
the expected version is actually running
```

The stronger deployment model is therefore:

```text
control-plane verification
        +
data-plane verification
```

For this issue:

```text
control plane:
ECS service reaches stable state

data plane:
HTTP /health and /version succeed externally
```

---

## Verification architecture

The retained development baseline already contained:

```text
ECS cluster
ECS service
task definition family
target group
task security group
ALB security group
public subnets
ECR repository
IAM roles
```

The permanent ECS desired count remained:

```text
0
```

To avoid paying continuously for an Application Load Balancer, the verification path creates only temporary ingress infrastructure during deployment.

The automated flow is:

```text
GitHub Actions
    ↓
assume AWS role with OIDC
    ↓
Terraform creates temporary ALB + HTTP listener
    ↓
register task definition
    ↓
scale ECS service 0 → 1
    ↓
wait for ECS stability
    ↓
call /health
    ↓
call /version
    ↓
verify exact Git SHA
    ↓
scale ECS service 1 → 0
    ↓
Terraform destroys ALB + listener
```

---

## Temporary Terraform ingress

Terraform owns only the ephemeral verification ingress.

Directory:

```text
infra/terraform/development-verification/
```

Files:

```text
versions.tf
provider.tf
main.tf
outputs.tf
.terraform.lock.hcl
```

Terraform does not own the full development environment.

It references retained infrastructure using data sources:

```text
target group
ALB security group
public subnet A
public subnet B
```

It creates only:

```text
aws_lb.verification
aws_lb_listener.http
```

This kept the scope of Issue #9 narrow and avoided migrating unrelated infrastructure into Terraform.

---

## Terraform version pinning

Terraform is pinned to:

```text
Terraform 1.15.9
```

The AWS provider is pinned to:

```text
hashicorp/aws 6.59.0
```

The dependency lock file is committed:

```text
infra/terraform/development-verification/.terraform.lock.hcl
```

GitHub Actions uses the same Terraform version.

The Terraform setup action is pinned by commit SHA.

---

## Terraform state decision

Terraform state is intentionally local to the GitHub Actions runner for this narrow ephemeral lifecycle.

The normal lifecycle is:

```text
terraform plan
    ↓
terraform apply
    ↓
verification
    ↓
terraform destroy
```

The same runner retains the local state throughout the job.

This is sufficient for the normal Issue #9 lifecycle, but it creates a known limitation:

```text
hard runner termination
or
hard workflow cancellation
```

could occur after infrastructure creation but before cleanup.

Because no remote backend exists, a later fresh runner would not automatically have the previous local Terraform state.

This limitation is documented rather than hidden.

A future improvement could introduce remote state or another recovery mechanism, but that was deliberately kept outside Issue #9.

---

## Authentication and authorization

Authentication remains GitHub OIDC.

GitHub Actions assumes:

```text
arn:aws:iam::333534066371:role/zero-to-prod-github-actions
```

No long-lived AWS credentials are stored in GitHub.

Authentication answers:

```text
Who is this workload?
```

Answer:

```text
GitHub Actions running the trusted main workflow
```

Authorization remains a separate concern.

Issue #9 required new permissions for the temporary verification infrastructure.

---

## Least-privilege IAM derivation

The new policy is stored at:

```text
infra/aws/iam/github-actions-verification-infra-policy.json
```

The policy was not built from a guessed broad permission list.

The workflow was first exercised manually using the local SSO administrator identity.

CloudTrail was then inspected to determine which AWS API calls Terraform actually made.

Observed ELB operations included:

```text
CreateLoadBalancer
CreateListener
ModifyLoadBalancerAttributes
DescribeLoadBalancers
DescribeListeners
DescribeListenerAttributes
DescribeLoadBalancerAttributes
DescribeCapacityReservation
DescribeTags
DescribeTargetGroups
DescribeTargetGroupAttributes
DeleteListener
DeleteLoadBalancer
```

Observed EC2 read operations included:

```text
DescribeAccountAttributes
DescribeInternetGateways
DescribeSecurityGroups
DescribeSubnets
DescribeVpcs
GetSecurityGroupsForVpc
```

CloudTrail also showed that Elastic Load Balancing itself created network interfaces.

Therefore GitHub Actions did not need direct EC2 network-interface write permissions.

---

## Verification infrastructure IAM scope

Read-only discovery actions require wildcard resources where AWS does not support useful resource-level restriction.

The write operations are narrowed to the temporary verification ALB and listener naming pattern.

The policy permits lifecycle operations only for:

```text
zero-to-prod-dev-alb
```

Representative resource forms are:

```text
arn:aws:elasticloadbalancing:eu-west-3:333534066371:loadbalancer/app/zero-to-prod-dev-alb/*
```

and:

```text
arn:aws:elasticloadbalancing:eu-west-3:333534066371:listener/app/zero-to-prod-dev-alb/*/*
```

IAM simulation confirmed the required resource-scoped actions were allowed.

The stored live role policy was then fetched and compared against the repository policy.

The normalized policies matched.

---

## ECS authorization

Existing ECS deployment permissions were retained.

The GitHub Actions role can update only the intended ECS service:

```text
arn:aws:ecs:eu-west-3:333534066371:service/zero-to-prod-dev/demo-api
```

The relevant actions remain separated by purpose:

```text
ecs:RegisterTaskDefinition
ecs:UpdateService
ecs:DescribeServices
iam:PassRole
```

The new verification infrastructure policy did not broaden ECS access.

---

## Verification script

The HTTP verification logic lives in:

```text
scripts/verify-deployment.sh
```

The script is executable:

```text
mode 100755
```

Required inputs are:

```text
BASE_URL
EXPECTED_VERSION
```

The script fails immediately if either is missing.

Verification sequence:

```text
GET /health
    ↓
HTTP request must succeed
    ↓
JSON status must equal "healthy"
    ↓
GET /version
    ↓
HTTP request must succeed
    ↓
JSON version must exactly equal EXPECTED_VERSION
```

---

## Health endpoint verification

The script calls:

```text
/health
```

Expected response:

```json
{"status":"healthy"}
```

The body is parsed with `jq`.

The deployment fails if:

```text
.status != "healthy"
```

This makes verification stronger than checking only HTTP 200.

---

## Version verification

The script calls:

```text
/version
```

Expected response:

```json
{
  "version": "<expected Git SHA>"
}
```

The script extracts:

```text
.version
```

and compares it exactly against:

```text
EXPECTED_VERSION
```

For GitHub Actions:

```text
EXPECTED_VERSION=${{ github.sha }}
```

This verifies that the externally observed application corresponds to the same source revision that triggered the deployment.

---

## Timeout and retry behavior

The verification script defines explicit network bounds.

Each request uses:

```text
--connect-timeout 3
--max-time 5
--retry 4
--retry-delay 2
--retry-connrefused
```

This means the verifier does not wait indefinitely for:

```text
DNS problems
connection failures
slow endpoints
unhealthy responses
```

The retry behavior is explicit and reproducible.

---

## Useful diagnostics

The verifier prints:

```text
verification target
expected version
health endpoint
health response
version endpoint
version response
observed version
```

For failures it emits GitHub-compatible error messages:

```text
::error::...
```

HTTP response bodies are captured to temporary files and printed when useful.

This preserves evidence without mixing response content into command substitution or losing the body after a failed curl request.

---

## Failed verification behavior

The script uses:

```text
set -euo pipefail
```

and returns non-zero for failed verification.

Observed failure exit codes included:

```text
HTTP 500        → 22
timeout         → 28
wrong endpoint  → 7
wrong version   → 1
```

GitHub Actions invokes the script as a normal workflow step:

```text
./scripts/verify-deployment.sh
```

Therefore a non-zero verifier exit causes the verification step and deployment job to fail.

Cleanup steps use:

```text
if: always()
```

so cleanup is still attempted after verification failure.

---

## GitHub Actions integration

The deployment job now performs:

```text
checkout
    ↓
render task definition
    ↓
OIDC authentication
    ↓
Terraform setup
    ↓
Terraform init
    ↓
Terraform plan
    ↓
Terraform apply
    ↓
register task definition
    ↓
update ECS service + desired count 1
    ↓
wait for service stability
    ↓
HTTP verification
    ↓
report deployment
    ↓
scale ECS back to 0
    ↓
Terraform destroy
```

The deployment job timeout was increased from:

```text
10 minutes
```

to:

```text
20 minutes
```

because the job now includes:

```text
ALB provisioning
ECS startup
HTTP verification
cleanup
```

A very small job timeout would risk terminating the workflow before `always()` cleanup could execute.

---

## Build-once behavior remains intact

Issue #9 did not rebuild the application during deployment.

The existing architecture remains:

```text
test
    ↓
build Docker image once
    ↓
transfer exact image artifact
    ↓
push immutable SHA tag
    ↓
deploy exact SHA
```

The verification layer consumes the deployed result.

It does not create a second application artifact.

---

## Pull request behavior

The workflow intentionally does not deploy from a pull request.

The PR run for Issue #9 produced:

```text
Test and build demo API       success
Publish immutable image       skipped
Deploy to development         skipped
```

This preserves the existing security boundary:

```text
pull request
    → test/build only

push to main
    → AWS publication/deployment
```

The real verification lifecycle therefore ran only after the PR was merged to `main`.

---

## Successful automated verification

Issue #9 was merged through:

```text
PR #23
```

Merge commit:

```text
934b7de64bc2439ba7252362d110abc02230b4ea
```

GitHub Actions run:

```text
32373633227
```

Workflow conclusion:

```text
success
```

Jobs:

```text
Test and build demo API        success
Publish immutable image        success
Deploy to development          success
```

---

## Terraform creation evidence

The Terraform plan reported:

```text
Plan: 2 to add, 0 to change, 0 to destroy.
```

Created resources:

```text
Application Load Balancer
HTTP listener
```

Apply result:

```text
Apply complete! Resources: 2 added, 0 changed, 0 destroyed.
```

Temporary verification endpoint:

```text
http://zero-to-prod-dev-alb-272160783.eu-west-3.elb.amazonaws.com
```

---

## ECS deployment evidence

The workflow registered:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:4
```

The ECS service was updated to:

```text
Service: demo-api
TaskDefinition: zero-to-prod-demo-api:4
DesiredCount: 1
```

The workflow then waited until ECS reported a stable service.

---

## External health verification evidence

The workflow called:

```text
http://zero-to-prod-dev-alb-272160783.eu-west-3.elb.amazonaws.com/health
```

Observed response:

```json
{"status":"healthy"}
```

Workflow output:

```text
Health verification passed.
```

This proves the application was reachable through the external ALB rather than only visible through ECS control-plane state.

---

## External version verification evidence

Expected version:

```text
934b7de64bc2439ba7252362d110abc02230b4ea
```

Observed `/version` response:

```json
{"version":"934b7de64bc2439ba7252362d110abc02230b4ea"}
```

Observed version:

```text
934b7de64bc2439ba7252362d110abc02230b4ea
```

Workflow output:

```text
Version verification passed.
Deployment verification passed.
```

Therefore:

```text
expected Git SHA
        ==
externally observed application version
```

---

## Task definition verification

The workflow independently checked the ECS service after HTTP verification.

Reported state:

```text
Environment: development
Version: 934b7de64bc2439ba7252362d110abc02230b4ea
Task definition: arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:4
```

This connects:

```text
Git source revision
    ↓
immutable image tag
    ↓
task definition
    ↓
running ECS service
    ↓
externally observed application version
```

---

## Failure experiments

Issue #9 explicitly required four failure scenarios.

All four were exercised deliberately.

---

### Failure experiment 1 — application returns HTTP 500

A disposable local HTTP server returned:

```text
HTTP 500
```

The verifier retried according to policy.

Final evidence:

```text
Health response: {"error":"simulated failure"}
::error::Health request failed with curl exit code 22
```

Verifier exit:

```text
22
```

This proves an application HTTP failure produces a failed verification result with the response body retained for diagnostics.

---

### Failure experiment 2 — health endpoint times out

A disposable local server intentionally delayed the health response beyond the configured request limit.

The verifier enforced:

```text
--max-time 5
```

Final verifier exit:

```text
28
```

Diagnostic:

```text
::error::Health request failed with curl exit code 28
```

The temporary Python server later emitted a broken-pipe message because the client had already timed out and disconnected.

That was expected behavior of the disposable test server and not a verifier failure.

---

### Failure experiment 3 — old or wrong version remains deployed

The live healthy application was checked using an intentionally incorrect expected version:

```text
deadbeef
```

Health verification still passed.

Observed version remained the real deployed SHA.

The verifier reported:

```text
::error::Expected version deadbeef, observed <real version>
```

Verifier exit:

```text
1
```

This proves application health alone is insufficient.

A healthy but stale deployment is rejected.

---

### Failure experiment 4 — wrong endpoint

The verifier was pointed at an unreachable endpoint:

```text
http://127.0.0.1:1
```

The configured connection retry behavior executed.

Final verifier exit:

```text
7
```

This proves endpoint or connection failures do not produce false deployment success.

---

## Cost-conscious cleanup

The verification path temporarily creates the two important billable runtime components:

```text
one Fargate task
one Application Load Balancer
```

They exist only during verification.

The workflow scales ECS back to:

```text
desired count 0
```

using an `always()` cleanup step.

It then destroys:

```text
ALB listener
Application Load Balancer
```

using Terraform.

---

## Automated cleanup evidence

After successful verification, the workflow reported:

```text
Scaling demo-api back to zero...
```

The update result included:

```json
{
  "Service": "demo-api",
  "DesiredCount": 0,
  "RunningCount": 1
}
```

The workflow then waited for ECS stabilization.

Final cleanup message:

```text
Verification task stopped.
```

Terraform planned:

```text
0 to add, 0 to change, 2 to destroy
```

Destroy result:

```text
Destroy complete! Resources: 2 destroyed.
Verification infrastructure destroyed.
```

---

## Independent AWS-side cleanup verification

Cleanup was not trusted only because GitHub Actions reported success.

The AWS state was checked independently using the local SSO profile.

Final ECS state:

```json
{
  "Desired": 0,
  "Running": 0,
  "Pending": 0,
  "TaskDefinition": "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:4"
}
```

This proves the Fargate runtime returned to zero.

The ALB was then queried by name.

AWS returned:

```text
LoadBalancerNotFound
```

This proves the temporary Application Load Balancer was removed.

---

## Retained infrastructure

Low/no-idle-cost reusable resources remain available for future experiments:

```text
ECS cluster
ECS service at desired count 0
task definitions
target group
security groups
IAM roles and policies
ECR repository and images
```

The temporary ALB and Fargate runtime are not left running between experiments.

---

## What I learned

### 1. Control-plane success is not application success

Before Issue #9, the pipeline stopped after:

```text
ECS service stable
```

That proves orchestration state, not real application behavior.

A stronger deployment requires:

```text
platform verification
        +
application verification
```

---

### 2. Health and version answer different questions

`/health` answers:

```text
Is the application functioning?
```

`/version` answers:

```text
Is this the exact application revision I intended to deploy?
```

Both are necessary.

A healthy stale deployment should still fail verification.

---

### 3. Retry must be bounded

Retries are useful for normal transient startup behavior.

Unlimited retries would hide deployment failures.

The useful model is:

```text
retry
    +
timeout
    +
eventual hard failure
```

---

### 4. Failure diagnostics are part of deployment reliability

A verifier that only returns:

```text
exit 1
```

would make production debugging difficult.

Printing:

```text
target
response
expected version
observed version
curl exit code
```

makes failed deployments actionable.

---

### 5. Cleanup is part of the deployment design

Temporary infrastructure cannot be treated as an afterthought.

The actual lifecycle is:

```text
create
    ↓
verify
    ↓
destroy
```

not only:

```text
create
    ↓
verify
```

The cleanup path is especially important because the temporary resources have recurring cost.

---

### 6. `always()` improves cleanup but is not absolute

`if: always()` handles normal step failures.

It cannot guarantee execution after every possible runner failure or hard cancellation.

This is why the local Terraform-state limitation remains important.

---

### 7. CloudTrail can guide least privilege

Instead of granting broad ELB permissions and stopping there, actual API calls were observed.

That created a better sequence:

```text
perform controlled operation
    ↓
observe API calls
    ↓
map required actions
    ↓
scope resources where supported
    ↓
simulate policy
    ↓
apply
    ↓
verify stored policy
```

This was more reliable than guessing Terraform's AWS API behavior.

---

## Mistakes and corrections

### Initially considered migrating more infrastructure to Terraform

The verification work exposed a temptation to move the complete development environment into Terraform.

That would have expanded Issue #9 significantly.

The scope was corrected to:

```text
Terraform owns temporary ALB + listener only
```

Existing ECS, target-group, network, and security-group resources remain outside this Terraform state.

---

### Local Terraform state has a recovery limitation

The current workflow uses runner-local state.

That works correctly during the normal same-job lifecycle.

However, a runner that disappears after `apply` but before `destroy` could leave an orphaned ALB.

This is now an explicit known limitation rather than an undocumented assumption.

---

### Cleanup conditions needed to handle partial failures

Cleanup cannot depend only on successful verification.

The workflow uses step outcomes together with:

```text
if: always()
```

so cleanup can run when a preceding deployment or verification step fails after resources have already been created.

---

### Job timeout needed to account for cleanup

The previous deployment timeout was designed for a shorter ECS-only workflow.

Once ALB provisioning and destruction were added, that value became too small.

The job timeout was increased to reduce the risk of GitHub terminating the job before cleanup.

---

## Knowledge gaps identified

Topics worth exploring later include:

```text
Terraform remote state
state locking
orphan-resource recovery
deployment rollback
blue/green deployment
ALB target deregistration delay
synthetic monitoring
HTTPS verification
DNS-based stable endpoints
deployment health budgets
progressive delivery
canary deployment
GitHub Actions cancellation behavior
AWS CodeDeploy ECS deployments
```

These were deliberately kept outside Issue #9.

---

## Acceptance criteria

Issue #9 required:

```text
Workflow calls /health after deployment.
```

Verified in GitHub Actions run:

```text
32373633227
```

Observed:

```text
Health response: {"status":"healthy"}
Health verification passed.
```

---

Issue #9 required:

```text
Verification checks the deployed version.
```

Expected:

```text
934b7de64bc2439ba7252362d110abc02230b4ea
```

Observed:

```text
934b7de64bc2439ba7252362d110abc02230b4ea
```

Result:

```text
Version verification passed.
```

---

Issue #9 required:

```text
Timeout and retry behaviour are defined.
```

Implemented:

```text
connect timeout: 3 seconds
request max time: 5 seconds
retries: 4 after initial request
retry delay: 2 seconds
retry connection refused: enabled
```

---

Issue #9 required:

```text
Failed verification fails the deployment.
```

Failure experiments produced non-zero verifier exits:

```text
HTTP 500        → 22
timeout         → 28
wrong version   → 1
wrong endpoint  → 7
```

The verifier runs as a normal GitHub Actions step, so non-zero verification exits fail the deployment job.

---

Issue #9 required:

```text
Useful diagnostics are printed.
```

Verified diagnostics include:

```text
verification target
expected version
health response
version response
observed version
curl exit code
GitHub ::error:: messages
```

All acceptance criteria are satisfied.

---

## Focused work performed

Issue #9 included:

```text
existing deployment-path review
verification architecture design
temporary ALB cost analysis
Terraform installation
Terraform version pinning
AWS provider pinning
Terraform temporary ingress implementation
Terraform plan/apply/destroy testing
independent ALB verification
ECS scale-up testing
external /health verification
external /version verification
version mismatch test
HTTP 500 failure experiment
timeout failure experiment
wrong-endpoint failure experiment
CloudTrail permission discovery
least-privilege IAM design
IAM simulation
live IAM policy deployment
stored-policy comparison
GitHub Actions integration
cleanup design
workflow validation
actionlint validation
Terraform validation
shell syntax validation
successful PR CI
successful main deployment
successful automated external verification
independent ECS cleanup verification
independent ALB cleanup verification
documentation
```

The work remained evidence-first:

```text
change
    ↓
observe
    ↓
verify
    ↓
test failure
    ↓
verify cleanup
```

rather than assuming that a green control-plane deployment meant the application was correct.

---

## Session record

Focused time:

```text
~2h 10m
```

Primary successful GitHub Actions run:

```text
32373633227
```

Verified merge SHA:

```text
934b7de64bc2439ba7252362d110abc02230b4ea
```

Verified ECS task definition:

```text
zero-to-prod-demo-api:4
```

---

## Result

Sprint 01 now has the following verified delivery path:

```text
source commit
    ↓
CI tests
    ↓
Docker build
    ↓
exact image artifact
    ↓
immutable ECR SHA tag
    ↓
ECS task definition
    ↓
development ECS service
    ↓
ECS stable
    ↓
external /health
    ↓
external /version
    ↓
exact SHA match
    ↓
deployment verified
    ↓
runtime cleanup
```

Issue #9 closes the gap between:

```text
"the infrastructure says deployment succeeded"
```

and:

```text
"the deployed application is externally reachable,
healthy, and is the exact expected revision"
```

---

## Next experiment

Sprint 01's next capability should address deployment failure recovery.

The next question is:

```text
What should happen when a new deployment fails verification?
```

A useful next experiment should explore rollback behavior while preserving the properties already established:

```text
immutable artifacts
least-privilege AWS access
external health verification
exact version verification
bounded failure
evidence-driven cleanup
```

The desired progression is:

```text
deploy candidate
    ↓
verify candidate
    ↓
success → keep candidate
failure → restore last known-good version
```

That belongs to the next issue rather than expanding Issue #9.
