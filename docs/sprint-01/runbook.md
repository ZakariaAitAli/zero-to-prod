# Sprint 01 — Operations Runbook

## Purpose

This runbook provides evidence-backed troubleshooting and recovery procedures for the Sprint 01 deployment system.

## OIDC role assumption fails

### Symptom

The GitHub Actions `Configure AWS credentials` step fails with an error such as:

```text
Not authorized to perform sts:AssumeRoleWithWebIdentity
```

Sprint 01 reproduced this failure in GitHub Actions run:

```text
32503496084
```

### What it means

This failure occurs during AWS role assumption, before ECS or Terraform deployment permissions are evaluated.

In the observed Sprint 01 failure, adding:

```yaml
environment: development
```

changed the GitHub OIDC subject from:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
```

to:

```text
repo:ZakariaAitAli/zero-to-prod:environment:development
```

The IAM trust policy did not yet allow the environment-bound subject.

### Check

Inspect the repository trust policy:

```bash
cat infra/aws/iam/github-actions-trust-policy.json
```

The current policy should allow exactly:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
repo:ZakariaAitAli/zero-to-prod:environment:development
```

Also verify that the AWS-facing workflow job has:

```yaml
permissions:
  id-token: write
```

### Recovery

Correct the OIDC trust relationship or workflow identity mismatch before investigating ECS or ECR service permissions.

Do not add broader AWS service permissions to solve an `AssumeRoleWithWebIdentity` failure.

In the observed failure, authentication stopped before Terraform or ECS mutation occurred.

## ECS deployment authorization fails

### Symptom

OIDC authentication succeeds, but the deployment fails during the ECS service update with an error such as:

```text
AccessDeniedException
```

Sprint 01 reproduced this failure by temporarily removing:

```text
ecs:UpdateService
```

from the GitHub Actions role.

The observed sequence was:

```text
OIDC authentication            success
render task definition         success
register task definition       success
update ECS service             failure
wait for service stability     skipped
deployment verification        skipped
```

### What it means

Authentication succeeded, but the assumed IAM role did not have permission to mutate the ECS service.

A registered task definition alone does not mean a deployment occurred.

In the failure experiment, task definition:

```text
zero-to-prod-demo-api:3
```

was registered, but the service remained on the previous task definition:

```text
zero-to-prod-demo-api:2
```

### Check

Inspect the deployment policy:

```bash
cat infra/aws/iam/github-actions-ecs-deploy-policy.json
```

The role should allow:

```text
ecs:UpdateService
ecs:DescribeServices
```

for:

```text
arn:aws:ecs:eu-west-3:333534066371:service/zero-to-prod-dev/demo-api
```

Also check whether the workflow failed before or after `aws ecs update-service`.

### Recovery

Restore or correct the required ECS authorization, then rerun the intended deployment.

Do not treat successful OIDC authentication or successful task-definition registration as proof that the service was updated.

After an authorization failure, inspect ECS independently to confirm which task definition the service is actually using.

## ECS service does not stabilize

> This condition was not deliberately reproduced during Sprint 01. The procedure below is an operational troubleshooting path based on the implemented ECS health and deployment model.

### Symptom

The workflow reaches:

```text
Wait for ECS service stability
```

but `aws ecs wait services-stable` does not complete successfully within the workflow execution window.

### Check ECS deployment state

Inspect the service counts, deployment rollout state, and recent ECS events:

```bash
aws ecs describe-services \
  --cluster zero-to-prod-dev \
  --services demo-api \
  --region eu-west-3 \
  --profile sandbox \
  --query 'services[0].{Desired:desiredCount,Running:runningCount,Pending:pendingCount,Deployments:deployments[*].{Status:status,RolloutState:rolloutState,Desired:desiredCount,Running:runningCount,Pending:pendingCount},Events:events[0:10].[createdAt,message]}' \
  --output json
```

Look for:

```text
desired / running / pending counts that do not converge
rolloutState that does not become COMPLETED
repeated task start or stop events
target registration or deregistration messages
other ECS service errors in recent events
```

Do not move on to external HTTP verification until ECS has reached a stable deployment state.

### Check the health layers

Sprint 01 has more than one health layer:

```text
ECS container health
    → GET /health inside the task

ALB target-group health
    → GET /health on task port 8080

external verifier
    → /health + exact /version through the temporary ALB
```

The ECS task definition contains the container-local health check, and the existing target group expects `/health` to return HTTP `200`.

A stabilization problem therefore needs to be diagnosed before treating the issue as an external-verification failure.

### Recovery

Use the ECS service events and deployment state to identify the failing layer before changing the service.

If the service cannot reach a stable deployment, do not interpret task-definition registration or `UpdateService` success as proof that the candidate is healthy.

After recovery or cleanup, independently verify the service returns to the expected runtime baseline.

## HTTP 500 during verification

### Symptom

The external verification step fails while checking `/health`.

Sprint 01 reproduced this with a disposable server returning:

```text
HTTP 500
```

The verifier retained the response body:

```text
Health response: {"error":"simulated failure"}
```

and reported:

```text
::error::Health request failed with curl exit code 22
```

Final verifier exit:

```text
22
```

### What it means

`curl --fail-with-body` treats the HTTP 500 response as a failed request while still preserving the body for diagnostics.

The verifier retries according to its bounded retry policy before returning failure.

### Check

Inspect the GitHub Actions verification output for:

```text
Health response:
Health request failed with curl exit code 22
```

The response body may contain application-level information explaining the failure.

### Recovery

Treat the deployment as failed.

Do not interpret ECS service stability as proof that the application is healthy. Diagnose the application response first.

The normal workflow cleanup should still attempt to scale ECS back to zero and destroy the temporary verification infrastructure.

## Health verification times out

### Symptom

The external verifier cannot receive the `/health` response within its configured request limit.

Sprint 01 reproduced this with a disposable server that intentionally delayed its response beyond:

```text
--max-time 5
```

The verifier reported:

```text
::error::Health request failed with curl exit code 28
```

Final verifier exit:

```text
28
```

### What it means

Curl exit code `28` indicates that the request exceeded its configured time bound.

The verifier uses explicit network bounds:

```text
--connect-timeout 3
--max-time 5
--retry 4
--retry-delay 2
--retry-connrefused
```

This prevents deployment verification from waiting indefinitely for an unhealthy or excessively slow endpoint.

### Check

Inspect the verification logs for:

```text
Health request failed with curl exit code 28
```

Confirm whether the failure occurred while connecting or while waiting for the application response.

Also distinguish this from ECS stabilization: ECS may report a stable service while the externally observed application still fails to respond within the verification limit.

### Recovery

Treat the candidate as unverified and the deployment as failed.

Investigate why `/health` is not responding within the expected bound before increasing timeout values.

Do not remove or substantially increase the verifier bounds merely to make the deployment pass.

The normal cleanup path should still attempt to scale ECS back to zero and destroy the temporary verification infrastructure.

## Verification endpoint is unreachable

### Symptom

The verifier cannot establish a connection to the configured verification endpoint.

Sprint 01 reproduced this by pointing the verifier at:

```text
http://127.0.0.1:1
```

The configured connection retry behavior executed and the verifier exited with:

```text
7
```

### What it means

Curl exit code `7` indicates that the verifier could not connect to the endpoint.

This is different from:

```text
HTTP 500    → application responded with an error
timeout     → request exceeded its configured time bound
exit 7      → connection to the endpoint could not be established
```

### Check

Inspect the verification log for the target URL and curl exit code.

Confirm that:

```text
BASE_URL
```

points to the temporary verification ALB created by the workflow.

If the endpoint was expected to exist, verify that the temporary ALB was created successfully before investigating the application itself.

### Recovery

Treat the deployment as unverified.

Diagnose the verification ingress or endpoint configuration rather than interpreting the failure as an application HTTP error.

The normal cleanup path should still attempt to scale ECS back to zero and destroy any temporary verification infrastructure that was successfully created.

## Deployed version does not match the expected SHA

### Symptom

The application passes `/health`, but `/version` does not equal the expected deployment SHA.

Sprint 01 reproduced this by checking a healthy deployment with an intentionally incorrect expected version:

```text
deadbeef
```

Health verification passed, but the verifier reported:

```text
::error::Expected version deadbeef, observed <real version>
```

Final verifier exit:

```text
1
```

The same failure mode was later reproduced in the real deployment workflow with controlled candidate:

```text
e7eec825bd16634a19e31a82a7140083e3360bd9
```

while verification intentionally expected:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

GitHub Actions run:

```text
32506584207
```

failed during external version verification.

### What it means

The application is reachable and healthy, but the running artifact is not the version the deployment intended to verify.

ECS stability and health checks are therefore insufficient to prove artifact correctness.

### Check

Inspect the verification output for:

```text
Expected version:
Observed version:
```

Compare the observed `/version` value with the full Git SHA expected by the workflow.

Also inspect the ECS service task definition if necessary to determine which image was selected.

### Recovery

Treat the candidate deployment as failed.

The normal workflow should clean up the verification runtime first.

If recovery requires rollback, explicitly dispatch the rollback workflow using a known-good full SHA that already exists as an immutable ECR image.

Do not rebuild the old source as part of rollback.

## Rollback image does not exist

### Symptom

A manually requested rollback fails during the immutable-image existence check before ECS deployment begins.

Sprint 01 tested the deliberately nonexistent SHA:

```text
0000000000000000000000000000000000000000
```

Amazon ECR returned:

```text
ImageNotFound
```

and the rollback guard returned non-zero.

### What it means

Rollback is allowed to select only an image that has already been published to:

```text
zero-to-prod-demo-api
```

The rollback workflow uses `ecr:BatchGetImage` and checks for an image digest before registering or deploying a rollback task definition.

A syntactically valid 40-character SHA is therefore not sufficient by itself.

### Check

Verify the requested image directly:

```bash
aws ecr batch-get-image \
  --repository-name zero-to-prod-demo-api \
  --image-ids imageTag="<FULL_40_CHARACTER_SHA>" \
  --region eu-west-3 \
  --profile sandbox \
  --query '{Images:images[*].imageId,Failures:failures}' \
  --output json
```

A valid rollback target should return an image with an `imageDigest`.

### Recovery

Choose a known-good full Git SHA that already exists in the immutable ECR repository.

Do not rebuild missing historical source merely to satisfy the rollback request.

The rollback must reuse an already-published artifact so that recovery preserves the same artifact-integrity model as normal deployment.

## Temporary verification ALB may remain

### Symptom

A deployment or rollback workflow terminates unexpectedly after the temporary verification infrastructure was created, and the verification ALB may still exist.

Normal verification failures are handled by cleanup steps using:

```yaml
if: always()
```

but this cannot guarantee cleanup after every hard runner termination or hard workflow cancellation.

### What it means

Terraform state for the temporary verification infrastructure is local to the GitHub Actions runner.

The normal lifecycle is:

```text
terraform plan
    ↓
terraform apply
    ↓
deployment verification
    ↓
terraform destroy
```

If the runner disappears after `apply` but before `destroy`, a fresh runner does not automatically have the previous local Terraform state required to resume that destroy operation.

This is a documented Sprint 01 limitation.

### Check

Verify whether the temporary ALB still exists:

```bash
aws elbv2 describe-load-balancers \
  --names zero-to-prod-dev-alb \
  --region eu-west-3 \
  --profile sandbox
```

The expected clean-baseline result is:

```text
LoadBalancerNotFound
```

If the command returns a load balancer instead, do not assume the previous workflow cleaned up successfully.

### Recovery

Treat an unexpectedly retained verification ALB as an orphaned temporary resource that requires deliberate investigation and cleanup.

Before deleting anything, confirm that it is the Sprint 01 verification ALB and inspect the related listener and target-group relationship.

Do not assume that rerunning `terraform destroy` from a fresh checkout will reconstruct the missing runner-local state.

Remote Terraform state or another orphan-resource recovery mechanism remains outside Sprint 01.

## Verify the final runtime baseline

After deployment, rollback, or recovery work, verify cleanup independently rather than relying only on the GitHub Actions result.

### Verify ECS

```bash
aws ecs describe-services \
  --cluster zero-to-prod-dev \
  --services demo-api \
  --region eu-west-3 \
  --profile sandbox \
  --query 'services[0].{Service:serviceName,Desired:desiredCount,Running:runningCount,Pending:pendingCount,TaskDefinition:taskDefinition}' \
  --output json
```

Expected clean runtime state:

```text
Desired = 0
Running = 0
Pending = 0
```

The task-definition ARN may advance over time as deployments or rollbacks register new revisions. The important cleanup condition is the runtime count returning to `0/0/0`.

### Verify the temporary ALB is absent

```bash
aws elbv2 describe-load-balancers \
  --names zero-to-prod-dev-alb \
  --region eu-west-3 \
  --profile sandbox
```

Expected clean-baseline result:

```text
LoadBalancerNotFound
```

### Issue #11 re-verification

During final Sprint 01 documentation, the sandbox was checked again independently.

Observed ECS state:

```text
Service: demo-api
Desired: 0
Running: 0
Pending: 0
TaskDefinition: zero-to-prod-demo-api:8
```

Observed verification ALB state:

```text
LoadBalancerNotFound
```

This confirms the final Sprint 01 runtime baseline remains clean.
