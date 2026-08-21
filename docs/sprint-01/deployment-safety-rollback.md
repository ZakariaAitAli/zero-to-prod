# Sprint 01 — Add Deployment Safety and Rollback

## Purpose

Issue #10 extends the deployment pipeline from:

```text
deploy candidate
    ↓
verify candidate
    ↓
report success or failure
```

to a safer operational model:

```text
serialize deployment mutations
        ↓
deploy candidate
        ↓
verify candidate
        ↓
success → candidate accepted
failure → select known-good immutable image
        ↓
rollback
        ↓
verify restored version externally
        ↓
record recovery duration
        ↓
cleanup
```

Issue #9 proved that a deployment could be checked externally through:

```text
/health
/version
```

and that failed verification produced a non-zero deployment result.

Issue #10 answers the next question:

```text
What happens after verification fails?
```

---

## Target capability

The target capability was:

```text
Make deployment mutation safer and provide a tested,
operator-controlled rollback path to a previously published
immutable application image.
```

Issue #10 acceptance criteria require:

```text
concurrent deployments to the same environment are prevented
production-like deployment requires approval
a previous image version can be selected
the rollback procedure is tested
rollback duration is recorded
```

---

## Baseline entering Issue #10

The retained development environment already contained:

```text
ECS cluster: zero-to-prod-dev
ECS service: demo-api
ECR repository: zero-to-prod-demo-api
GitHub OIDC role
temporary verification ALB Terraform
external /health verification
external /version verification
immutable full-SHA image tags
```

The normal idle baseline remained:

```text
ECS desired count: 0
temporary ALB: absent
```

Before Issue #10, the last known-good application revision was:

```text
934b7de64bc2439ba7252362d110abc02230b4ea
```

During Issue #10, a newer successfully deployed and externally verified revision became the rollback baseline:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

That revision became the last known-good target for the controlled rollback experiment.

---

## Deployment safety model

Issue #10 introduced two separate paths:

```text
normal deployment
```

and:

```text
manual workflow-assisted rollback
```

They intentionally remain separate.

The normal deployment path answers:

```text
What should be deployed from the current main commit?
```

The rollback path answers:

```text
Which already-published known-good immutable image
should replace the failed candidate?
```

Keeping these flows separate avoids mixing normal CI artifact creation with incident recovery.

---

## Deployment serialization

The normal development deployment job now contains:

```yaml
concurrency:
  group: demo-api-development-deployment
  cancel-in-progress: false
```

The rollback workflow uses the same concurrency group:

```yaml
concurrency:
  group: demo-api-development-deployment
  cancel-in-progress: false
```

Therefore normal deployments and rollback operations targeting development share one mutation lane.

The intended behavior is:

```text
deployment A running
        ↓
deployment B cannot mutate the same environment simultaneously
```

and:

```text
normal deployment running
        ↓
rollback cannot mutate development simultaneously
```

---

## Why active deployments are not cancelled

The concurrency configuration uses:

```text
cancel-in-progress: false
```

This is deliberate.

A deployment may already have created:

```text
temporary ALB
listener
Fargate verification task
```

Cancelling the active run to start a newer deployment could interrupt cleanup.

The current Terraform state also remains runner-local.

Therefore the safer behavior for this experiment is:

```text
active deployment finishes its lifecycle
        ↓
cleanup executes
        ↓
next queued mutation may proceed
```

rather than:

```text
cancel active deployment immediately
        ↓
risk incomplete cleanup
```

---

## GitHub concurrency limitation

GitHub Actions concurrency guarantees that workflows using the same concurrency key are not running concurrently.

However, GitHub's concurrency model can retain only a limited pending queue for a group.

A newer pending run may replace an older pending run.

Therefore Issue #10 proves:

```text
no simultaneous shared-environment mutation
```

but does not claim that GitHub provides a durable FIFO deployment queue.

The concurrency configuration was statically validated.

A deliberate live overlapping-deployment stress test was not performed because it would create additional temporary billable deployments without being necessary for the rollback experiment.

This remains an explicit evidence limitation.

---

## GitHub environment

The deployment and rollback jobs use:

```yaml
environment: development
```

A GitHub environment named:

```text
development
```

was created.

Its deployment branch policy permits:

```text
main
```

This adds an explicit environment boundary around development mutations.

---

## Approval model

The rollback workflow is manually triggered using:

```text
workflow_dispatch
```

It requires:

```text
target_sha
confirmation
```

The confirmation must exactly equal:

```text
ROLLBACK
```

and the job also requires:

```text
github.ref == refs/heads/main
```

The effective operator gate is therefore:

```text
operator selects rollback workflow
        ↓
operator selects target SHA
        ↓
operator types ROLLBACK
        ↓
workflow must run from main
        ↓
development environment is used
```

---

## Approval limitation

The current private repository / GitHub plan does not provide the stronger independent-reviewer protection used by larger production environments in this setup.

Therefore the implemented approval model is:

```text
explicit operator approval / confirmation
```

not:

```text
independent second-person reviewer approval
```

This distinction is intentional and documented.

Issue #10 must not overclaim that a two-person production approval process exists.

For a real production environment, a stronger model would normally include:

```text
protected environment
required reviewers
separation of deployer and approver
deployment audit trail
```

---

## Rollback strategy

The chosen rollback model is:

```text
manual
+
workflow-assisted
```

It is not automatic rollback.

Automatic rollback was deliberately avoided because this experiment first needed to prove:

```text
target selection
artifact existence
deployment restoration
external verification
timing
cleanup
```

before introducing automated recovery policy.

---

## Rollback never rebuilds an old image

The rollback workflow requires the operator to select a full 40-character Git SHA.

The workflow does not:

```text
checkout old source
rebuild old source
publish a new image
```

Instead it selects an already-published immutable image:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:<target_sha>
```

The recovery model is:

```text
known-good immutable artifact
        ↓
redeploy exact existing artifact
```

This preserves the artifact that was previously tested rather than attempting to recreate it later.

---

## Rollback SHA validation

The rollback input must match:

```text
^[0-9a-fA-F]{40}$
```

A malformed value such as:

```text
main
```

was tested locally.

Result:

```text
rejected
exit code 1
```

A valid known-good SHA was accepted.

---

## Rollback image existence validation

A syntactically valid SHA is not sufficient.

The workflow calls ECR:

```text
ecr:BatchGetImage
```

and inspects both:

```text
image
failures
```

A deliberately nonexistent SHA was tested:

```text
0000000000000000000000000000000000000000
```

ECR returned:

```text
ImageNotFound
```

The workflow guard emitted an error and returned non-zero.

The known-good rollback SHA:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

was confirmed to exist before the live rollback.

---

## Rollback task-definition strategy

The rollback does not blindly reuse an old ECS task-definition revision.

Instead it:

```text
loads the current task-definition template
        ↓
replaces only the image placeholder
        ↓
uses the selected immutable old image
        ↓
registers a new task-definition revision
```

This means rollback restores:

```text
application image version
```

while retaining the current task-definition configuration for:

```text
CPU
memory
execution role
container settings
health configuration
```

This choice avoids accidentally rolling back unrelated configuration together with the application image.

---

## Task-definition rendering evidence

A local preflight rendered the known-good image into:

```text
infra/aws/ecs/demo-api-task-definition.json
```

Observed values included:

```text
family: zero-to-prod-demo-api
cpu: 256
memory: 512
execution role:
arn:aws:iam::333534066371:role/zero-to-prod-ecs-task-execution
```

Rendered image:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:934b7de64bc2439ba7252362d110abc02230b4ea
```

Replacing the rendered image with the placeholder again and diffing against the template produced no difference.

This proved the rendering operation changed only the image field.

---

## Rollback workflow

Rollback implementation lives in:

```text
.github/workflows/demo-api-rollback.yml
```

The workflow performs:

```text
validate rollback SHA
        ↓
checkout current repository
        ↓
display requested rollback
        ↓
authenticate to AWS using OIDC
        ↓
verify AWS identity
        ↓
verify selected ECR image exists
        ↓
render rollback task definition
        ↓
Terraform init
        ↓
Terraform plan
        ↓
create temporary verification ALB
        ↓
record rollback start
        ↓
register rollback task definition
        ↓
update ECS service + desired count 1
        ↓
wait for ECS stability
        ↓
verify service task definition
        ↓
external /health
        ↓
external /version
        ↓
record rollback duration
        ↓
scale ECS back to 0
        ↓
destroy temporary verification infrastructure
```

---

## Third-party GitHub Actions pinning

Third-party actions remain pinned to full commit SHAs.

Rollback uses pinned revisions for:

```text
actions/checkout
aws-actions/configure-aws-credentials
hashicorp/setup-terraform
```

Terraform remains pinned to:

```text
1.15.9
```

This preserves the supply-chain discipline established by previous issues.

---

## Authentication and authorization

Authentication remains GitHub OIDC.

The GitHub Actions role is:

```text
arn:aws:iam::333534066371:role/zero-to-prod-github-actions
```

No long-lived AWS credentials are stored in GitHub.

Authentication answers:

```text
Who is this workload?
```

Authorization answers:

```text
What may this workload do?
```

Issue #10 exposed an important difference between those two concerns.

---

## Existing rollback authorization

Before adding rollback, the live role policies were inspected.

Existing ECS permissions already included:

```text
ecs:RegisterTaskDefinition
ecs:UpdateService
ecs:DescribeServices
iam:PassRole
```

`ecs:UpdateService` and `ecs:DescribeServices` remained scoped to:

```text
arn:aws:ecs:eu-west-3:333534066371:service/zero-to-prod-dev/demo-api
```

`iam:PassRole` remained restricted to the ECS task execution role and ECS tasks service.

Existing ECR permissions already included:

```text
ecr:BatchGetImage
```

for the intended repository.

The verification Terraform permissions from Issue #9 already covered temporary ALB lifecycle operations.

Therefore rollback required:

```text
no new AWS service authorization permissions
```

---

## OIDC environment subject failure

The first implementation added:

```yaml
environment: development
```

to the deployment job.

The first post-merge deployment then failed during:

```text
Configure AWS credentials
```

GitHub Actions repeatedly reported:

```text
Not authorized to perform sts:AssumeRoleWithWebIdentity
```

Failed run:

```text
32503496084
```

No Terraform or ECS mutation occurred because authentication failed before deployment infrastructure creation.

---

## Why authentication failed

Before the environment was attached, the OIDC trust policy expected:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
```

When a GitHub Actions job references an environment, the OIDC subject changes to an environment-bound form:

```text
repo:ZakariaAitAli/zero-to-prod:environment:development
```

The environment therefore changed the workload identity.

The failure was:

```text
authentication trust mismatch
```

not:

```text
missing ECS permission
missing ECR permission
missing ELB permission
```

This distinction prevented unnecessary authorization broadening.

---

## OIDC trust correction

The source-controlled trust policy is:

```text
infra/aws/iam/github-actions-trust-policy.json
```

The trust condition was changed from one allowed subject to two exact subjects:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
```

and:

```text
repo:ZakariaAitAli/zero-to-prod:environment:development
```

The branch subject was retained because the ECR publication job still authenticates without an environment.

The environment subject was added for deployment and rollback jobs.

No wildcard subject was introduced.

---

## Live trust-policy verification

The live IAM role was updated using the local sandbox identity.

The stored live trust policy was then fetched independently.

Observed subjects:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
repo:ZakariaAitAli/zero-to-prod:environment:development
```

This proved the source-controlled trust policy and live authentication boundary matched.

The IAM change itself does not create recurring AWS cost.

---

## Successful deployment after OIDC correction

OIDC trust fix PR:

```text
#26
```

Merge commit:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

Successful GitHub Actions run:

```text
32504890769
```

Jobs:

```text
Test and build demo API        success
Publish immutable image        success
Deploy to development          success
```

The deployment completed:

```text
OIDC authentication
Terraform temporary ALB creation
task-definition registration
ECS deployment
external HTTP verification
ECS cleanup
Terraform cleanup
```

Task definition after this successful run:

```text
zero-to-prod-demo-api:5
```

This revision became the last known-good rollback target.

---

## Independent baseline verification

After the successful corrected deployment, AWS was checked independently.

ECS:

```json
{
  "service": "demo-api",
  "desired": 0,
  "running": 0,
  "pending": 0,
  "taskDefinition": "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:5"
}
```

ALB lookup returned:

```text
LoadBalancerNotFound
```

This re-established the clean cost-conscious baseline before the rollback experiment.

---

## Rollback timing definition

The rollback measurement boundaries were defined before the live experiment.

Start:

```text
immediately before ecs:RegisterTaskDefinition
```

This is the first application rollback mutation.

End:

```text
immediately after successful external /health
and exact /version verification
```

The measurement intentionally excludes:

```text
temporary ALB creation
```

because that is verification-harness preparation.

It also excludes:

```text
post-verification ECS scale-down
Terraform destroy
```

because restoration has already been proven at the data plane when external verification succeeds.

The metric therefore measures:

```text
first rollback application mutation
        ↓
ECS recovery
        ↓
externally verified known-good application
```

---

## Controlled failure design

The rollback experiment required a failed candidate.

The safest deliberate failure was chosen:

```text
keep the application itself healthy
but deliberately expect the wrong version
```

The application continued to embed the real candidate SHA at build time.

Only the workflow verification expectation was temporarily changed from:

```yaml
EXPECTED_VERSION: ${{ github.sha }}
```

to:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

The candidate therefore remained:

```text
healthy
reachable
correctly built
```

while deployment verification deliberately rejected it as the wrong version.

This avoided introducing an unrelated application or infrastructure outage just to test rollback.

---

## Controlled failure PR

Experiment PR:

```text
#27
```

Experiment commit:

```text
8ceb5ea313d7afb3f5a0cb3c65a61872b9a14e7d
```

Merge commit:

```text
e7eec825bd16634a19e31a82a7140083e3360bd9
```

The PR changed:

```text
1 file
1 insertion
1 deletion
```

PR CI result:

```text
Test and build demo API        success
Publish immutable image        skipped
Deploy to development          skipped
```

CodeRabbit was rate-limited on this PR.

Therefore the successful CodeRabbit status must not be described as a substantive completed review for this experiment.

---

## Failed candidate deployment evidence

Main workflow run:

```text
32506584207
```

Results:

```text
Test and build demo API        success
Publish immutable image        success
Deploy to development          failure
```

Deployment successfully reached:

```text
temporary ALB created
task definition registered
ECS service updated
ECS stable
HTTP verification started
```

Verification then reported:

```text
Expected version:
1b6a631f0db11289b641fdd3b364282c87cf457b
```

Observed version:

```text
e7eec825bd16634a19e31a82a7140083e3360bd9
```

Result:

```text
Process completed with exit code 1.
```

This reproduced the stale/wrong-version failure mode established in Issue #9 inside the real deployment workflow.

---

## Failed-candidate cleanup behavior

Although verification failed, the workflow still executed:

```text
Stop verification task
Destroy verification infrastructure
```

Both cleanup steps reported success.

The cleanup conditions are:

```yaml
if: always()
```

combined with step-outcome guards.

This proves normal verification failure does not skip cleanup.

---

## Independent failed-candidate cleanup verification

Cleanup was checked independently through AWS rather than trusting only GitHub Actions.

ECS after the failed candidate:

```json
{
  "service": "demo-api",
  "desired": 0,
  "running": 0,
  "pending": 0,
  "taskDefinition": "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:6"
}
```

Temporary ALB lookup returned:

```text
LoadBalancerNotFound
```

The failed candidate therefore left no active verification runtime.

---

## Rollback dispatch

After failed-candidate cleanup was independently verified, rollback was manually dispatched.

Target SHA:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

Confirmation:

```text
ROLLBACK
```

Workflow:

```text
Demo API Rollback
```

Run:

```text
32507641720
```

---

## Successful rollback evidence

Rollback run:

```text
32507641720
```

Job:

```text
Roll back development
```

Conclusion:

```text
success
```

Successful steps included:

```text
Validate rollback SHA
Verify rollback image exists
Render rollback task definition
Create verification infrastructure
Record rollback start
Register rollback task definition
Deploy rollback task definition
Wait for rollback service stability
Verify rollback task definition
Verify rollback over HTTP
Record rollback duration
Stop rollback verification task
Destroy verification infrastructure
```

This is the primary live rollback experiment for Issue #10.

---

## Rollback duration

Recorded start:

```text
2026-08-21T17:24:05Z
```

Recorded completion:

```text
2026-08-21T17:26:15Z
```

Measured rollback duration:

```text
130 seconds
```

Equivalent:

```text
2 minutes 10 seconds
```

The complete GitHub Actions workflow took longer because it also included:

```text
authentication
image validation
Terraform initialization
ALB creation
cleanup
```

Those operations are intentionally outside the defined rollback recovery metric.

---

## Rollback external verification

The rollback verifier required:

```text
/health
```

to be healthy and:

```text
/version
```

to exactly equal:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

The rollback workflow succeeded only after those external checks passed.

Therefore the rollback proof is not limited to ECS control-plane stability.

It proves:

```text
selected immutable image
        ↓
running ECS task
        ↓
externally reachable application
        ↓
exact known-good version observed
```

---

## Independent rollback cleanup verification

After rollback success, AWS was checked independently.

ECS:

```json
{
  "service": "demo-api",
  "desired": 0,
  "running": 0,
  "pending": 0,
  "taskDefinition": "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:7"
}
```

Temporary ALB lookup returned:

```text
LoadBalancerNotFound
```

Task definition `:7` was then inspected directly.

Observed image:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:1b6a631f0db11289b641fdd3b364282c87cf457b
```

This independently proves the ECS rollback revision references the exact selected immutable known-good image.

---

## Restoring normal verification

The controlled wrong-version expectation was temporary.

Leaving it on `main` would cause every future deployment to fail.

Cleanup PR:

```text
#28
```

Commit:

```text
1af2e3a598f38bfa339a8647ffbcbff9f4c5296b
```

Merge commit:

```text
e36b6b080c0a6b5f774185d66c102096c2acd850
```

The normal verifier was restored to:

```yaml
EXPECTED_VERSION: ${{ github.sha }}
```

PR CI passed.

CodeRabbit reported:

```text
Review completed
```

with no remaining review threads.

---

## Final successful deployment

The verifier-restoration merge triggered:

```text
GitHub Actions run 32510301964
```

Result:

```text
Test and build demo API        success
Publish immutable image        success
Deploy to development          success
```

Successful deployment steps included:

```text
OIDC authentication
Terraform temporary ALB creation
task-definition registration
ECS deployment
service stability
external HTTP verification
deployment reporting
ECS scale-down
Terraform destroy
```

This proved that the temporary controlled-failure experiment did not leave the normal deployment pipeline broken.

---

## Final AWS baseline

After the final successful deployment, ECS was independently inspected:

```json
{
  "service": "demo-api",
  "desired": 0,
  "running": 0,
  "pending": 0,
  "taskDefinition": "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:8"
}
```

Temporary ALB lookup returned:

```text
LoadBalancerNotFound
```

Final retained runtime baseline:

```text
ECS desired: 0
ECS running: 0
ECS pending: 0
temporary ALB: absent
```

---

## Cost-conscious lifecycle

The live deployment and rollback experiments temporarily created the billable runtime components already established in Issue #9:

```text
one Fargate task
one Application Load Balancer
```

These existed only during verification.

The failed candidate cleanup was independently verified before rollback began.

The rollback cleanup was independently verified.

The final verifier-restoration deployment cleanup was independently verified.

No verification ALB or active Fargate task remains after the experiment.

---

## Cleanup limitation

The known Issue #9 Terraform-state limitation remains.

Terraform state for the temporary ALB lifecycle is stored on the GitHub Actions runner.

Normal failures are handled using:

```text
if: always()
```

However:

```text
hard runner termination
```

after `terraform apply` but before `terraform destroy` could still leave an orphaned ALB.

`cancel-in-progress: false` reduces deliberate cancellation risk but cannot eliminate hard runner failure.

Issue #10 does not claim this limitation is solved.

---

## Pull request behavior

Normal pull requests continue to perform:

```text
test
build
```

without:

```text
ECR publication
AWS deployment
```

Observed experiment PR behavior:

```text
Test and build demo API        success
Publish immutable image        skipped
Deploy to development          skipped
```

The same pattern was observed on the verifier-restoration PR.

This preserves the existing boundary:

```text
pull request
    → test/build only

push to main
    → publish/deploy
```

---

## Observation — CI is not yet change-aware

During Issue #10, workflow-only and IAM-only changes still caused the Demo API test/build job to run.

A merge to `main` can also create a new application artifact even when the application source itself did not change.

This behavior is valid but inefficient.

A future optimization could distinguish changes such as:

```text
apps/demo-api/**
workflow files
Terraform files
IAM files
documentation
```

and run only the necessary validation/build/deployment stages.

This optimization was deliberately kept outside Issue #10 to avoid changing CI triggering semantics during the rollback experiment.

---

## Failure experiments

Issue #10 exercised several deliberate failure paths.

### Invalid rollback SHA

Input:

```text
main
```

Result:

```text
rejected by full-SHA validation
exit 1
```

---

### Nonexistent immutable rollback image

Input:

```text
0000000000000000000000000000000000000000
```

ECR result:

```text
ImageNotFound
```

Workflow-equivalent guard:

```text
failure
exit 1
```

---

### Environment OIDC trust mismatch

Adding:

```yaml
environment: development
```

changed the GitHub OIDC subject.

Run:

```text
32503496084
```

failed before AWS deployment mutation with:

```text
Not authorized to perform sts:AssumeRoleWithWebIdentity
```

This proved authentication must be re-evaluated when the GitHub workload identity changes.

---

### Controlled candidate verification failure

Run:

```text
32506584207
```

Application remained healthy.

Expected version:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

Observed candidate:

```text
e7eec825bd16634a19e31a82a7140083e3360bd9
```

Result:

```text
exit 1
deployment job failure
cleanup still executed
```

This was the live trigger for the rollback procedure.

---

## What I learned

### 1. Rollback should select artifacts, not recreate them

A known-good artifact has already passed previous validation.

Rebuilding old source later can produce a different artifact because of:

```text
base image changes
dependency changes
toolchain changes
registry changes
```

Immutable artifact selection is therefore a stronger rollback primitive.

---

### 2. Rollback image version and infrastructure configuration are separate decisions

Reusing an old ECS task-definition revision would also restore old task configuration.

Issue #10 intentionally restores only:

```text
application image
```

while using the current task-definition template.

This makes the rollback boundary explicit.

---

### 3. Authentication can change without authorization changing

Adding a GitHub environment changed:

```text
OIDC subject
```

even though the requested AWS service operations did not change.

The correct fix was:

```text
update trust relationship
```

not:

```text
grant broader ECS permissions
```

---

### 4. Rollback duration needs a definition before measurement

A number such as:

```text
6 minutes
```

is meaningless unless the boundaries are defined.

Issue #10 defined:

```text
start:
first application rollback mutation

end:
externally verified restored application
```

before executing the live experiment.

The measured recovery time was:

```text
130 seconds
```

---

### 5. Cleanup must be verified independently

GitHub Actions reporting:

```text
Destroy verification infrastructure ✓
```

is useful but not sufficient evidence.

The stronger model is:

```text
workflow cleanup reports success
        +
AWS independently reports desired=0
        +
AWS reports ALB absent
```

---

### 6. Concurrency is part of deployment correctness

Two workflows mutating the same ECS service and temporary ingress concurrently could produce ambiguous state.

Shared environment mutation therefore needs serialization, not only successful individual workflow logic.

---

### 7. Approval has levels

Manual confirmation is better than an accidental one-click rollback.

It is not equivalent to independent human approval.

The implementation should state which guarantee it actually provides.

---

## Mistakes and corrections

### Environment OIDC subject was initially overlooked

The first environment-bound deployment failed because the trust policy still expected only the branch-based subject.

Correction:

```text
preserve branch subject for publication
+
add exact development environment subject
```

No wildcard trust was introduced.

---

### Initially assumed GitHub Free/private environment support was unavailable

The repository proved that a `development` environment could be created and used.

The assumption was corrected based on the actual repository behavior.

Stronger reviewer protection remains limited in the current setup.

---

### Arithmetic syntax bug caught before live rollback

The first duration implementation accidentally used command substitution around arithmetic.

Incorrect form:

```bash
rollback_duration_seconds="$(
  (rollback_completed_epoch - ROLLBACK_STARTED_EPOCH)
)"
```

Corrected before execution to:

```bash
rollback_duration_seconds="$((rollback_completed_epoch - ROLLBACK_STARTED_EPOCH))"
```

The live rollback therefore recorded the intended duration correctly.

---

### Interactive negative test exited WSL

A negative Bash test containing:

```text
exit 1
```

was initially run directly in the interactive shell.

That exited the WSL shell and returned to PowerShell.

Correction:

```text
negative workflow-behavior tests run in a subshell
```

so intentional non-zero exits do not terminate the interactive environment.

---

### GitHub CLI PR edit hit deprecated GraphQL behavior

A PR metadata update using:

```text
gh pr edit
```

failed because the command queried deprecated Projects classic data.

The PR body was corrected using the REST path instead.

This did not mutate AWS or application code.

---

### CodeRabbit rate limiting must not be overclaimed

Some Issue #10 PRs reported successful CodeRabbit checks while the description said:

```text
Review rate limited
```

Those were recorded as rate-limited rather than described as substantive reviews.

The final verifier-restoration PR later reported:

```text
Review completed
```

with no review threads.

---

## Knowledge gaps identified

Useful follow-up topics include:

```text
remote Terraform state
orphan ALB recovery
GitHub concurrency pending-run semantics
required-reviewer environment protection
two-person production approval
automatic rollback policy
rollback configuration compatibility
deployment history storage
release metadata
change-aware CI/CD
progressive delivery
blue/green deployment
canary deployment
deployment health budgets
```

These remain outside Issue #10.

---

## Acceptance criteria

Issue #10 required:

```text
Concurrent deployments to the same environment are prevented.
```

Implemented using the shared concurrency group:

```text
demo-api-development-deployment
```

for both:

```text
normal development deployment
rollback
```

with:

```text
cancel-in-progress: false
```

This prevents simultaneous mutation by those workflow jobs.

Runtime overlapping-deployment stress testing was not performed, so the evidence here is configuration plus GitHub Actions concurrency semantics rather than a deliberately billable collision experiment.

---

Issue #10 required:

```text
Production-like deployment requires approval.
```

Implemented in the available setup as:

```text
manual workflow_dispatch
+
explicit target SHA
+
confirmation must equal ROLLBACK
+
main-only condition
+
development environment
```

This provides explicit operator approval.

It does not provide an independent second-person reviewer gate.

That limitation is documented rather than hidden.

---

Issue #10 required:

```text
Previous image version can be selected.
```

Implemented using:

```text
target_sha
```

The target must:

```text
be a full 40-character SHA
exist in the immutable ECR repository
```

The live rollback selected:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

---

Issue #10 required:

```text
Rollback procedure is tested.
```

Live rollback run:

```text
32507641720
```

Result:

```text
success
```

The workflow restored the selected immutable image and passed external health/version verification.

---

Issue #10 required:

```text
Rollback duration is recorded.
```

Measured:

```text
130 seconds
```

Boundary:

```text
immediately before RegisterTaskDefinition
        ↓
successful external /health + exact /version
```

All acceptance criteria are implemented with the approval and concurrency evidence limitations documented above.

---

## Focused work performed

Issue #10 included:

```text
existing deployment workflow review
issue acceptance-criteria review
deployment concurrency design
GitHub environment creation
environment branch restriction
approval-model analysis
rollback strategy selection
manual rollback workflow implementation
immutable image selection
SHA validation
ECR image-existence validation
rollback task-definition rendering
current-template rollback design
rollback timing-boundary definition
cleanup design
rollback Terraform integration
OIDC environment identity investigation
authentication vs authorization diagnosis
OIDC trust-policy correction
live trust-policy verification
least-privilege review
actionlint validation
workflow diff validation
negative SHA test
missing-image test
task-definition render test
controlled candidate failure design
failed candidate deployment experiment
failed candidate cleanup verification
manual rollback experiment
external rollback verification
rollback duration measurement
rollback cleanup verification
rollback image verification
normal verifier restoration
final successful deployment
final ECS cleanup verification
final ALB cleanup verification
documentation
```

The work remained evidence-first:

```text
inspect
    ↓
design
    ↓
validate locally
    ↓
deploy
    ↓
observe failure
    ↓
verify cleanup
    ↓
rollback
    ↓
verify externally
    ↓
verify cleanup independently
```

---

## Session record

Focused time:

```text
~3h 30m
```

Primary controlled-failure run:

```text
32506584207
```

Primary rollback run:

```text
32507641720
```

Measured rollback duration:

```text
130 seconds
```

Rollback target:

```text
1b6a631f0db11289b641fdd3b364282c87cf457b
```

Rollback ECS task definition:

```text
zero-to-prod-demo-api:7
```

Final normal deployment run:

```text
32510301964
```

Final ECS task definition:

```text
zero-to-prod-demo-api:8
```

Final baseline:

```text
desired=0
running=0
pending=0
temporary ALB absent
```

---

## Result

Sprint 01 now has a verified delivery and recovery path:

```text
source commit
    ↓
CI tests
    ↓
Docker build
    ↓
immutable ECR image
    ↓
serialized development mutation
    ↓
ECS deployment
    ↓
external /health
    ↓
external /version
    ↓
candidate accepted
```

and for a failed candidate:

```text
candidate deploys
    ↓
verification fails
    ↓
cleanup
    ↓
operator selects known-good immutable SHA
    ↓
rollback registers new task definition
    ↓
ECS restores known-good image
    ↓
external /health succeeds
    ↓
external /version matches rollback target
    ↓
recovery recorded at 130 seconds
    ↓
cleanup
```

Issue #10 closes the gap between:

```text
"deployment failure is detected"
```

and:

```text
"there is a tested, measured procedure
for restoring a known-good immutable application version"
```

---

## Next experiment

The next planned repository issue is:

```text
Issue #11 — Document and demonstrate the system
```

Its target is to turn the completed Sprint 01 implementation into an understandable operational system.

The next work should cover:

```text
README deployment flow
architecture diagram
security decisions
common-failure runbook
short demonstration
final sprint reflection
```

The desired progression is:

```text
working system
    ↓
documented architecture
    ↓
operational runbook
    ↓
demonstrable delivery flow
    ↓
Sprint 01 reflection
```

Change-aware CI/CD remains a useful follow-up optimization, but it should not replace the already-planned Issue #11.
