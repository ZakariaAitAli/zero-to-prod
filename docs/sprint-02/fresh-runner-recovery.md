# Fresh-runner recovery of orphaned verification infrastructure

## Objective

Issue #37 proves that temporary verification infrastructure can be recovered and removed after the GitHub Actions runner that created it disappears before cleanup.

Issue #35 established durable remote Terraform state.

Issue #36 added S3-native state locking.

Issue #37 tests the next failure boundary:

```
Terraform apply succeeds
    ↓
temporary verification infrastructure exists
    ↓
original runner disappears before destroy
    ↓
local workspace is lost
    ↓
a separate fresh runner starts
    ↓
remote state identifies the orphan
    ↓
Terraform destroys the orphan safely
```

The experiment was deliberately supervised because the temporary Application Load Balancer incurs cost while it exists.

No production environment was involved.

## Environment

AWS account:

```
333534066371
```

Region:

```
eu-west-3
```

Terraform root:

```
infra/terraform/development-verification
```

Remote state bucket:

```
zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate
```

Remote state key:

```
development-verification/terraform.tfstate
```

S3 lock object:

```
development-verification/terraform.tfstate.tflock
```

Terraform version:

```
1.15.9
```

AWS provider:

```
6.59.0
```

State lineage:

```
71e55630-6ebf-c0b8-9bb1-50500e8816c4
```

Verification ALB:

```
zero-to-prod-dev-alb
```

ECS cluster:

```
zero-to-prod-dev
```

ECS service:

```
demo-api
```

## Clean baseline

Before the interruption experiment, the authoritative state was:

```
serial         = 9
resource_count = 0
```

The verification ALB did not exist.

No current `.tflock` object existed.

ECS was already at the intended zero-runtime baseline:

```
desired = 0
running = 0
pending = 0
```

No running service tasks were present.

This established a clean starting point before intentionally creating an orphan.

## Safety controls

The experiment reused the existing verification Terraform root rather than creating an independent unmanaged test resource.

A temporary experiment branch was used:

```
feat/issue-37-fresh-runner-recovery
```

The normal rollback workflow was temporarily instrumented so that, on this branch only, it paused immediately after successful Terraform creation of the verification infrastructure.

The pause happened before any ECS deployment steps.

The existing GitHub Actions deployment concurrency group remained:

```
demo-api-development-deployment
```

The existing `development` environment remained in use.

The AWS OIDC role remained:

```
zero-to-prod-github-actions
```

IAM was not broadened for the experiment.

The remote backend and emergency Terraform destroy path were confirmed before interruption.

The experiment was supervised continuously so orphan lifetime would remain bounded.

## Preliminary failure — environment protection blocked the branch

The first experiment dispatch was:

```
run ID = 34641876195
commit = 8a8fb4e0c67dfbc8638de7162072b4cdd6bd106f
```

The workflow failed before receiving a runner.

GitHub reported that:

```
feat/issue-37-fresh-runner-recovery
```

was not allowed to deploy to the protected:

```
development
```

environment.

There were no workflow steps, no runner assignment, no OIDC session, and no AWS or Terraform changes.

This was useful evidence that the environment protection rule was effective.

Instead of weakening the environment or broadening IAM, one exact temporary deployment branch policy was added for:

```
feat/issue-37-fresh-runner-recovery
```

After the experiment, that temporary rule was deleted.

The final environment branch policy again permits only:

```
main
```

## Failure experiment 1 — interrupt cleanup after Terraform apply

### Experiment

The controlled interruption run was:

```
run ID = 34642213628
commit = 8a8fb4e0c67dfbc8638de7162072b4cdd6bd106f
```

The workflow successfully completed:

```
Terraform setup
backend initialization
Terraform plan
Terraform apply
```

It then entered the dedicated interruption hold step.

No ECS deployment step had started.

### Infrastructure evidence before interruption

AWS independently reported an active ALB:

```
arn:aws:elasticloadbalancing:eu-west-3:333534066371:loadbalancer/app/zero-to-prod-dev-alb/543cea38b5356425
```

Created:

```
2026-09-11T20:04:47.616Z
```

State:

```
active
```

The listener was:

```
arn:aws:elasticloadbalancing:eu-west-3:333534066371:listener/app/zero-to-prod-dev-alb/543cea38b5356425/1321a6c5c48d6472
```

Listener configuration:

```
port     = 80
protocol = HTTP
```

The remote Terraform state had advanced to:

```
serial = 10
```

It contained six state entries:

```
data.aws_lb_target_group.demo_api
data.aws_security_group.alb
data.aws_subnet.alb_a
data.aws_subnet.alb_b
aws_lb.verification
aws_lb_listener.http
```

The two Terraform-managed resources were therefore:

```
aws_lb.verification
aws_lb_listener.http
```

The state lineage remained unchanged.

No active Terraform lock remained after apply.

ECS was still:

```
desired = 0
running = 0
pending = 0
```

and no running tasks were present.

### Hard interruption

The workflow was force-cancelled at:

```
2026-09-11T20:07:25Z
```

GitHub concluded the run as:

```
cancelled
```

The important step results were:

```
Create verification infrastructure  -> success
Hold interruption experiment        -> cancelled
Record rollback start               -> skipped
Register rollback task definition   -> skipped
Deploy rollback task definition     -> skipped
Stop rollback verification task     -> skipped
Destroy verification infrastructure -> skipped
```

This deliberately reproduced the failure mode where normal cleanup never executes.

### Post-cancel orphan evidence

At:

```
2026-09-11T20:08:28Z
```

the original workflow was already gone, but AWS still reported the ALB as:

```
active
```

Remote state still reported:

```
serial = 10
```

with:

```
aws_lb.verification
aws_lb_listener.http
```

still managed.

The temporary infrastructure had therefore survived the runner that created it.

### Result

Pass.

The interruption successfully created a Terraform-managed orphan without starting ECS compute.

## What survived the original runner

The original GitHub-hosted runner workspace did not survive.

The following information did survive externally:

```
S3 remote Terraform state
state lineage
state serial
Terraform resource addresses
Terraform resource attributes
ALB ARN
listener ARN
live AWS resources
```

This is the critical recovery boundary.

Recovery did not depend on:

```
the original .terraform directory
a local terraform.tfstate
the original saved plan
shell variables from the first run
files in the first runner workspace
```

The durable recovery authority was the remote Terraform state.

AWS discovery was used independently to confirm that the resources described by state also existed in AWS.

## Failure experiment 2 — recover from a fresh runner

### Fresh runner

A separate recovery workflow run was started:

```
run ID = 34642955772
commit = 09abede5ee487bad086d8e0ab165a8075339af8e
```

The runner identified itself as a new GitHub-hosted runner.

Before checkout, the workspace was empty.

After checkout and before Terraform initialization, the workflow explicitly confirmed the absence of:

```
.terraform
terraform.tfstate
terraform.tfstate.backup
verification.tfplan
recovery-destroy.tfplan
```

The recovery therefore had no filesystem state from the interrupted run.

### Remote state initialization

The runner configured AWS credentials through the existing OIDC role and successfully ran Terraform initialization against the S3 backend.

It then executed:

```
terraform state list
terraform state pull
```

The fresh runner recovered:

```
serial  = 10
lineage = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
```

The recovered state contained:

```
data.aws_lb_target_group.demo_api
data.aws_security_group.alb
data.aws_subnet.alb_a
data.aws_subnet.alb_b
aws_lb.verification
aws_lb_listener.http
```

The fresh runner recovered the exact ALB and listener ARNs from Terraform state.

### Recovery destroy plan

The fresh runner generated a saved destroy plan.

Terraform reported:

```
Plan: 0 to add, 0 to change, 2 to destroy.
```

The plan JSON was checked before apply.

The only destructive changes were exactly:

```
aws_lb.verification    -> delete
aws_lb_listener.http   -> delete
```

The workflow would fail instead of applying if any additional managed resource appeared in the destructive plan.

### Recovery destroy

Recovery destruction started at:

```
2026-09-11T20:13:09Z
```

Terraform destroyed:

```
aws_lb_listener.http
aws_lb.verification
```

Terraform reported:

```
Apply complete! Resources: 0 added, 0 changed, 2 destroyed.
```

Recovery destruction completed at:

```
2026-09-11T20:13:16Z
```

### Independent AWS verification

After Terraform completed, AWS API checks against the exact recovered ARNs returned:

```
LoadBalancerNotFound
ListenerNotFound
```

This independently confirmed that both orphaned resources were absent.

### Final state

The remote Terraform state advanced to:

```
serial = 11
```

with:

```
resource_count         = 0
managed_resource_count = 0
```

The state lineage remained:

```
71e55630-6ebf-c0b8-9bb1-50500e8816c4
```

No current state lock remained.

### ECS baseline

The recovery workflow confirmed:

```
desired = 0
running = 0
pending = 0
```

for:

```
zero-to-prod-dev / demo-api
```

The workflow's final `ecs:ListTasks` assertion failed because the GitHub Actions role does not have:

```
ecs:ListTasks
```

That permission was not added just to make the evidence check green.

The recovery itself had already succeeded.

An independent administrator-profile check afterward confirmed:

```
taskArns = []
```

The final baseline was therefore:

```
desired = 0
running = 0
pending = 0
running tasks = 0
```

### Result

Pass.

A new runner with no previous workspace recovered the Terraform-owned orphan solely from durable remote state and removed it successfully.

## Failure experiment 3 — stale or held state lock

### Objective

Prove that recovery does not bypass the state-locking protection added in Issue #36.

The test needed to be safe and must not recreate infrastructure.

### Experiment

The backend was first confirmed to have no current lock.

A controlled synthetic S3 lock object was then created at:

```
development-verification/terraform.tfstate.tflock
```

Lock metadata:

```
ID        = fe03571b-f03d-4947-8e41-52151e35fca3
Operation = OperationTypePlan
Who       = issue-37-controlled-test
Version   = 1.15.9
Created   = 2026-09-11T20:22:47Z
Path      = zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate/development-verification/terraform.tfstate
```

A Terraform plan then attempted to acquire the same state lock with:

```
lock timeout = 5 seconds
refresh      = false
```

No apply operation was performed.

### Observation

Terraform waited for the lock and then failed with:

```
Error acquiring the state lock
```

S3 returned:

```
StatusCode: 412
PreconditionFailed
```

Terraform surfaced the exact synthetic lock metadata.

The Terraform process exited:

```
1
```

The synthetic lock object was deleted immediately after the failed plan.

A subsequent S3 `HeadObject` returned:

```
404 Not Found
```

### Result

Pass.

Recovery remains protected by the same state-locking mechanism as normal Terraform operations.

A stale or held lock blocks the operation rather than allowing a competing mutation to continue.

The correct response is to investigate and resolve the lock.

The design does not use:

```
-lock=false
```

to bypass this protection.

## `if: always()` versus external recovery

The existing rollback workflow uses cleanup steps designed to execute even when earlier workflow steps fail.

That is useful for ordinary failures where the GitHub Actions runner is still alive and GitHub can continue scheduling cleanup steps.

This experiment proved a harder boundary.

After force-cancellation:

```
Destroy verification infrastructure -> skipped
```

The presence of an `always()` cleanup condition cannot guarantee cleanup after every form of runner loss or forced workflow termination.

The distinction is:

```
ordinary step failure
    ↓
runner still executing
    ↓
always() cleanup can run
```

versus:

```
hard cancellation / runner disappearance
    ↓
runner no longer executes cleanup
    ↓
durable remote state + external recovery required
```

Remote state is therefore part of the recovery design, not only a collaboration feature.

## State versus AWS discovery

Terraform state answered:

```
which resources Terraform owns
their Terraform addresses
their stored attributes
their ARNs
the state lineage
the state serial
```

AWS API discovery answered:

```
whether the ALB still existed
whether the listener still existed
whether deletion actually completed
whether ECS remained at the intended runtime baseline
```

Neither source replaces the other.

Terraform state provides ownership and intended management context.

AWS APIs independently verify actual cloud state.

The recovery procedure intentionally used both.

## Maximum orphan lifetime

The ALB was created at:

```
2026-09-11T20:04:47.616Z
```

Terraform completed its destruction at:

```
2026-09-11T20:13:16Z
```

Maximum observed orphan/resource lifetime:

```
approximately 8 minutes 28 seconds
```

The workflow was force-cancelled at:

```
2026-09-11T20:07:25Z
```

Time from forced cancellation to completed recovery:

```
approximately 5 minutes 51 seconds
```

The experiment remained supervised for the entire orphan window.

## Cost observation

The only intentionally created runtime infrastructure was the temporary verification ALB and listener.

ECS remained at:

```
desired = 0
```

throughout the interruption and recovery experiment, so the experiment did not add ECS task runtime.

The ALB existed for less than nine minutes.

S3 state and lock activity consisted only of small state/lock objects and API requests.

The exact billed AWS amount was not yet available at experiment time and should be confirmed later from Cost Explorer rather than estimated from runtime alone.

The important bounded-cost evidence is:

```
one temporary verification ALB
less than nine minutes of actual existence
zero ECS runtime
immediate supervised recovery
no persistent orphan after the experiment
```

## Mistakes and knowledge gaps

### Environment protection was stricter than the experiment branch

The first dispatch was rejected because the `development` environment allowed only `main`.

This was the correct protection behavior.

The experiment was fixed by temporarily allowing only the exact experiment branch.

The AWS OIDC trust policy was not broadened.

The temporary GitHub environment branch rule was removed after testing.

### Final evidence check assumed an IAM permission that was not present

The fresh-runner recovery workflow successfully recovered and destroyed the orphan.

Its final evidence step later attempted:

```
ecs:ListTasks
```

The least-privilege GitHub Actions role did not have that permission.

The job therefore ended with a failure conclusion even though:

```
remote-state recovery succeeded
the two-resource destroy succeeded
AWS confirmed ALB absence
AWS confirmed listener absence
remote state returned to zero resources
ECS describe-services showed 0 / 0 / 0
```

The missing permission was not added merely to make the workflow green.

Instead, the final task-list assertion was performed independently with the administrator profile.

This exposed an important distinction between:

```
permissions required to perform recovery
```

and:

```
permissions used only for additional evidence collection
```

Those should not automatically be treated as the same IAM requirement.

## Cleanup

All temporary experiment infrastructure was removed.

Final cloud state:

```
ALB                = absent
listener           = absent
Terraform resources = 0
current .tflock    = absent
ECS desired        = 0
ECS running        = 0
ECS pending        = 0
ECS running tasks  = 0
```

The temporary recovery workflow was removed from the branch.

The temporary rollback interruption instrumentation was reverted.

The existing rollback workflow again matches `main`.

The temporary `development` environment branch policy was deleted.

The only allowed deployment branch is again:

```
main
```

No experiment-specific IAM permission remains.

## Acceptance evidence

| Requirement                                         | Evidence                                                              | Result |
| --------------------------------------------------- | --------------------------------------------------------------------- | ------ |
| Create temporary verification ALB through Terraform | Run `34642213628`; state serial advanced to `10`                      | Pass   |
| Interrupt cleanup after apply                       | Force-cancel at `20:07:25Z`; destroy step skipped                     | Pass   |
| Start separate fresh workflow run                   | Recovery run `34642955772`                                            | Pass   |
| Fresh runner has no previous filesystem             | Empty pre-checkout workspace; no local Terraform files after checkout | Pass   |
| Recover existing Terraform resources                | State serial `10`; exact ALB/listener addresses and ARNs recovered    | Pass   |
| Destroy orphan from fresh runner                    | Saved plan contained exactly two deletes; apply destroyed both        | Pass   |
| Independently verify deletion                       | `LoadBalancerNotFound` and `ListenerNotFound`                         | Pass   |
| Restore Terraform baseline                          | State serial `11`, zero resources                                     | Pass   |
| Restore ECS baseline                                | `0 / 0 / 0`, independent running-task list `[]`                       | Pass   |
| Reproduce stale/held lock safely                    | Synthetic lock caused S3 `412 PreconditionFailed`, Terraform exit `1` | Pass   |
| Record maximum orphan lifetime                      | Approximately `8m 28s`                                                | Pass   |
| Remove temporary experiment controls                | Workflow instrumentation and branch policy removed                    | Pass   |

## Focused time

Measured supervised experiment window:

```
approximately 30 minutes
```

This covers the first workflow dispatch through infrastructure recovery, stale-lock validation, and temporary control cleanup.

Earlier planning and final documentation time are not included in that measured execution window.

## Learning summary

The experiment answered the main questions from Issue #37.

What survives a CI runner?

```
durable external systems survive
runner-local filesystem state does not
```

For this workflow, the important durable system is the S3 Terraform backend.

What information comes from Terraform state?

```
Terraform ownership
addresses
resource attributes
ARNs
lineage
serial
```

What information comes from AWS discovery?

```
actual resource existence
actual resource deletion
ECS runtime status
```

What can `if: always()` protect?

```
failures where the runner remains available to execute cleanup
```

What requires external recovery?

```
hard cancellation
runner disappearance
any failure where cleanup steps never execute
```

The resulting recovery model is:

```
durable remote state
    +
state locking
    +
least-privilege AWS access
    +
fresh-runner Terraform initialization
    +
exact destroy-plan review
    +
independent AWS verification
```

## Next experiment

Issue #38:

```
Add ECS application logging to CloudWatch
```

The next experiment moves from infrastructure recoverability to application diagnostics.

It should establish a durable, cost-conscious log path for the demo API so failed ECS tasks can be investigated using application stdout/stderr rather than relying only on ECS control-plane status.

The next failure experiments should cover:

```
missing CloudWatch Logs permission
incorrect log group or region configuration
application error output before container exit
```
