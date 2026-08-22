# Sprint 01 — Final Reflection

## Original capability

Sprint 01 started with one end-to-end objective:

> Given a tested commit in the `zero-to-prod` repository, use GitHub Actions to transform it into an immutable container image, deploy that image to an AWS development environment without permanent AWS credentials, verify the running application, and recover to a known-good version when necessary.

The original target path was:

```text
Git commit
    ↓
GitHub Actions
    ↓
automated tests
    ↓
Docker image build
    ↓
GitHub OIDC authentication
    ↓
Amazon ECR
    ↓
ECS task-definition revision
    ↓
ECS Fargate deployment
    ↓
service stability
    ↓
/health + /version verification
    ↓
rollback when necessary
```

The sprint deliberately focused on one small Go service, one AWS sandbox account, one development environment, and one delivery path.

Production deployment, Kubernetes, multi-account design, advanced observability, canary or blue/green deployment, and automatic alarm-driven rollback were outside the Sprint 01 target.

The original plan also treated broader Terraform-based infrastructure provisioning as out of scope. During Issue #9, that boundary was refined rather than abandoned: Terraform was introduced only for the temporary verification ALB and listener because those resources needed a reproducible create/destroy lifecycle.

## What Sprint 01 delivered

Sprint 01 completed the intended development delivery path and added evidence for both success and failure behavior.

### GitHub-to-AWS authentication

GitHub Actions authenticates to the AWS sandbox through OIDC and AWS STS rather than stored long-lived AWS access keys.

The IAM trust relationship accepts the verified identities for:

```text
main branch
development GitHub environment
```

and AWS-facing jobs receive temporary credentials through:

```text
arn:aws:iam::333534066371:role/zero-to-prod-github-actions
```

### Immutable artifact publication

The demo API is built once in CI and identified by the full Git commit SHA.

The exact Docker artifact is transferred between GitHub Actions jobs rather than rebuilt before publication.

Amazon ECR stores the image using immutable SHA tags, and a deliberate overwrite experiment proved that an existing tag cannot be reassigned to another image digest.

### ECS Fargate deployment

A push to `main` can deploy the selected immutable image to:

```text
Cluster: zero-to-prod-dev
Service: demo-api
```

The reusable ECS task-definition template does not contain a stale image SHA. The workflow renders the exact ECR image into a new task-definition revision before deployment.

Pull requests validate the application and Docker build but do not publish an image or deploy to AWS.

### External deployment verification

Deployment verification goes beyond ECS control-plane stability.

The workflow temporarily creates an internet-facing ALB and listener with Terraform, starts the service at desired count `1`, waits for ECS stability, and verifies:

```text
/health
/version
```

through the external endpoint.

`/version` must exactly match the expected Git SHA.

The verifier has bounded connection, request, and retry behavior and preserves useful diagnostics on failure.

### Cost-conscious cleanup

After deployment or rollback verification, the workflow scales the ECS service back to:

```text
desired = 0
```

and destroys the temporary verification ALB.

Normal failure cleanup uses `if: always()` with step-outcome guards.

Independent AWS checks repeatedly confirmed:

```text
desired = 0
running = 0
pending = 0
temporary verification ALB = absent
```

### Deployment safety and rollback

Normal deployment and rollback share:

```text
concurrency group: demo-api-development-deployment
cancel-in-progress: false
GitHub environment: development
```

Rollback is explicit and workflow-assisted rather than automatic.

It requires a full 40-character target SHA and `ROLLBACK` confirmation, verifies that the immutable image already exists in ECR, and registers a new task-definition revision using that existing image.

The controlled recovery experiment successfully rolled back to:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

and externally verified the known-good version.

Measured rollback recovery time was:

```text
130 seconds
```

The final normal deployment afterward succeeded with task definition:

```text
zero-to-prod-demo-api:8
```

and Issue #11 independently re-verified the final `0/0/0` runtime state with the temporary ALB absent.

## What I learned

### Authentication and authorization are separate controls

Successful GitHub OIDC authentication proves only that the workflow can assume an AWS role.

It does not prove that the role can:

```text
push to ECR
update ECS
pass an execution role
create verification infrastructure
```

Sprint 01 produced separate failures for identity trust and ECS authorization, which made this distinction concrete.

### Artifact identity and immutability are different

A full Git SHA tag identifies a source version, but the tag alone does not prevent reassignment.

The complete artifact-integrity model requires:

```text
full Git SHA
    +
build once
    +
immutable ECR tag
```

Each control solves a different problem.

### Build once matters across CI jobs

Separate GitHub Actions jobs do not share Docker daemon state.

Explicitly transferring the built Docker image prevents the publishing or deployment stage from silently rebuilding a different artifact.

### ECS stability is not application verification

`aws ecs wait services-stable` proves ECS control-plane convergence.

It does not prove that an external client can reach the application or that the correct artifact is running.

External verification is therefore a separate deployment stage.

### Health and version answer different questions

`/health` answers:

```text
is the application functioning?
```

`/version` answers:

```text
is this the exact artifact we intended to deploy?
```

The controlled wrong-version experiment proved that a healthy application can still be the wrong deployment.

### Failure handling needs explicit bounds and diagnostics

Retries without timeout limits can hide or prolong deployment failures.

The verifier therefore uses bounded connection time, request time, retry count, and retry delay while retaining useful response diagnostics.

Different failure modes also produce useful distinctions:

```text
HTTP failure
timeout
connection failure
version mismatch
```

### Cleanup is part of deployment correctness

Temporary infrastructure is not merely a testing convenience.

If deployment verification creates runtime resources, returning the environment to its intended baseline is part of the deployment lifecycle itself.

Cleanup should also be checked independently when cost or operational state matters.

### `if: always()` is useful but not absolute

GitHub Actions cleanup conditions handle normal step failures, but they cannot guarantee execution after every hard runner termination.

This made Terraform state strategy part of the reliability design rather than only an infrastructure implementation detail.

### Rollback should select a known artifact, not recreate history

Rebuilding an old commit during an incident could produce an artifact that differs from the one previously tested.

Rollback is safer and easier to reason about when it selects an existing immutable image and applies current deployment configuration around that artifact.

### Deployment concurrency is part of correctness

Deployment and rollback both mutate the same ECS service.

Serializing them with one concurrency group avoids intentional overlapping mutations of the development environment.

### Approval has different levels

Typing `ROLLBACK` is a useful operator confirmation guard.

It is not equivalent to independent reviewer approval.

Sprint 01 taught me to describe the actual control that exists instead of using stronger production terminology that the implementation does not support.

## Mistakes and corrections

### I assumed a feature-branch push would trigger the workflow

The workflow was configured for:

```text
push → main
pull_request → main
```

I initially expected a direct push to the feature branch to start CI.

It did not.

Instead of changing the workflow immediately, I checked the configured triggers and GitHub behavior first.

That reinforced a simple rule:

> Verify trigger behavior from configuration and observed runs rather than assuming how CI will react.

### My first ECR immutability test tested the wrong behavior

My first `put-image` experiment reused:

```text
same tag
same manifest
```

and returned:

```text
ImageAlreadyExistsException
```

That only proved duplicate-image detection.

It did not prove that an immutable tag could not be reassigned.

I corrected the experiment by attempting to associate the existing tag with a different image digest.

ECR then returned:

```text
ImageTagAlreadyExistsException
```

and the original tag-to-digest mapping remained unchanged.

The lesson was that a failure test must actually exercise the property it claims to prove.

### I initially overlooked artifact transfer between GitHub jobs

The Docker image produced in the build job existed only on that runner.

There was initially no:

```text
docker save / docker load
upload-artifact / download-artifact
```

path between the build and publishing jobs.

I corrected the design so the tested image is exported, transferred, loaded by the publishing job, and pushed without rebuilding it.

This made “build once” an actual implementation property instead of only an intention.

### I almost expanded Terraform beyond the issue capability

While implementing external verification, I considered migrating more of the development environment into Terraform.

That would have turned a deployment-verification issue into a much larger infrastructure-migration task.

I corrected the scope to:

```text
Terraform owns temporary verification ALB + listener only
```

while the existing ECS service, target group, networking, and security groups remain outside that state.

This reinforced the value of adding the smallest infrastructure capability necessary for the experiment.

### I discovered that local Terraform state weakens cleanup recovery

The same-job:

```text
terraform apply
    ↓
verification
    ↓
terraform destroy
```

lifecycle works during normal workflow execution.

However, runner-local state means a hard runner termination after `apply` but before `destroy` could leave an orphaned ALB that a fresh runner cannot automatically reconstruct from state.

Instead of treating `if: always()` as a complete solution, I recorded this as an explicit limitation.

That changed how I think about Terraform state: state storage is part of recovery design, not merely an implementation detail.

### I initially overlooked the OIDC subject change introduced by the GitHub environment

Adding:

```yaml
environment: development
```

changed the OIDC identity used by the deployment job.

The first environment-bound deployment therefore failed with:

```text
Not authorized to perform sts:AssumeRoleWithWebIdentity
```

because the IAM trust policy still accepted only the branch-based subject.

I corrected the trust relationship by preserving the exact branch subject for image publication and adding the exact `development` environment subject for deployment.

No wildcard trust was introduced.

This failure made the distinction between identity trust and AWS service permissions much clearer.

### I made an assumption about GitHub environment support before testing it

I initially assumed the repository could not use GitHub Environments under its current plan and visibility.

The actual repository behavior proved that the `development` environment could be created and used.

I corrected the assumption based on direct evidence.

The remaining limitation is not environment usage itself, but the level of independent reviewer protection available in the current setup.

## Knowledge gaps

Sprint 01 exposed several gaps in my understanding. Some were resolved through the experiments; others remain deliberate follow-up topics.

### Gaps I closed during the sprint

#### Docker health checks and ECS health checks are not the same configuration

I initially expected the Dockerfile `HEALTHCHECK` to automatically provide the ECS container health check.

For the ECS task, the health check had to be represented explicitly in the task definition.

This helped me separate:

```text
container-local process health
ECS task health
load-balancer target health
external application verification
```

as distinct layers.

#### IAM simulation results require resource-level inspection

When simulating policies across multiple resources, reading only the aggregate IAM simulator output was misleading.

The useful decisions were under:

```text
ResourceSpecificResults
```

This reinforced that authorization debugging needs to inspect the actual action-resource decision rather than only a summary result.

#### Workflow timeout must include the cleanup lifecycle

The original deployment timeout was appropriate for the shorter ECS-only workflow.

Once verification added:

```text
Terraform apply
ALB provisioning
ECS deployment
external verification
scale-down
Terraform destroy
```

the timeout also had to account for cleanup.

This made job timeout part of deployment safety rather than just a CI convenience.

### Gaps that remain open

The following topics were identified but deliberately kept outside Sprint 01:

```text
Terraform remote state and state locking
orphan-resource recovery after hard runner failure
GitHub Actions cancellation behavior
GitHub concurrency pending-run semantics
required-reviewer environment protection
two-person production approval
automatic rollback policy
rollback configuration compatibility
deployment history and release metadata
change-aware CI/CD
blue/green deployment
canary and progressive delivery
deployment health budgets
ALB target deregistration behavior
synthetic monitoring
HTTPS verification
DNS-based stable verification endpoints
AWS CodeDeploy ECS deployment strategies
```

These are not requirements that Sprint 01 failed to complete. They are the next layer of reliability, deployment safety, and operational maturity exposed by completing the basic end-to-end delivery path.

## Known limitations

### Terraform state is runner-local

The temporary verification ALB and listener use Terraform state stored only on the GitHub Actions runner.

During normal execution:

```text
apply
    ↓
verify
    ↓
destroy
```

happens in the same job and works correctly.

A hard runner termination after `apply` but before `destroy` could leave an orphaned ALB while a fresh runner would not possess the previous local state.

Remote state, locking, and a tested orphan-recovery procedure are not implemented yet.

### Cleanup is strongly attempted, not mathematically guaranteed

The workflow uses `if: always()` and step-outcome guards and has successfully cleaned up after controlled verification failures.

However, GitHub Actions cannot execute cleanup code after every possible hard runner or infrastructure termination.

The final AWS baseline therefore needs to remain independently observable and recoverable.

### Rollback requires an operator

Rollback is not automatic.

After a deployment failure, cleanup runs first and an operator must deliberately dispatch the rollback workflow with:

```text
full 40-character SHA
confirmation = ROLLBACK
```

Sprint 01 does not define an automatic policy for deciding when a failed deployment should roll back.

### Rollback confirmation is not independent approval

The `ROLLBACK` confirmation protects against accidental invocation, and the workflow is attached to the `development` GitHub environment.

It does not implement a two-person production approval process or independent reviewer authorization.

### The environment is development-only

Sprint 01 proves the delivery capability in one AWS sandbox account and one development ECS service.

It does not prove production concerns such as:

```text
multi-account promotion
production approval
high availability
progressive traffic shifting
automatic rollback policy
production observability
```

### External verification currently uses temporary public HTTP ingress

The verification ALB is internet-facing and listens on HTTP port `80`.

Task port `8080` remains restricted to traffic from the ALB security group, but Sprint 01 does not implement HTTPS, a stable DNS verification endpoint, or private synthetic verification.

The public ALB exists only for the bounded deployment-verification window and is destroyed afterward.

### The development service intentionally returns to zero runtime

After verification, `demo-api` is scaled back to desired count `0`.

This minimizes idle runtime cost for the learning environment, but it means Sprint 01 is not demonstrating an always-on production service.

### CI is not change-aware

The current workflow is triggered by pull requests to `main` even when a change affects documentation only.

Sprint 01 does not yet skip application build validation based on changed paths.

This is safe but inefficient and is a useful future CI/CD improvement.

## Focused time

Sprint 01 was originally planned with a maximum investment of:

```text
20 focused hours
```

Recorded or reconstructed time by issue:

```text
Issue #1   Define target capability                  0h 45m
Issue #2   Build the demo API                        3h 00m
Issue #3   Containerize the application              1h 00m
Issue #4   Create the CI workflow                    ~1h 00m
Issue #5   Prepare AWS sandbox and ECR               4h 00m
Issue #6   Configure GitHub-to-AWS OIDC             ~1h 00m
Issue #7   Push immutable images to ECR              1h 51m
Issue #8   Deploy the service to development        ~1h 55m
Issue #9   Add automated health verification        ~2h 10m
Issue #10  Add deployment safety and rollback       ~3h 30m
Issue #11  Document and demonstrate the system      ~2h
```

Issue #4 did not have an explicit focused-time value in its completion comment. Its value was reconstructed conservatively from the original project session and repository history, so the total should not be treated as minute-accurate.

Total Sprint 01 focused investment:

```text
~22 hours
```

Compared with the original plan:

```text
planned: ~20h
actual:  ~22h
variance: ~2h / ~10%
```

The largest estimation gap came from Issue #5. What initially looked like a small AWS sandbox and ECR task exposed missing account and identity foundations, which required AWS Organizations, IAM Identity Center, SSO-based CLI access, account separation, cost guardrails, and account-safety checks before workload provisioning.

The Sprint exceeded the original estimate, but the overrun produced capabilities that became prerequisites for every later AWS experiment rather than unrelated scope.

## What I can now explain

At the end of Sprint 01, I can explain the complete delivery path from source code to a verified AWS deployment and back to a safe runtime baseline.

I can explain:

```text
why the application exposes /health, /ready, and /version
how the Git SHA becomes application and image identity
why the container runs as a non-root user
why multi-stage Docker builds reduce runtime contents
how pull-request CI prevents broken changes from reaching main
why one Docker artifact should be built once and promoted
why separate GitHub Actions jobs require explicit artifact transfer
why ECR tag immutability is separate from SHA-based naming
how GitHub OIDC exchanges identity for temporary AWS credentials
how an IAM trust policy differs from an IAM permissions policy
why branch and environment OIDC subjects must match exactly
how scoped ECR, ECS, PassRole, and verification permissions differ
how ECS task definitions select the exact container image
why ECS service stability is not enough to prove a deployment works
how container, ECS, target-group, and external health checks differ
why /version detects a healthy but stale or incorrect deployment
how timeout, retry, and curl exit codes improve failure diagnosis
why temporary verification infrastructure has a cleanup lifecycle
why Terraform state affects orphan-resource recovery
why deployment and rollback need shared concurrency protection
why rollback should reuse an existing immutable artifact
why operator confirmation is not the same as independent approval
how to verify the AWS runtime baseline independently after a workflow
```

I can also walk through a real failure and recovery sequence:

```text
candidate image deploys
    ↓
ECS reaches stable state
    ↓
application responds successfully
    ↓
/version reports the wrong SHA
    ↓
deployment verification fails
    ↓
cleanup returns ECS to desired count 0
    ↓
temporary ALB is destroyed
    ↓
operator selects known-good immutable SHA
    ↓
rollback deploys that existing image
    ↓
/health and /version verify recovery
    ↓
environment returns to the zero-runtime baseline
```

The most important change from the start of the sprint is that I no longer think of deployment as:

```text
build → push → run
```

I now think of it as:

```text
identity
    ↓
build and test
    ↓
immutable artifact
    ↓
authenticated and authorized delivery
    ↓
runtime convergence
    ↓
external verification
    ↓
failure handling
    ↓
cleanup
    ↓
recoverability
```

## Next experiment — Sprint 02

Sprint 01 proved a complete development deployment and recovery path.

The most important reliability limitation it exposed is that temporary verification infrastructure depends on runner-local Terraform state.

My first Sprint 02 experiment should answer:

```text
GitHub Actions creates temporary infrastructure
    ↓
runner disappears before cleanup
    ↓
can another execution discover ownership,
recover the Terraform state safely,
and remove the orphaned infrastructure?
```

The capability to develop is:

> Make temporary deployment infrastructure recoverable across CI runner failure instead of depending on one runner surviving from `terraform apply` through `terraform destroy`.

The experiment should remain narrow.

Initial topics to investigate are:

```text
remote Terraform state
state locking
state ownership and naming
safe state initialization from CI
failure after apply but before destroy
orphan-resource detection
recovery from a fresh runner
independent AWS baseline verification
```

A successful experiment should demonstrate, not merely configure, that:

```text
temporary verification infrastructure is created
    ↓
the original execution is intentionally interrupted
    ↓
a fresh execution can access the correct state
    ↓
the orphaned infrastructure can be identified safely
    ↓
cleanup succeeds
    ↓
AWS independently confirms the zero-runtime baseline
```

This keeps Sprint 02 connected to a real limitation discovered in Sprint 01 rather than introducing a new technology only for its own sake.

Other open topics such as HTTPS verification, production approvals, deployment history, change-aware CI, blue/green delivery, and automatic rollback remain candidates for later experiments after the infrastructure-recovery path is understood.
