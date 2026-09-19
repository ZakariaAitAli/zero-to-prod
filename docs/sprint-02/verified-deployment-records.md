# Sprint 02 — Record Machine-Readable Verified Deployment Metadata

## Purpose

Issue #40 adds durable deployment evidence to the development delivery path.

Before this issue, the pipeline could prove a deployment during the workflow run:

```text
publish image
    ↓
deploy candidate
    ↓
external /health
    ↓
exact /version
    ↓
report success
```

but that verification evidence was not persisted as a durable machine-readable release record.

Issue #40 changes the model to:

```text
publish image
    ↓
deploy candidate
    ↓
external /health
    ↓
exact /version
    ↓
verify deployed ECS task definition
    ↓
persist verified deployment record
    ↓
retrieve record by Git SHA
    ↓
cleanup temporary verification infrastructure
```

The central rule is:

```text
a published image is not automatically a verified release
```

A deployment record is created only after the deployment proves that the expected application revision is healthy and externally serving the exact Git SHA.

---

## Target capability

The target capability was:

```text
Persist durable machine-readable release evidence
only after successful external deployment verification.
```

Each verified deployment record contains:

```text
environment
Git SHA
image URI
image digest
task-definition ARN
workflow run identifier
verification timestamp
health result
version result
```

The record must also be:

```text
cheap to retain
retrievable by SHA
protected from silent replacement
written with narrowly scoped CI permissions
```

---

## Storage architecture

Verified deployment records are stored in a dedicated S3 bucket:

```text
zero-to-prod-333534066371-eu-west-3-deployment-records
```

Region:

```text
eu-west-3
```

The deterministic key format is:

```text
<environment>/<full-git-sha>.json
```

For development:

```text
development/<full-git-sha>.json
```

This allows direct retrieval by Git SHA without requiring `s3:ListBucket`.

The deployment-record storage is intentionally separate from the Terraform remote-state bucket.

Terraform state and verified release records have different ownership and access requirements and therefore should not share the same storage namespace.

---

## Bootstrap boundary

The persistent deployment-record bucket has its own Terraform bootstrap root:

```text
infra/terraform/bootstrap/deployment-records
```

It creates:

```text
aws_s3_bucket.deployment_records
aws_s3_bucket_public_access_block.deployment_records
aws_s3_bucket_versioning.deployment_records
aws_s3_bucket_server_side_encryption_configuration.deployment_records
aws_s3_bucket_policy.deployment_records
```

The bootstrap root follows the same project convention as the remote-state bootstrap infrastructure.

The final post-apply plan returned:

```text
No changes. Your infrastructure matches the configuration.
```

---

## Storage protections

The bucket has:

```text
S3 versioning: Enabled
server-side encryption: AES256 / SSE-S3
BlockPublicAcls: true
IgnorePublicAcls: true
BlockPublicPolicy: true
RestrictPublicBuckets: true
```

No customer-managed KMS key was introduced.

For this workload, SSE-S3 avoids unnecessary KMS key management and recurring KMS key cost.

---

## Conditional-write guardrail

Verified records use deterministic keys.

That makes silent replacement dangerous because a second write to:

```text
development/<same-sha>.json
```

could otherwise overwrite the original verification evidence.

The recorder therefore sends:

```text
If-None-Match: *
```

on `PutObject`.

The bucket policy independently requires that condition for writes under:

```text
development/*
```

An unconditional `PutObject` is explicitly denied by the bucket resource policy.

This creates two layers of protection:

```text
application helper
    ↓
conditional PutObject

bucket policy
    ↓
reject unconditional replacement
```

S3 versioning remains enabled as an additional recovery safeguard.

---

## Least-privilege GitHub Actions access

The existing GitHub Actions role is:

```text
zero-to-prod-github-actions
```

Issue #40 adds a dedicated inline policy:

```text
zero-to-prod-deployment-records
```

The policy permits only:

```text
s3:GetObject
s3:PutObject
```

for:

```text
arn:aws:s3:::zero-to-prod-333534066371-eu-west-3-deployment-records/development/*.json
```

The role does not receive:

```text
s3:DeleteObject
s3:ListBucket
s3:*
bucket administration
version-history administration
```

IAM Access Analyzer returned no findings for the identity policy or the bucket resource policy.

IAM simulation also confirmed that unrelated prefixes remain implicitly denied.

---

## Verification evidence contract

`scripts/verify-deployment.sh` now optionally writes machine-readable verification evidence.

The result file is removed before verification starts.

It is written only after:

```text
/health returns healthy
AND
/version exactly matches the expected full Git SHA
```

The result contains:

```json
{
  "verification_timestamp": "...",
  "health": {
    "status": "healthy"
  },
  "version": {
    "expected": "<full-sha>",
    "observed": "<full-sha>",
    "matches": true
  }
}
```

A failed verification therefore cannot reuse stale successful evidence from an earlier attempt.

---

## Deployment record writer

The new helper is:

```text
scripts/record-verified-deployment.sh
```

Before contacting S3, it validates:

```text
full lowercase 40-character Git SHA
sha256 image digest
valid environment name
healthy verification result
version.matches == true
expected SHA == Git SHA
observed SHA == Git SHA
verification timestamp exists
```

If any of those checks fail, the helper exits before writing a record.

For a valid result, it writes:

```text
development/<git-sha>.json
```

using conditional `PutObject`.

It then retrieves the same object immediately and byte-compares the retrieved record with the record it attempted to store.

The workflow therefore proves both:

```text
record creation
AND
record retrieval by Git SHA
```

---

## Successful main deployment experiment

PR #62 merged the implementation to `main`.

Merge commit:

```text
236147faca2751ed69bfa54e463ed5b63281e081
```

GitHub Actions run:

```text
35454383940
```

The real deployment path completed successfully:

```text
publish immutable image
temporary verification ALB
register ECS task definition
start ECS verification task
wait for ECS stability
external /health verification
exact /version verification
verify deployed task-definition ARN
resolve immutable ECR digest
write deployment record
retrieve deployment record
scale ECS back to zero
destroy temporary ALB and listener
```

GitHub Actions reported:

```text
Health verification passed.
Version verification passed.
Deployment verification passed.
Verified deployment record stored and retrieved.
```

The verified image was:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:236147faca2751ed69bfa54e463ed5b63281e081
```

Image digest:

```text
sha256:2e704c3ef7aabe82eb4632aa5bae2f3da82a70a5f42a04402c6769187f102d4d
```

Registered task definition:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:20
```

The record was written to:

```text
s3://zero-to-prod-333534066371-eu-west-3-deployment-records/development/236147faca2751ed69bfa54e463ed5b63281e081.json
```

---

## Persisted record

Independent retrieval after the workflow returned:

```json
{
  "schema_version": 1,
  "verification_status": "verified",
  "environment": "development",
  "git_sha": "236147faca2751ed69bfa54e463ed5b63281e081",
  "image_uri": "333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:236147faca2751ed69bfa54e463ed5b63281e081",
  "image_digest": "sha256:2e704c3ef7aabe82eb4632aa5bae2f3da82a70a5f42a04402c6769187f102d4d",
  "task_definition_arn": "arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:20",
  "workflow_run_id": "35454383940",
  "verification_timestamp": "2026-09-19T16:21:51Z",
  "health": {
    "status": "healthy"
  },
  "version": {
    "expected": "236147faca2751ed69bfa54e463ed5b63281e081",
    "observed": "236147faca2751ed69bfa54e463ed5b63281e081",
    "matches": true
  }
}
```

S3 metadata confirmed:

```text
ContentLength: 766 bytes
ContentType: application/json
ServerSideEncryption: AES256
VersionId: g2JUXQXIBZ_P.pxIaW503yLJfoOjIpS2
```

---

## Failure experiments

### 1. Image exists in ECR but no verified deployment record exists

Before Issue #40 created its first record, the previously deployed SHA:

```text
1f743445334d04fc49ee42b21347178498a0dd79
```

already existed in ECR.

Its image digest was:

```text
sha256:14d22ae8d3e65ca34dffc9b09d1003915511d41ec91b1431ff38073f857fd018
```

A lookup for:

```text
development/1f743445334d04fc49ee42b21347178498a0dd79.json
```

returned:

```text
404 Not Found
```

This proves:

```text
published artifact != verified deployment record
```

An ECR image can exist even though no durable successful-deployment evidence exists for that SHA.

---

### 2. Verification fails before record creation

The verifier was tested with a healthy `/health` response but a `/version` value that did not match the expected SHA.

Result:

```text
verifier exit code: 1
verification result file: absent
```

A deliberately stale result file was removed before verification and was not recreated after the mismatch.

The record helper was also supplied verification evidence containing:

```text
health.status = healthy
version.matches = false
observed SHA != expected SHA
```

Result:

```text
Verification result does not prove this exact Git SHA
```

The helper exited before contacting S3.

The target object remained absent.

This experiment was intentionally performed at the verifier/recorder boundary rather than creating another paid ALB lifecycle solely to reproduce a known version mismatch.

The real external success path was separately proven by GitHub Actions run `35454383940`.

---

### 3. Duplicate attempt to record the same verified SHA

After the successful workflow, the verified SHA was recorded once.

The original S3 version was:

```text
g2JUXQXIBZ_P.pxIaW503yLJfoOjIpS2
```

The same valid record was then submitted again through:

```text
scripts/record-verified-deployment.sh
```

S3 returned:

```text
PreconditionFailed
```

Exit code:

```text
254
```

A subsequent version-history check showed exactly one object version remained:

```text
VersionId: g2JUXQXIBZ_P.pxIaW503yLJfoOjIpS2
Size: 766
IsLatest: true
```

The rejected duplicate therefore created no replacement version.

---

### 4. Unconditional overwrite attempt

An operator session with AdministratorAccess attempted to write the same key without:

```text
If-None-Match: *
```

The bucket policy returned:

```text
AccessDenied
```

AWS reported that the request was blocked by:

```text
an explicit deny in a resource-based policy
```

Exit code:

```text
254
```

The object still had exactly one version afterward.

This proves the immutability control does not rely only on correct behavior by the recorder script.

---

### 5. Missing deployment-record write permission

The dedicated deployment-record inline policy was temporarily removed from:

```text
zero-to-prod-github-actions
```

No GitHub Actions run was active during the experiment.

IAM simulation against the real GitHub Actions role then returned:

```text
s3:GetObject    → implicitDeny
s3:PutObject    → implicitDeny
s3:DeleteObject → implicitDeny
```

An EXIT trap restored the policy immediately after the simulation.

The restored live policy was byte-normalized and compared with the committed policy.

Result:

```text
policy_restored_and_matches_repo=yes
```

This proves deployment-record access depends on the dedicated narrow policy and is not accidentally inherited from broader S3 permissions.

---

## Post-deployment cleanup evidence

After GitHub Actions run `35454383940`, the ECS service returned to:

```text
Desired: 0
Running: 0
Pending: 0
```

The service retained task definition:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:20
```

The temporary verification load balancer lookup returned:

```text
LoadBalancerNotFound
```

The workflow itself reported:

```text
Verification task stopped.
Destroy complete! Resources: 2 destroyed.
Verification infrastructure destroyed.
```

The durable deployment record remained after all temporary verification infrastructure had been removed.

This is the intended lifecycle:

```text
temporary verification infrastructure disappears
        ↓
verified deployment evidence remains
```

---

## Cost

AWS Pricing API data for S3 in:

```text
EU (Paris)
```

reported the following relevant prices.

S3 Standard storage, first 50 TB:

```text
$0.024 per GB-month
```

Tier 1 requests:

```text
PUT/COPY/POST/LIST
$0.0053 per 1,000 requests
$0.0000053 per request
```

Tier 2 requests:

```text
GET and all other requests
$0.0042 per 10,000 requests
$0.00000042 per request
```

The first real deployment record measured:

```text
766 bytes
```

Approximate monthly storage for one record:

```text
766 / 1,000,000,000 × $0.024
≈ $0.0000000184 per month
```

The normal successful record path performs approximately:

```text
1 PutObject
1 GetObject readback
```

Request cost:

```text
PUT = $0.00000530
GET = $0.00000042

total request cost ≈ $0.00000572
```

Including the first month of storage, one record is approximately:

```text
$0.00000574
```

At 100 similarly sized records, initial PUT/GET operations plus one month of storage would still be approximately:

```text
$0.000574
```

At the current project scale, deployment-record storage is therefore operationally negligible compared with temporary ALB/Fargate deployment experiments.

No always-on compute, DynamoDB table, or customer-managed KMS key was introduced.

---

## Retention

Verified deployment records currently have no lifecycle expiration.

The retention policy is:

```text
retain verified deployment records for the life of the project
```

This is intentional because:

```text
records are extremely small
storage cost is negligible
records provide durable release evidence
Issue #41 will use them for rollback eligibility
```

Versioning remains enabled.

Normal verified-record creation does not intentionally create multiple versions for one SHA because conditional writes reject duplicate keys.

A lifecycle policy can be reconsidered only if retained volume or version history becomes materially larger.

---

## Built, published, deployed, and verified

Issue #40 exposed an important distinction.

### Built

A build proves that source code produced an artifact.

It does not prove the artifact exists in the deployment registry or has run successfully.

### Published

A published revision proves that an immutable image exists in ECR.

It does not prove that ECS successfully ran it.

### Deployed

A deployed revision proves that the orchestrator started the requested task definition.

It does not by itself prove that the application is healthy or serving the expected code externally.

### Verified

A verified revision proves that the running deployment passed the required external checks and that durable evidence of those checks was recorded.

For this project:

```text
verified
=
healthy external /health
+
exact external /version
+
expected ECS task definition
+
durable machine-readable record
```

---

## Which facts belong in a release record?

The record contains facts needed to identify both the source revision and the actual deployed artifact:

```text
environment
Git SHA
image URI
image digest
task-definition ARN
workflow run ID
verification timestamp
health result
version result
```

The Git SHA identifies the source revision.

The image digest identifies the immutable container artifact.

The task-definition ARN identifies the ECS deployment configuration.

The workflow run ID connects the record to CI execution evidence.

The verification fields prove why the record was considered successful.

---

## Rollback authority

Before Issue #40, rollback selection could rely on operator knowledge or previous workflow history.

Issue #40 creates durable evidence that can support a stronger rule:

```text
ECR image exists
AND
successful verified deployment record exists
```

should become the minimum evidence for considering a revision rollback-eligible.

The deployment record should be authoritative for previous successful verification status.

It should not eliminate post-rollback verification.

A rollback still changes a live deployment at a new point in time, so the restored revision must again pass:

```text
/health
/version
```

after rollback.

Automating that eligibility decision belongs to Issue #41.

---

## Mistake / knowledge gap

The initial deployment model implicitly treated several states as being closer than they actually are:

```text
image exists
deployment ran
deployment verified
known-good release
```

Issue #40 demonstrated that these are separate facts.

The clearest example was the previous SHA:

```text
1f743445334d04fc49ee42b21347178498a0dd79
```

Its immutable ECR image existed, but no verified deployment record existed.

A published artifact therefore cannot by itself be treated as authoritative evidence that it is known-good.

Another implementation detail discovered during the work was that a conditional write in the client is not sufficient protection by itself.

The storage layer should also reject unconditional replacement.

That led to the S3 bucket-policy guardrail requiring `If-None-Match`.

---

## Acceptance criteria

Issue #40 required successful deployments to write a durable deployment record.

Verified by GitHub Actions run:

```text
35454383940
```

and independent S3 retrieval.

---

Issue #40 required low-cost storage suitable for small JSON objects.

Implemented with S3 Standard.

The first record was 766 bytes and the measured pricing shows negligible storage/request cost at project scale.

---

Issue #40 required environment, Git SHA, image URI, image digest, task-definition ARN, workflow run identifier, verification timestamp, health result, and version result.

All fields are present in the persisted record for:

```text
236147faca2751ed69bfa54e463ed5b63281e081
```

---

Issue #40 required failed or unverified deployments not to be marked verified.

A version-mismatch experiment produced no success evidence.

The record helper also rejected invalid verification evidence before contacting S3.

---

Issue #40 required records to be immutable or protected from accidental silent replacement where practical.

Verified through:

```text
conditional PutObject
bucket-policy conditional-write requirement
duplicate-write PreconditionFailed
unconditional-write explicit AccessDenied
S3 versioning
```

---

Issue #40 required retrieval by Git SHA.

Verified through the deterministic key:

```text
development/<full-sha>.json
```

and successful `GetObject` retrieval of the real main deployment record.

---

Issue #40 required narrowly scoped read/write IAM permissions.

The GitHub Actions role receives only:

```text
s3:GetObject
s3:PutObject
```

for:

```text
development/*.json
```

Missing-permission simulation returned implicit deny after the dedicated policy was removed.

---

Issue #40 required storage cost and retention to be documented.

Both are documented above using AWS Pricing API evidence and the measured 766-byte record.

---

## Next experiment

Issue #41 is:

```text
Make rollback eligibility machine-verifiable
```

The next experiment is to change rollback selection from:

```text
operator identifies a supposedly known-good SHA
```

to:

```text
requested full SHA
    ↓
immutable ECR image exists?
    ↓
verified deployment record exists?
    ↓
record environment matches rollback target?
    ↓
eligible for operator-confirmed rollback
```

The rollback itself must still perform external `/health` and exact `/version` verification after restoring the revision.

---

## Focused time

`~2h`

---

## Result

Issue #40 changes release evidence from:

```text
successful workflow output that eventually disappears from normal operational context
```

to:

```text
durable machine-readable verification evidence keyed by Git SHA
```

The successful production-like development experiment proved the full path:

```text
main merge
    ↓
immutable image published
    ↓
ECS candidate deployed
    ↓
external health verified
    ↓
exact Git SHA verified
    ↓
image digest resolved
    ↓
verified record written conditionally
    ↓
record retrieved by SHA
    ↓
ECS scaled back to zero
    ↓
temporary ALB destroyed
    ↓
verified record remains
```

The project can now distinguish an artifact that merely exists from a revision that has actually been deployed and externally verified.

Issue #41 can use this durable evidence to make rollback eligibility machine-verifiable.
