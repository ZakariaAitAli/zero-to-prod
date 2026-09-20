# Sprint 02 — Operations Runbook

## Purpose

This runbook covers the tested Sprint 02 operational paths for the development-only Zero-to-Prod environment:

- normal deployment;
- failed-deployment diagnosis and cleanup;
- fresh-runner recovery after interrupted cleanup;
- rollback eligibility and operator-triggered rollback;
- final zero-runtime baseline verification;
- evidence-aware ECR retention review.

The AWS sandbox is:

```text
account = 333534066371
region  = eu-west-3
cluster = zero-to-prod-dev
service = demo-api
```

Sprint 02 does not claim production readiness or recovery from every possible failure.

## Normal deployment

Normal deployment happens from a push to `main` when the change classifier determines that deployment is required.

The workflow is:

```text
Demo API CI
```

A manual `workflow_dispatch` of `Demo API CI` performs conservative static validation. It is not the normal deployment trigger.

The deployment path is:

```text
change classification
    ↓
required validation
    ↓
build exact application artifact
    ↓
publish immutable full-SHA ECR image
    ↓
initialize remote Terraform backend
    ↓
create temporary verification ALB
    ↓
register task definition
    ↓
scale ECS 0 → 1
    ↓
wait for service stability
    ↓
external /health
    ↓
exact /version
    ↓
write verified deployment record
    ↓
scale ECS 1 → 0
    ↓
destroy temporary verification ALB
    ↓
return to zero-runtime baseline
```

A successful deployment record is written only after ECS stability, successful external health verification, and an exact version match.

## Ordinary deployment failure

When the GitHub Actions runner remains alive, the deployment workflow attempts bounded diagnostics followed by cleanup.

The deployment job contains:

```text
Collect failed deployment diagnostics
Destroy verification infrastructure
```

Diagnostics are designed not to block cleanup.

The useful evidence depends on the failure layer:

| Failure                                | Primary evidence                       |
| -------------------------------------- | -------------------------------------- |
| Application exits during startup       | stopped-task/container metadata + logs |
| Container health check fails           | ECS service events + task health       |
| ECS service does not stabilize         | ECS service events                     |
| External `/health` fails               | external verifier evidence             |
| Exact `/version` fails                 | external verifier evidence             |
| Application stdout/stderr needed later | CloudWatch Logs                        |

Useful evidence includes:

```text
expected Git SHA
task-definition ARN
image identity
ECS desired/running/pending counts
service deployments/events
stopped-task reason
container exit code/reason
container health
recent CloudWatch logs
external verifier stage/result
```

CloudWatch application logs are retained for 7 days.

Do not treat a waiter timeout by itself as the root cause. Use the correlated diagnostic evidence to determine why stabilization or verification failed.

## Verify cleanup after an ordinary failure

After cleanup, independently verify ECS rather than relying only on workflow success:

```bash
PROFILE="sandbox"
REGION="eu-west-3"
CLUSTER="zero-to-prod-dev"
SERVICE="demo-api"

aws ecs describe-services \
  --profile "$PROFILE" \
  --region "$REGION" \
  --cluster "$CLUSTER" \
  --services "$SERVICE" \
  --query 'services[0].{
    Desired:desiredCount,
    Running:runningCount,
    Pending:pendingCount,
    TaskDefinition:taskDefinition
  }' \
  --output json \
  --no-cli-pager

aws ecs list-tasks \
  --profile "$PROFILE" \
  --region "$REGION" \
  --cluster "$CLUSTER" \
  --service-name "$SERVICE" \
  --desired-status RUNNING \
  --query 'taskArns' \
  --output json \
  --no-cli-pager
```

Expected resting runtime:

```text
desired = 0
running = 0
pending = 0
running taskArns = []
```

Verify that no temporary verification ALB remains:

```bash
aws elbv2 describe-load-balancers \
  --profile "$PROFILE" \
  --region "$REGION" \
  --query 'LoadBalancers[].{
    Name:LoadBalancerName,
    State:State.Code
  }' \
  --output table \
  --no-cli-pager
```

The retained target group:

```text
zero-to-prod-dev-demo-api
```

is expected baseline infrastructure and is not the temporary ALB.

## Hard interruption after Terraform apply

An `always()` cleanup step cannot run if the workflow itself is terminated before GitHub schedules that cleanup.

Sprint 02 therefore uses durable remote Terraform state for recovery from a fresh execution.

The tested recovery model is:

```text
durable remote state
+
native state locking
+
fresh workspace
+
saved destroy plan
+
destructive-change allowlist
+
independent AWS verification
```

Do not use:

```text
-lock=false
```

to bypass locking during recovery.

## Fresh-runner recovery

Use a clean repository checkout with AWS access to the sandbox.

Set the Terraform root:

```bash
ROOT="infra/terraform/development-verification"
```

Initialize the existing remote backend:

```bash
terraform -chdir="$ROOT" init \
  -input=false \
  -lockfile=readonly
```

Inspect recovered Terraform ownership:

```bash
terraform -chdir="$ROOT" state list

terraform -chdir="$ROOT" state pull \
  > /tmp/zero-to-prod-recovered.tfstate
```

If temporary verification infrastructure is present, capture the exact ALB and listener identities from Terraform state:

```bash
terraform -chdir="$ROOT" show -json \
  > /tmp/zero-to-prod-current.json

ALB_ARN="$(
  jq -r '
    .values.root_module.resources[]
    | select(.address == "aws_lb.verification")
    | .values.arn
  ' /tmp/zero-to-prod-current.json
)"

LISTENER_ARN="$(
  jq -r '
    .values.root_module.resources[]
    | select(.address == "aws_lb_listener.http")
    | .values.arn
  ' /tmp/zero-to-prod-current.json
)"

printf 'ALB: %s\nListener: %s\n' \
  "$ALB_ARN" \
  "$LISTENER_ARN"
```

Create a saved destroy plan:

```bash
terraform -chdir="$ROOT" plan \
  -destroy \
  -input=false \
  -lock-timeout=60s \
  -out=recovery-destroy.tfplan

terraform -chdir="$ROOT" show \
  -json recovery-destroy.tfplan \
  > /tmp/zero-to-prod-recovery-plan.json
```

Before applying the plan, allowlist the destructive changes:

```bash
ACTUAL="$(
  jq -c '
    [
      .resource_changes[]
      | select(.change.actions != ["no-op"])
      | {
          address,
          actions: .change.actions
        }
    ]
    | sort_by(.address)
  ' /tmp/zero-to-prod-recovery-plan.json
)"

EXPECTED='[
  {"address":"aws_lb.verification","actions":["delete"]},
  {"address":"aws_lb_listener.http","actions":["delete"]}
]'

EXPECTED="$(jq -c 'sort_by(.address)' <<<"$EXPECTED")"

printf '%s\n' "$ACTUAL" | jq .

if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "Refusing recovery: destroy plan contains unexpected changes."
  exit 1
fi
```

The expected interrupted-cleanup recovery plan is:

```text
0 to add
0 to change
2 to destroy
```

and the only allowed deletions are:

```text
aws_lb.verification
aws_lb_listener.http
```

Apply only the reviewed saved plan:

```bash
terraform -chdir="$ROOT" apply \
  -input=false \
  -lock-timeout=60s \
  -auto-approve \
  recovery-destroy.tfplan
```

## Independently verify recovered-resource deletion

Do not treat Terraform success alone as proof that AWS is clean.

Verify the exact recovered ALB:

```bash
if aws elbv2 describe-load-balancers \
  --profile sandbox \
  --region eu-west-3 \
  --load-balancer-arns "$ALB_ARN" \
  --no-cli-pager \
  >/tmp/zero-to-prod-alb.out \
  2>/tmp/zero-to-prod-alb.err; then

  echo "ERROR: ALB still exists."
  exit 1

elif grep -q 'LoadBalancerNotFound' \
  /tmp/zero-to-prod-alb.err; then

  echo "ALB absent."

else
  cat /tmp/zero-to-prod-alb.err
  echo "ERROR: ALB absence could not be verified."
  exit 1
fi
```

Verify the exact listener:

```bash
if aws elbv2 describe-listeners \
  --profile sandbox \
  --region eu-west-3 \
  --listener-arns "$LISTENER_ARN" \
  --no-cli-pager \
  >/tmp/zero-to-prod-listener.out \
  2>/tmp/zero-to-prod-listener.err; then

  echo "ERROR: listener still exists."
  exit 1

elif grep -q 'ListenerNotFound' \
  /tmp/zero-to-prod-listener.err; then

  echo "Listener absent."

else
  cat /tmp/zero-to-prod-listener.err
  echo "ERROR: listener absence could not be verified."
  exit 1
fi
```

Finally verify Terraform and ECS:

```bash
terraform -chdir="$ROOT" state pull \
  > /tmp/zero-to-prod-final.tfstate

jq '{
  serial,
  lineage,
  managed_resources: [
    .resources[]
    | select(.mode == "managed")
    | (.type + "." + .name)
  ]
}' /tmp/zero-to-prod-final.tfstate

aws ecs describe-services \
  --profile sandbox \
  --region eu-west-3 \
  --cluster zero-to-prod-dev \
  --services demo-api \
  --query 'services[0].{
    Desired:desiredCount,
    Running:runningCount,
    Pending:pendingCount,
    TaskDefinition:taskDefinition
  }' \
  --output json \
  --no-cli-pager

aws ecs list-tasks \
  --profile sandbox \
  --region eu-west-3 \
  --cluster zero-to-prod-dev \
  --service-name demo-api \
  --desired-status RUNNING \
  --query 'taskArns' \
  --output json \
  --no-cli-pager
```

Expected final Terraform/runtime state:

```text
managed Terraform resources = none
ECS desired = 0
ECS running = 0
ECS pending = 0
running ECS tasks = none
temporary ALB = absent
```

## Rollback eligibility

Rollback is not automatic.

Use:

```text
Demo API Rollback
```

with:

```text
target_sha   = full lowercase 40-character Git SHA
confirmation = ROLLBACK
```

A rollback candidate must pass the machine-verifiable eligibility gate before Terraform apply, task-definition registration, or Fargate runtime begins.

Current eligibility requires:

```text
full target SHA is valid
immutable ECR image exists
schema_version = 2
verification_status = verified
environment matches
Git SHA matches
image URI matches
image digest matches
runtime_config_digest matches current configuration
historical health status = healthy
historical exact version matched
required provenance exists
```

Schema-v1 deployment records are historical evidence only and are not rollback-eligible under the current policy.

## Request a rollback

From the GitHub CLI:

```bash
TARGET_SHA="<full-40-character-sha>"

GH_PAGER=cat gh workflow run demo-api-rollback.yml \
  --repo ZakariaAitAli/zero-to-prod \
  --ref main \
  -f target_sha="$TARGET_SHA" \
  -f confirmation=ROLLBACK
```

Inspect the resulting run:

```bash
GH_PAGER=cat gh run list \
  --repo ZakariaAitAli/zero-to-prod \
  --workflow demo-api-rollback.yml \
  --limit 5
```

Historical verification establishes eligibility only.

A successful rollback must still freshly prove:

```text
ECS service stability
+
external /health
+
exact /version for TARGET_SHA
```

and must finish by returning to the zero-runtime baseline.

## Ineligible rollback

Expected fail-closed cases include:

```text
invalid SHA
missing ECR image
missing deployment record
inaccessible deployment record
malformed deployment record
schema-v1 record
wrong environment
image URI mismatch
image digest mismatch
runtime_config_digest mismatch
historical health failure
historical version mismatch
```

An ineligible rollback should fail before creating temporary verification infrastructure or running Fargate compute.

Do not bypass the eligibility check merely because the operator believes an image is known-good.

## Runtime-configuration compatibility boundary

A matching:

```text
runtime_config_digest
```

proves the task-definition configuration represented by the project matches the historically verified configuration.

It does not prove compatibility for mutable external state such as:

```text
secret value rotation behind an unchanged secret reference
database/schema changes
external API/service contract changes
other mutable external dependencies
```

Those remain known limitations and future experiment boundaries.

## ECR retention

The ECR repository uses immutable full-SHA tags.

Sprint 02 intentionally does not enable a blind age- or count-based lifecycle rule.

Before deleting an ECR image, verify all of the following:

```text
the image is not the current ECS service image
the image is not required by a retained schema-v2 verified deployment record
the deletion does not remove the only artifact needed by intended rollback evidence
```

Start by identifying the current ECS image:

```bash
aws ecs describe-services \
  --profile sandbox \
  --region eu-west-3 \
  --cluster zero-to-prod-dev \
  --services demo-api \
  --query 'services[0].taskDefinition' \
  --output text \
  --no-cli-pager
```

Resolve that task definition's application image:

```bash
TASK_DEF="$(
  aws ecs describe-services \
    --profile sandbox \
    --region eu-west-3 \
    --cluster zero-to-prod-dev \
    --services demo-api \
    --query 'services[0].taskDefinition' \
    --output text \
    --no-cli-pager
)"

aws ecs describe-task-definition \
  --profile sandbox \
  --region eu-west-3 \
  --task-definition "$TASK_DEF" \
  --query 'taskDefinition.containerDefinitions[?name==`demo-api`].image' \
  --output text \
  --no-cli-pager
```

Inspect the deterministic deployment record for the candidate SHA before considering deletion:

```bash
CANDIDATE_SHA="<full-40-character-sha>"
RECORD_KEY="development/${CANDIDATE_SHA}.json"
RECORD_FILE="/tmp/zero-to-prod-${CANDIDATE_SHA}.json"

if aws s3api get-object \
  --profile sandbox \
  --region eu-west-3 \
  --bucket zero-to-prod-333534066371-eu-west-3-deployment-records \
  --key "$RECORD_KEY" \
  "$RECORD_FILE" \
  --no-cli-pager \
  >/dev/null 2>&1; then

  jq '{
    schema_version,
    verification_status,
    environment,
    git_sha,
    image_uri,
    image_digest,
    runtime_config_digest
  }' "$RECORD_FILE"

  echo "Deployment evidence exists for this SHA. Review it before deleting the image."
else
  echo "No readable deployment record found for ${CANDIDATE_SHA}."
fi
```

If a retained schema-v2 verified record identifies the candidate as rollback evidence, preserve the image.

Deletion remains an explicit operator decision.

At the Sprint 02 scale, ECR storage is small enough that preserving uncertain images is preferable to unsafe automatic deletion.

## Final baseline verification

At sprint completion, independently verify all of the following.

### ECS

```bash
aws ecs describe-services \
  --profile sandbox \
  --region eu-west-3 \
  --cluster zero-to-prod-dev \
  --services demo-api \
  --query 'services[0].{
    Desired:desiredCount,
    Running:runningCount,
    Pending:pendingCount,
    TaskDefinition:taskDefinition
  }' \
  --output json \
  --no-cli-pager
```

Expected:

```text
Desired = 0
Running = 0
Pending = 0
```

### Running tasks

```bash
aws ecs list-tasks \
  --profile sandbox \
  --region eu-west-3 \
  --cluster zero-to-prod-dev \
  --desired-status RUNNING \
  --output json \
  --no-cli-pager
```

Expected:

```text
taskArns = []
```

### Temporary ALB

```bash
aws elbv2 describe-load-balancers \
  --profile sandbox \
  --region eu-west-3 \
  --query 'LoadBalancers[].{
    Name:LoadBalancerName,
    State:State.Code
  }' \
  --output table \
  --no-cli-pager
```

Expected:

```text
no temporary verification ALB
```

### Terraform state

```bash
ROOT="infra/terraform/development-verification"

terraform -chdir="$ROOT" state list

terraform -chdir="$ROOT" state pull \
  | jq '{
      serial,
      lineage,
      managed_resources: [
        .resources[]
        | select(.mode == "managed")
        | (.type + "." + .name)
      ]
    }'
```

Expected:

```text
managed_resources = []
```

This AWS/Terraform inspection is the authoritative final baseline check. Workflow success alone is not sufficient evidence of cleanup.
