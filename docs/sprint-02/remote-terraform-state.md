# Sprint 02 — Bootstrap Remote Terraform State

## Purpose

Issue #35 addresses the most important recovery limitation left by Sprint 01.

Sprint 01 used runner-local Terraform state for:

```text
infra/terraform/development-verification
```

That was sufficient for the normal path:

```text
terraform apply
    ↓
external verification
    ↓
terraform destroy
```

when all Terraform operations happened on the same GitHub Actions runner.

It was not sufficient for recovery after runner loss.

If the runner disappeared after `apply` but before `destroy`, a later runner did not automatically possess the Terraform state required to reconstruct ownership of the temporary verification infrastructure.

Issue #35 moves the authoritative state outside the runner.

The target question was:

```text
Can Terraform state survive the execution that created it
and be accessed from a separate fresh execution?
```

---

## Target capability

The development verification Terraform root now uses durable S3-backed state:

```text
Terraform execution
      ↓
S3 backend
      ↓
versioned state object
      ↓
future Terraform execution
```

The required properties were:

```text
dedicated backend
public access blocked
server-side encryption
versioning
least-privilege CI access
OIDC rather than long-lived AWS credentials
fresh-execution recovery
low recurring cost
```

State locking is intentionally excluded from this issue and belongs to Issue #36.

Hard runner-interruption recovery is intentionally excluded from this issue and belongs to Issue #37.

---

## Bootstrap boundary

The backend cannot use itself before it exists.

For that reason, the backend infrastructure has its own Terraform root:

```text
infra/terraform/bootstrap/development-verification-state
```

This root creates only the durable state storage.

It intentionally retains local bootstrap state.

The application verification root is separate:

```text
infra/terraform/development-verification
```

The dependency direction is:

```text
bootstrap root
    ↓
S3 backend exists
    ↓
verification root can use remote state
```

This avoids a circular dependency where Terraform would require the backend before Terraform could create the backend.

---

## Backend architecture

The dedicated S3 bucket is:

```text
zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate
```

The verification state key is:

```text
development-verification/terraform.tfstate
```

Region:

```text
eu-west-3
```

The bootstrap root creates:

```text
aws_s3_bucket.terraform_state
aws_s3_bucket_public_access_block.terraform_state
aws_s3_bucket_versioning.terraform_state
aws_s3_bucket_server_side_encryption_configuration.terraform_state
```

No compute resources are part of the backend.

No DynamoDB table is created.

No Terraform state locking mechanism is introduced by this issue.

---

## Public access protection

All four S3 public-access controls were independently verified after creation:

```text
BlockPublicAcls        = true
IgnorePublicAcls       = true
BlockPublicPolicy      = true
RestrictPublicBuckets = true
```

The backend is not intended for public access.

---

## Encryption

The bucket uses S3-managed server-side encryption:

```text
SSEAlgorithm = AES256
```

The Terraform backend also declares:

```hcl
encrypt = true
```

A customer-managed KMS key was intentionally not introduced.

For this learning workload, SSE-S3 provides encryption at rest without adding KMS key management or recurring KMS key cost.

---

## Versioning

S3 versioning is enabled.

After the remote-state experiments, the state object had three retained versions:

```text
initial empty remote state
    ↓
state containing verification ALB and listener
    ↓
post-destroy empty state
```

Observed versions:

```text
VCKay6NiSkLqlEdGWSWueqJ2WFHvwkdk   latest   181 bytes
TQZ4s7EvDbJmDYVGysuz1SySasOhKexS   older     14281 bytes
s8G8ktY4xM5OwaBvq6Sqi6khFvIyiI.D   older     181 bytes
```

Total retained state storage at the time of measurement:

```text
14643 bytes
```

Versioning therefore preserves recovery information while the current project-scale storage footprint remains extremely small.

---

## Backend configuration

The verification Terraform root now declares:

```hcl
backend "s3" {
  bucket  = "zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate"
  key     = "development-verification/terraform.tfstate"
  region  = "eu-west-3"
  encrypt = true
}
```

No credentials are stored in Terraform configuration.

Authentication remains external to Terraform.

---

## GitHub Actions authentication

The existing GitHub Actions workflows already authenticate to AWS before Terraform initialization.

The deployment path is:

```text
GitHub Actions
    ↓
OIDC token
    ↓
AssumeRoleWithWebIdentity
    ↓
zero-to-prod-github-actions
    ↓
temporary AWS credentials
    ↓
terraform init
```

The trusted GitHub identities already include:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
repo:ZakariaAitAli/zero-to-prod:environment:development
```

No long-lived AWS access key was introduced.

---

## Least-privilege state access

A dedicated IAM policy was added for the existing GitHub Actions role.

Normal CI state access is restricted to:

```text
s3:ListBucket
    exact backend bucket
    exact state key through s3:prefix

s3:GetObject
s3:PutObject
    exact state object
```

The CI role does not receive:

```text
s3:*
s3:DeleteObject
s3:GetObjectVersion
s3:ListBucketVersions
KMS administration
backend locking permissions
```

Before the state policy was attached, IAM simulation returned:

```text
s3:ListBucket → implicitDeny
s3:GetObject  → implicitDeny
s3:PutObject  → implicitDeny
```

After attaching the policy, simulation allowed the configured bucket and state object.

An unrelated state path remained:

```text
implicitDeny
```

Historical version access also remained:

```text
s3:GetObjectVersion   → implicitDeny
s3:ListBucketVersions → implicitDeny
```

This separates normal CI access from privileged operator recovery access.

---

## State sensitivity

Terraform state is operationally sensitive even when it does not intentionally contain credentials.

State can contain:

```text
resource identifiers
network topology
resource attributes
provider-derived metadata
application infrastructure relationships
```

For that reason:

```text
state is not committed to Git
public access is blocked
CI access is narrowly scoped
historical recovery uses a privileged operator identity
```

The existing repository ignore rules continue to exclude:

```text
*.tfstate
*.tfstate.*
```

---

## Initial migration behavior

Before remote backend adoption, the verification root had a local state file with:

```text
serial    = 10
lineage   = 27fce205-3b06-7565-e006-09b127bb4e9f
resources = 0
```

The source state was empty.

Running:

```text
terraform init -migrate-state -force-copy
```

successfully configured the S3 backend, but did not create a durable S3 state object.

A subsequent state push established the first remote state object.

Because the source state was empty, the S3 backend established a new authoritative state identity:

```text
serial    = 1
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 0
```

The old local lineage was not forced onto the new backend.

This was a useful distinction:

```text
backend configured successfully
```

does not necessarily mean:

```text
durable state object already exists
```

when the source state is empty.

---

## Fresh-execution experiment

A new temporary working directory was created containing only committed Terraform configuration and the provider lock file.

It contained no:

```text
.terraform/
terraform.tfstate
terraform.tfstate.backup
```

After `terraform init`, the fresh execution retrieved:

```text
serial    = 1
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 0
```

The S3 version ID did not change during the read.

This proved that state could be accessed independently of the filesystem that originally configured the backend.

---

## Fresh-execution plan

From the clean working directory, Terraform produced the expected verification plan:

```text
aws_lb.verification
aws_lb_listener.http
```

Result:

```text
Plan: 2 to add, 0 to change, 0 to destroy.
```

The remote state identity remained unchanged after planning.

---

## Remote-state apply evidence

The first fresh execution applied the verification infrastructure.

Terraform created:

```text
aws_lb.verification
aws_lb_listener.http
```

Result:

```text
Resources: 2 added, 0 changed, 0 destroyed.
```

After apply, remote state contained:

```text
serial    = 2
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 6
```

The six addresses represented:

```text
data.aws_lb_target_group.demo_api
data.aws_security_group.alb
data.aws_subnet.alb_a
data.aws_subnet.alb_b
aws_lb.verification
aws_lb_listener.http
```

S3 created a new version containing the managed-resource state.

---

## Second fresh-execution recovery

A second clean working directory was then created.

It had never executed the original apply and contained no local Terraform state or `.terraform` directory.

After initialization it recovered:

```text
serial    = 2
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 6
```

It therefore reconstructed the same ownership information written by the first execution.

This directly answered the Issue #35 experiment:

```text
state survived the execution that wrote it
and was accessible to a separate fresh execution
```

---

## Cross-execution destroy evidence

The second fresh execution performed the destroy.

Plan:

```text
0 to add
0 to change
2 to destroy
```

Destroyed:

```text
aws_lb_listener.http
aws_lb.verification
```

Result:

```text
Destroy complete! Resources: 2 destroyed.
```

Independent AWS verification confirmed that:

```text
zero-to-prod-dev-alb
```

no longer existed.

The resulting remote state was:

```text
serial    = 3
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 0
```

This proves normal cross-execution recovery and cleanup.

It does not yet prove recovery after a hard runner interruption; that experiment belongs to Issue #37.

---

## Historical version recovery experiment

The S3 version written after apply was retrieved explicitly by version ID:

```text
TQZ4s7EvDbJmDYVGysuz1SySasOhKexS
```

The recovered object was:

```text
encrypted with AES256
14281 bytes
serial    = 2
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 6
```

Direct inspection of the recovered state confirmed the historical managed resources:

```text
aws_lb.verification
aws_lb_listener.http
```

The current authoritative state remained:

```text
serial    = 3
resources = 0
```

and its current S3 version ID remained unchanged.

The historical version was therefore recoverable for inspection without promoting stale state back to the authoritative key.

Promoting the old version was intentionally avoided because the ALB had already been destroyed and restoring that state would intentionally make Terraform's ownership record stale.

---

## Failure experiments

### Wrong bucket

Terraform was initialized with a nonexistent bucket.

Result:

```text
NoSuchBucket
exit code 1
```

The backend failed loudly during initialization.

---

### Wrong key

Terraform was initialized against the correct bucket but with:

```text
wrong-path/terraform.tfstate
```

Initialization succeeded.

Terraform then observed:

```text
serial    = 0
lineage   = ""
resources = 0
```

The wrong key did not exist in S3 and was not written during the read.

The correct backend remained:

```text
serial    = 3
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 0
```

This exposed an important failure mode:

```text
wrong bucket → obvious failure

wrong key → can look like a brand-new empty state
```

A reachable backend is therefore not sufficient evidence that the correct state key is configured.

---

### Missing state permission

Before attaching the CI state policy, IAM simulation returned:

```text
ListBucket → implicitDeny
GetObject  → implicitDeny
PutObject  → implicitDeny
```

After attaching the policy, those actions were allowed only for the intended bucket/key.

---

### Inaccessible state outside CI scope

The normal GitHub Actions role remains denied access to:

```text
unrelated state object
historical object versions
bucket version history
```

Observed:

```text
s3:GetObject on unrelated key → implicitDeny
s3:GetObjectVersion           → implicitDeny
s3:ListBucketVersions         → implicitDeny
```

This proves the state policy does not grant general S3 recovery privileges to CI.

---

## Stale local state experiment

After backend migration, the original ignored local state file still existed on disk:

```text
serial    = 10
lineage   = 27fce205-3b06-7565-e006-09b127bb4e9f
resources = 0
```

Terraform from the same checkout nevertheless retrieved the S3 state:

```text
serial    = 3
lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
resources = 0
```

It then produced the expected plan:

```text
Plan: 2 to add, 0 to change, 0 to destroy.
```

The configured backend, rather than the mere presence of a local `terraform.tfstate` file, determines authoritative state.

---

## GitHub Actions OIDC experiment

A temporary read-only workflow was created to test:

```text
GitHub Actions
    ↓
OIDC
    ↓
zero-to-prod-github-actions
    ↓
terraform init
    ↓
S3 state read
```

The workflow was pushed on:

```text
feat/issue-35-remote-state
```

GitHub Actions run:

```text
33981010289
```

The job did not reach a runner.

GitHub rejected the deployment because:

```text
Branch "feat/issue-35-remote-state" is not allowed to deploy
to development due to environment protection rules.
```

No workflow steps executed.

Therefore this run did not test OIDC or S3 access.

The environment protection was intentionally left unchanged.

The authoritative OIDC/backend test must come from the normal `main` deployment path after merge.

---

## Cost

The backend has no always-on compute.

At the time of measurement:

```text
current state object: 181 bytes
total retained version storage: 14643 bytes
```

The project therefore incurs only very small S3 storage and request charges at its current usage level.

No customer-managed KMS key was introduced.

No DynamoDB table was introduced.

Expected recurring backend cost at this scale is effectively negligible and should remain well below one US cent per month unless request volume or retained state volume grows substantially.

---

## What I learned

### 1. Terraform state is part of recovery architecture

State is not only Terraform bookkeeping.

For temporary infrastructure, durable state determines whether a later execution can reconstruct ownership after the original execution disappears.

### 2. Backend bootstrap is a separate infrastructure problem

A remote backend cannot depend on itself for initial creation.

Separating backend bootstrap from application infrastructure avoids that circular dependency.

### 3. Successful backend initialization does not always mean a state object exists

With an empty source state, backend initialization succeeded without creating a durable object.

Durability needed to be verified independently in S3.

### 4. A wrong state key can be more dangerous than a wrong bucket

A nonexistent bucket fails loudly.

A wrong key can initialize successfully and appear to represent a new empty deployment.

### 5. Versioning provides useful recovery evidence

S3 preserved the state before apply, during managed-resource ownership, and after destroy.

The historical managed-resource state could be retrieved without changing the current authoritative version.

### 6. CI does not need recovery-administration permissions

Normal deployment automation needs only the current state object.

Historical state recovery can remain an operator action using a more privileged identity.

### 7. Existing deployment protections should not be weakened for testing

The feature-branch OIDC smoke test was correctly rejected by the protected `development` environment.

The protection boundary was preserved rather than relaxed to make the experiment pass.

---

## Mistakes and corrections

### Assumed backend migration would necessarily write the empty state

`terraform init -migrate-state` successfully configured S3, but no object appeared because the source state was empty.

The backend was verified independently rather than treating successful initialization as proof of persistence.

### Initially treated historical `terraform state list -state=...` output as authoritative evidence

That invocation returned no addresses even though the recovered raw state contained six resources.

The recovered JSON was inspected directly and confirmed the expected addresses.

### Attempted a branch-only OIDC smoke test through the protected development environment

The job was rejected before runner allocation because the feature branch was not permitted to deploy to `development`.

The environment protection was kept intact.

The final OIDC proof remains the normal allowed `main` deployment path.

---

## Knowledge gaps identified

Issue #35 does not yet answer:

```text
How should concurrent state mutations be prevented?
```

That belongs to Issue #36.

It also does not yet answer:

```text
What happens if the CI runner is killed after apply
and before normal destroy?
```

That belongs to Issue #37.

Remote state makes that recovery experiment possible but does not itself prove hard-interruption recovery.

---

## Acceptance criteria

Issue #35 required:

```text
Dedicated S3 backend for development verification.
```

Implemented and independently verified.

---

Issue #35 required:

```text
Public access blocked.
```

All four S3 public-access controls are enabled.

---

Issue #35 required:

```text
Server-side encryption without unnecessary customer-managed KMS.
```

Implemented with SSE-S3:

```text
AES256
```

---

Issue #35 required:

```text
Versioning.
```

Enabled and proven by three retained state versions.

---

Issue #35 required:

```text
Documented backend/bootstrap procedure.
```

Documented in this file.

---

Issue #35 required:

```text
Least-privilege state object/backend operations.
```

Implemented and verified with IAM policy simulation.

---

Issue #35 required:

```text
Verification infrastructure can plan, apply, and destroy
using remote state from separate executions.
```

Verified.

A first clean execution applied the ALB/listener.

A second clean execution recovered the same remote state and destroyed them.

---

Issue #35 required:

```text
GitHub Actions can access the backend through OIDC.
```

The repository and IAM configuration are prepared for this path.

A feature-branch smoke test was blocked before runner allocation by the existing `development` environment protection.

This criterion requires final evidence from the normal allowed `main` deployment after merge.

---

## Focused work performed

Issue #35 included:

```text
Sprint 01 state limitation review
backend architecture design
bootstrap boundary design
S3 bucket Terraform implementation
Terraform version pinning
AWS provider pinning
public access protection
SSE-S3 configuration
versioning configuration
bootstrap plan/apply
independent AWS verification
remote-state IAM design
IAM Access Analyzer validation
IAM simulation before policy attachment
live role-policy attachment
least-privilege verification
S3 backend configuration
local-to-remote initialization
empty-state migration investigation
remote state object establishment
fresh working-directory initialization
fresh execution plan
remote-state apply
second fresh execution recovery
cross-execution destroy
independent ALB cleanup verification
S3 version-history inspection
historical state retrieval
wrong-bucket failure experiment
wrong-key failure experiment
missing-permission simulation
out-of-scope-object denial verification
stale-local-state experiment
GitHub Actions OIDC smoke-test attempt
environment-protection verification
cost-footprint measurement
documentation
```

---

## Result

Issue #35 changes the verification-state model from:

```text
runner filesystem
    ↓
Terraform state
```

to:

```text
Terraform execution
    ↓
durable versioned S3 state
    ↓
future fresh execution
```

The central experiment succeeded:

```text
first clean execution
    ↓
apply temporary verification infrastructure
    ↓
state written to S3
    ↓
first execution discarded
    ↓
second clean execution
    ↓
same state recovered
    ↓
temporary infrastructure destroyed
```

Remote state therefore no longer depends on the lifecycle of the runner that originally wrote it.

The remaining final integration proof is the normal GitHub Actions `main` deployment accessing this backend through OIDC.

---

## Next experiment

Once the post-merge OIDC/backend path is verified, Sprint 02 can proceed to Issue #36.

The next question is:

```text
What prevents two Terraform executions from mutating
the same remote state concurrently?
```

That experiment should add state locking without expanding into hard-runner-interruption recovery, which remains Issue #37.
