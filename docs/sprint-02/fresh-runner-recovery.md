# Fresh-runner recovery of orphaned verification infrastructure

## Objective

Issue #37 proves that temporary verification infrastructure can be recovered and removed after the GitHub Actions execution that created it stops before cleanup.

The controlled experiment uses force-cancellation as a safe proxy for runner loss. It directly tests recovery after cleanup is skipped, but it does not prove that the original GitHub-hosted runner VM itself was destroyed.

Issue #35 established durable remote Terraform state.

Issue #36 added S3-native state locking.

Issue #37 tests the next failure boundary:

```
Terraform apply succeeds
    ↓
temporary verification infrastructure exists
    ↓
original execution is force-cancelled before destroy
    ↓
later cleanup steps do not execute
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

This experiment is a force-cancellation proxy for the harder runner-loss failure mode.

It directly proves that GitHub can stop the execution after infrastructure creation without scheduling the later cleanup steps.

It does not directly prove that the underlying GitHub-hosted runner VM was destroyed or that its workspace became inaccessible. Literal runner-loss behavior therefore remains unverified.

The recovery experiment instead validates the property required for that failure mode: recovery does not depend on anything stored in the original runner workspace.

### Post-cancel orphan evidence

At:

```
2026-09-11T20:08:28Z
```

the force-cancelled execution was no longer running cleanup, but AWS still reported the ALB as:

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

The temporary infrastructure had therefore survived the force-cancelled execution that created it.

### Result

Pass.

The interruption successfully created a Terraform-managed orphan without starting ECS compute.

## What survived the interrupted execution

The experiment did not directly prove destruction of the original GitHub-hosted runner VM or its workspace.

What it did prove is that the separate recovery run did not rely on any runner-local recovery artifact from the interrupted execution.

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

This is the critical recovery boundary for the tested force-cancellation scenario.

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

### Copyable recovery procedure

The recovery path can be executed from a clean workspace using only the repository configuration, remote state, and AWS access.

Initialize the backend and inspect the recovered ownership:

```bash
ROOT="infra/terraform/development-verification"

terraform -chdir="$ROOT" init -input=false -lockfile=readonly
terraform -chdir="$ROOT" state list
terraform -chdir="$ROOT" state pull > /tmp/issue37-recovered.tfstate
```

Capture the exact ALB and listener ARNs from Terraform state:

```bash
terraform -chdir="$ROOT" show -json > /tmp/issue37-current.json

ALB_ARN="$(jq -r '.values.root_module.resources[] | select(.address == "aws_lb.verification") | .values.arn' /tmp/issue37-current.json)"
LISTENER_ARN="$(jq -r '.values.root_module.resources[] | select(.address == "aws_lb_listener.http") | .values.arn' /tmp/issue37-current.json)"

printf 'ALB: %s\nListener: %s\n' "$ALB_ARN" "$LISTENER_ARN"
```

Create a saved destroy plan:

```bash
terraform -chdir="$ROOT" plan -destroy -input=false -lock-timeout=60s -out=recovery-destroy.tfplan
terraform -chdir="$ROOT" show -json recovery-destroy.tfplan > /tmp/issue37-recovery-plan.json
```

Allowlist the destructive changes before applying:

```bash
ACTUAL="$(jq -c '[.resource_changes[] | select(.change.actions != ["no-op"]) | {address,actions:.change.actions}] | sort_by(.address)' /tmp/issue37-recovery-plan.json)"
EXPECTED='[{"address":"aws_lb.verification","actions":["delete"]},{"address":"aws_lb_listener.http","actions":["delete"]}]'

printf '%s\n' "$ACTUAL" | jq .

if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "Refusing recovery: destroy plan contains unexpected changes."
  exit 1
fi
```

Apply only the reviewed saved plan:

```bash
terraform -chdir="$ROOT" apply -input=false -lock-timeout=60s -auto-approve recovery-destroy.tfplan
```

Independently verify AWS deletion:

```bash
if AWS_PAGER="" aws elbv2 describe-load-balancers --profile sandbox --region eu-west-3 --load-balancer-arns "$ALB_ARN" >/tmp/issue37-alb.out 2>/tmp/issue37-alb.err; then
  echo "ERROR: ALB still exists."
  exit 1
elif grep -q 'LoadBalancerNotFound' /tmp/issue37-alb.err; then
  echo "ALB absent."
else
  cat /tmp/issue37-alb.err
  echo "ERROR: ALB absence could not be verified."
  exit 1
fi

if AWS_PAGER="" aws elbv2 describe-listeners --profile sandbox --region eu-west-3 --listener-arns "$LISTENER_ARN" >/tmp/issue37-listener.out 2>/tmp/issue37-listener.err; then
  echo "ERROR: listener still exists."
  exit 1
elif grep -q 'ListenerNotFound' /tmp/issue37-listener.err; then
  echo "Listener absent."
else
  cat /tmp/issue37-listener.err
  echo "ERROR: listener absence could not be verified."
  exit 1
fi
```

Verify the final remote-state and ECS baseline:

```bash
terraform -chdir="$ROOT" state pull > /tmp/issue37-final.tfstate
jq '{serial,lineage,resource_count:(.resources|length)}' /tmp/issue37-final.tfstate

AWS_PAGER="" aws ecs describe-services   --profile sandbox   --region eu-west-3   --cluster zero-to-prod-dev   --services demo-api   --query 'services[0].{Desired:desiredCount,Running:runningCount,Pending:pendingCount,TaskDefinition:taskDefinition}'   --output json

AWS_PAGER="" aws ecs list-tasks   --profile sandbox   --region eu-west-3   --cluster zero-to-prod-dev   --service-name demo-api   --desired-status RUNNING   --query 'taskArns'   --output json
```

For the controlled stale-lock experiment, first verify that no real lock exists. An expected 404 is handled explicitly and does not rely on shell-wide `set -e` behavior:

```bash
BUCKET="zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate"
LOCK_KEY="development-verification/terraform.tfstate.tflock"

if AWS_PAGER="" aws s3api head-object --profile sandbox --region eu-west-3 --bucket "$BUCKET" --key "$LOCK_KEY" >/tmp/issue37-lock.out 2>/tmp/issue37-lock.err; then
  echo "ERROR: a real Terraform lock already exists."
  cat /tmp/issue37-lock.out
  exit 1
elif grep -q '404' /tmp/issue37-lock.err; then
  echo "No current Terraform lock exists."
else
  cat /tmp/issue37-lock.err
  echo "ERROR: lock absence could not be verified."
  exit 1
fi
```

Create the synthetic lock used by the experiment:

```bash
LOCK_ID="$(cat /proc/sys/kernel/random/uuid)"
LOCK_CREATED="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"

jq -n   --arg ID "$LOCK_ID"   --arg Operation "OperationTypePlan"   --arg Info "Issue 37 controlled stale-lock experiment"   --arg Who "issue-37-controlled-test"   --arg Version "1.15.9"   --arg Created "$LOCK_CREATED"   --arg Path "$BUCKET/development-verification/terraform.tfstate"   '{ID:$ID,Operation:$Operation,Info:$Info,Who:$Who,Version:$Version,Created:$Created,Path:$Path}'   > /tmp/issue37-stale-lock.json

AWS_PAGER="" aws s3api put-object   --profile sandbox   --region eu-west-3   --bucket "$BUCKET"   --key "$LOCK_KEY"   --body /tmp/issue37-stale-lock.json
```

Run the bounded lock-conflict test, record its exit status, and immediately remove the synthetic lock:

```bash
if terraform -chdir="$ROOT" plan -input=false -lock-timeout=5s -refresh=false -no-color >/tmp/issue37-lock-plan.out 2>&1; then
  PLAN_RC=0
else
  PLAN_RC=$?
fi

AWS_PAGER="" aws s3api delete-object   --profile sandbox   --region eu-west-3   --bucket "$BUCKET"   --key "$LOCK_KEY"

cat /tmp/issue37-lock-plan.out
echo "Terraform exit: $PLAN_RC"
```

Finally, verify that the synthetic lock is gone:

```bash
if AWS_PAGER="" aws s3api head-object --profile sandbox --region eu-west-3 --bucket "$BUCKET" --key "$LOCK_KEY" >/tmp/issue37-lock-final.out 2>/tmp/issue37-lock-final.err; then
  echo "ERROR: lock still exists."
  exit 1
elif grep -q '404' /tmp/issue37-lock-final.err; then
  echo "Synthetic lock removed."
else
  cat /tmp/issue37-lock-final.err
  echo "ERROR: final lock absence could not be verified."
  exit 1
fi
```

The recovery procedure deliberately does not use `-lock=false`.

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

The presence of an `always()` cleanup condition cannot guarantee cleanup when the workflow execution itself is stopped before cleanup can be scheduled.

The experiment directly proved this force-cancellation path:

```
ordinary step failure
    ↓
runner remains available
    ↓
always() cleanup can run
```

versus:

```
force-cancellation after infrastructure creation
    ↓
later cleanup step is skipped
    ↓
durable remote state + external recovery required
```

A literal unexpected runner disappearance was not directly reproduced. It remains an unverified failure mode, but the recovery design intentionally avoids dependence on the original runner filesystem.

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

Total observed ALB lifetime:

```
approximately 8 minutes 28 seconds
```

This measures resource creation through completed destruction.

The workflow was force-cancelled at:

```
2026-09-11T20:07:25Z
```

Maximum observed orphan lifetime, measured from force-cancellation until completed recovery:

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

The AWS Cost Explorer console later showed the September 11 cost split clearly.

Elastic Load Balancing cost attributable to the Issue #37 infrastructure experiment:

`$0.03`

A separate `$0.03` was shown under Cost Explorer and came from the Cost Explorer API checks performed while investigating the experiment cost. That API-query cost is not counted as part of the infrastructure experiment itself.

The final experiment infrastructure cost recorded for Issue #37 is therefore:

`$0.03`

ECS contributed no experiment runtime cost because the service remained at:

`desired = 0`

The total additional cost visible for September 11 related to the experiment and its billing investigation was approximately:

`$0.06`

Of that amount, approximately half was the ELB experiment and half was avoidable Cost Explorer API usage.

Future cost checks for this project should use the AWS Cost Explorer console rather than the Cost Explorer API unless machine-readable billing evidence is explicitly required.

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

| Requirement                                         | Evidence                                                                                                  | Result |
| --------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | ------ |
| Create temporary verification ALB through Terraform | Run `34642213628`; state serial advanced to `10`                                                          | Pass   |
| Interrupt cleanup after apply                       | Force-cancel at `20:07:25Z`; destroy step skipped                                                         | Pass   |
| Start separate fresh workflow run                   | Recovery run `34642955772`                                                                                | Pass   |
| Fresh runner has no previous filesystem             | Empty pre-checkout workspace; no local Terraform state, plan, or initialized backend cache after checkout | Pass   |
| Recover existing Terraform resources                | State serial `10`; exact ALB/listener addresses and ARNs recovered                                        | Pass   |
| Destroy orphan from fresh runner                    | Saved plan contained exactly two deletes; apply destroyed both                                            | Pass   |
| Independently verify deletion                       | `LoadBalancerNotFound` and `ListenerNotFound`                                                             | Pass   |
| Restore Terraform baseline                          | State serial `11`, zero resources                                                                         | Pass   |
| Restore ECS baseline                                | `0 / 0 / 0`, independent running-task list `[]`                                                           | Pass   |
| Reproduce stale/held lock safely                    | Synthetic lock caused S3 `412 PreconditionFailed`, Terraform exit `1`                                     | Pass   |
| Record maximum orphan lifetime                      | Approximately `5m 51s`                                                                                    | Pass   |
| Remove temporary experiment controls                | Workflow instrumentation and branch policy removed                                                        | Pass   |

## Completion reflection status

The evidence, cleanup, mistake/knowledge-gap notes, next experiment, and focused-time record are complete.

The completion reflection is now complete.

The final Issue #37 infrastructure experiment cost was `$0.03` for Elastic Load Balancing.

An additional `$0.03` shown under Cost Explorer came from Cost Explorer API requests used during the billing investigation and is recorded separately from the infrastructure experiment cost.

The total additional September 11 cost related to the experiment and its cost investigation was therefore approximately `$0.06`.

## Focused time

Measured supervised experiment window:

```
approximately 30 minutes
```

This covers the first workflow dispatch through infrastructure recovery, stale-lock validation, and temporary control cleanup.

Earlier planning and final documentation time are not included in that measured execution window.

## Learning summary

The experiment answered the main questions from Issue #37.

What recovery data remains available after an interrupted CI execution?

```
durable external systems remain available
the fresh runner has no runner-local recovery artifacts from the interrupted execution
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

What requires external recovery in the tested scenario?

```
force-cancellation after infrastructure creation
any failure where cleanup steps never execute
```

Literal unexpected runner disappearance was not directly tested, but the same recovery design is intentionally independent of the original runner filesystem.

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
