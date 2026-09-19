# Sprint 02 — Make Rollback Eligibility Machine-Verifiable

## Purpose

Issue #41 replaces operator-attested rollback eligibility with a machine-verifiable decision based on durable deployment evidence.

> **Current-state note after Issue #42**
>
> The experiments in this document were originally performed with deployment-record schema version `1`.
>
> The current rollback policy uses schema version `2` and additionally requires the historical `runtime_config_digest` to exactly match the digest of the current ECS task-definition configuration.
>
> Schema-v1 records are now rejected fail-closed because they contain no runtime-configuration identity.
>
> The rollback workflow still does not restore a historical task definition. It restores the historical application image only when the recorded runtime configuration is compatible with the current task-definition template.
>
> See `docs/sprint-02/runtime-config-rollback-compatibility.md` for the Issue #42 experiment and current configuration-compatibility contract.

Before this issue, rollback required an operator to identify a revision believed to be known-good.

The workflow verified:

```text
valid full Git SHA
    ↓
immutable ECR image exists
    ↓
deploy selected image
    ↓
fresh external /health
    ↓
fresh exact /version
```

The missing control was proving, before deployment, that the selected artifact had actually succeeded in a previous verified deployment.

Issue #41 changes the decision to:

```text
explicit operator rollback request
    ↓
full Git SHA valid?
    ↓
immutable ECR image exists?
    ↓
verified deployment record exists?
    ↓
record matches target environment?
    ↓
record matches Git SHA?
    ↓
record matches image URI + current ECR digest?
    ↓
historical health + exact-version evidence valid?
    ↓
ROLLBACK ELIGIBLE
    ↓
deploy
    ↓
fresh /health + exact /version verification
```

Historical evidence determines whether an artifact may be selected for rollback.

It does not replace fresh verification after the rollback is deployed.

---

## Implementation

Primary implementation PR:

```text
#64 — Make rollback eligibility machine-verifiable
```

Follow-up integration fix:

```text
#65 — Handle inaccessible rollback deployment records
```

The rollback workflow now:

```text
.github/workflows/demo-api-rollback.yml
```

performs the eligibility gate before:

```text
task-definition rendering
Terraform plan/apply
ECS deployment
external HTTP verification
```

The validator is:

```text
scripts/verify-rollback-eligibility.sh
```

Regression coverage is:

```text
scripts/test-rollback-eligibility.sh
```

The CI path classifier treats changes to these rollback-policy scripts as workflow changes without triggering normal application publication or deployment.

---

## Evidence required for rollback eligibility

For target:

```text
environment = development
Git SHA = <requested full SHA>
```

the workflow first resolves the immutable ECR image and its current digest.

The deployment record must then prove:

```text
schema_version == 1
verification_status == verified
environment == requested target environment
git_sha == requested full SHA
image_uri == expected ECR URI for that SHA
image_digest == current ECR digest
health.status == healthy
version.expected == requested SHA
version.observed == requested SHA
version.matches == true
task_definition_arn is present
workflow_run_id is present
verification_timestamp is present
```

The historical task-definition ARN is retained as provenance.

It is not reused as the rollback task definition.

The rollback creates a fresh task-definition revision using the selected immutable image.

---

## Why bind eligibility to the current ECR digest?

A Git SHA identifies the source revision.

The ECR digest identifies the actual immutable container artifact.

Rollback eligibility therefore requires both:

```text
requested Git SHA
AND
current ECR digest
```

to match the previously verified deployment record.

This prevents a deployment record from being treated as proof for a different container artifact.

The ECR repository is immutable, but the digest comparison still makes the evidence relationship explicit and machine-verifiable.

---

## Controls against accidental rollback

Rollback is still an explicit operator action.

The workflow retains:

```text
workflow_dispatch
main branch requirement
explicit confirmation == ROLLBACK
full lowercase 40-character SHA validation
deployment concurrency
```

These controls reduce accidental invocation.

They do not determine whether the selected artifact is safe.

---

## Controls against unsafe target selection

The eligibility gate independently requires:

```text
immutable ECR image exists
verified deployment record exists
target environment matches
Git SHA matches
image URI matches
image digest matches
historical health verification succeeded
historical exact-version verification succeeded
required provenance fields exist
```

Only after those checks succeed may the workflow create temporary verification infrastructure or modify ECS.

This separates:

```text
Was rollback intentionally requested?
```

from:

```text
Is this specific artifact supported by verified evidence?
```

Both conditions are required.

---

## Regression validation

Local and GitHub Actions validation covers:

```text
missing deployment record
deployment record inaccessible without ListBucket
malformed deployment record
different environment
different image digest
record not verified
different Git SHA
different image URI
unhealthy historical verification
version verification mismatch
unsupported schema version
missing verification timestamp
missing task-definition provenance
missing workflow-run provenance
known-good verified record
```

Result:

```text
15 rollback eligibility cases passed
```

The classifier also confirmed for the eligibility scripts:

```text
app=false
terraform=false
workflow=true
deploy=false
```

The main implementation validation run was:

```text
35460777530
```

The post-merge CI run for PR #64 was:

```text
35461290863
```

Result:

```text
success
publish immutable image: skipped
deploy to development: skipped
```

The follow-up PR #65 CI run was:

```text
35461746711
```

It included:

```text
PASS: malformed deployment record
PASS: different environment
All rollback eligibility tests passed.
```

---

## Failure experiment 1 — valid full SHA with no ECR image

Target:

```text
9d5dfb38cfd00a5f3d33d5c60c87554c03f79867
```

This was a real Git commit:

```text
Merge pull request #64 from ZakariaAitAli/feat/rollback-eligibility
```

A read-only ECR lookup returned:

```text
images: []
failureCode: ImageNotFound
failureReason: Requested image not found
```

Rollback workflow run:

```text
35461461584
```

Result:

```text
Validate rollback SHA          PASS
Verify rollback image exists   FAIL
Verify rollback eligibility    SKIPPED
Terraform                      SKIPPED
ECS deployment                 SKIPPED
HTTP verification              SKIPPED
```

The workflow therefore rejected a syntactically valid, real repository SHA before consulting deployment evidence or creating infrastructure.

This proves:

```text
valid Git SHA != rollback-eligible artifact
```

---

## Failure experiment 2 — ECR image exists but no verified record exists

Target:

```text
1f743445334d04fc49ee42b21347178498a0dd79
```

The Git commit exists.

Its ECR image also exists:

```text
imageDigest:
sha256:14d22ae8d3e65ca34dffc9b09d1003915511d41ec91b1431ff38073f857fd018
```

The expected deployment-record key was:

```text
development/1f743445334d04fc49ee42b21347178498a0dd79.json
```

An operator-side `HeadObject` confirmed that the object does not exist:

```text
404 Not Found
exit=254
```

The first real workflow attempt exposed an important S3/IAM detail.

Because the GitHub Actions role intentionally has:

```text
s3:GetObject
```

for the deterministic object path but does not have:

```text
s3:ListBucket
```

S3 returned:

```text
AccessDenied
```

for the missing object rather than exposing whether the key exists.

The rollback still failed closed and all Terraform/ECS steps were skipped, but the diagnostic initially reported only that the record could not be retrieved.

That resulted in follow-up PR:

```text
#65 — Handle inaccessible rollback deployment records
```

The fix did not add `s3:ListBucket`.

Instead, the validator preserves least privilege and explicitly reports the decision as rejected when the deterministic record is unavailable or inaccessible.

Clean rerun:

```text
35461884393
```

Evidence:

```text
Rollback image exists with digest:
sha256:14d22ae8d3e65ca34dffc9b09d1003915511d41ec91b1431ff38073f857fd018

Rollback eligibility: REJECTED

Deployment record unavailable or inaccessible for
1f743445334d04fc49ee42b21347178498a0dd79
in development
```

Step result:

```text
Verify rollback image exists   PASS
Verify rollback eligibility    FAIL
Render rollback task definition SKIPPED
Terraform                       SKIPPED
ECS deployment                  SKIPPED
HTTP verification               SKIPPED
```

This proves:

```text
immutable image exists
!=
known-good verified release
```

---

## Failure experiment 3 — deployment record for a different environment

This experiment was performed at the deterministic validator boundary rather than by writing fake evidence into the persistent deployment-record bucket.

The test fixture contains an otherwise valid record with:

```text
environment = staging
```

while the rollback target is:

```text
environment = development
```

Expected result:

```text
Deployment record .environment mismatch
```

GitHub Actions run:

```text
35461746711
```

reported:

```text
PASS: different environment
```

The validator rejected the record.

A live S3 object was intentionally not created for this experiment because deployment records are append-only evidence and a deliberately false environment record would become permanent junk evidence.

---

## Failure experiment 4 — malformed deployment record

This experiment was also performed at the validator boundary.

The retrieved record fixture contained invalid JSON:

```text
{not-json
```

Expected result:

```text
Deployment record is malformed JSON
```

GitHub Actions run:

```text
35461746711
```

reported:

```text
PASS: malformed deployment record
```

The validator rejected the record before any rollback infrastructure could be created.

As with the environment-mismatch case, no malformed object was intentionally written into the durable append-only evidence bucket.

---

## Success experiment 5 — known-good verified release

Known-good SHA:

```text
236147faca2751ed69bfa54e463ed5b63281e081
```

Before rollback, the ECR image was independently confirmed:

```text
imageTag:
236147faca2751ed69bfa54e463ed5b63281e081

imageDigest:
sha256:2e704c3ef7aabe82eb4632aa5bae2f3da82a70a5f42a04402c6769187f102d4d
```

The durable deployment record reported:

```text
schema_version: 1
verification_status: verified
environment: development
git_sha: 236147faca2751ed69bfa54e463ed5b63281e081
image_digest: sha256:2e704c3ef7aabe82eb4632aa5bae2f3da82a70a5f42a04402c6769187f102d4d
health.status: healthy
version.expected: 236147faca2751ed69bfa54e463ed5b63281e081
version.observed: 236147faca2751ed69bfa54e463ed5b63281e081
version.matches: true
```

Pre-rollback runtime baseline:

```text
ECS desired: 0
ECS running: 0
ECS pending: 0
temporary ALB: LoadBalancerNotFound
```

Rollback workflow run:

```text
35462010501
```

The machine-verifiable gate reported:

```text
Rollback image exists with digest:
sha256:2e704c3ef7aabe82eb4632aa5bae2f3da82a70a5f42a04402c6769187f102d4d

Rollback eligibility: ELIGIBLE
```

Only after that decision did the workflow create temporary verification infrastructure and deploy the selected image.

Fresh external verification reported:

```text
Expected version:
236147faca2751ed69bfa54e463ed5b63281e081

Observed health status:
healthy

Observed version:
236147faca2751ed69bfa54e463ed5b63281e081

Health verification passed.
Version verification passed.
Deployment verification passed.
```

Measured rollback duration:

```text
115 seconds
```

The workflow then reported:

```text
Destroy complete! Resources: 2 destroyed.
Verification infrastructure destroyed.
```

Independent AWS-side verification after the workflow returned:

```text
desired: 0
running: 0
pending: 0
taskDefinition:
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:21
```

The temporary load balancer lookup returned:

```text
LoadBalancerNotFound
```

The service retaining task-definition revision `:21` is expected.

The service is scaled to zero, so no Fargate task remains running.

---

## What the experiments proved

The experiments establish three distinct states:

```text
Git revision exists
    ≠
deployable image exists
    ≠
verified rollback-eligible release
```

Experiment 1 stopped because no immutable image existed.

Experiment 2 stopped because the image existed but no durable verified deployment evidence existed.

Experiments 3 and 4 proved that merely retrieving an object is insufficient: its contents must satisfy the verification contract.

Experiment 5 proved that a previously verified release can pass the historical evidence gate and still must pass a new runtime verification after rollback.

---

## What evidence is sufficient to call an artifact known-good?

For this project, an ECR image by itself is not enough.

The minimum historical evidence for rollback eligibility is:

```text
full Git SHA
+
immutable ECR image
+
current image digest
+
durable verified deployment record
+
matching target environment
+
successful historical /health
+
exact historical /version
```

This establishes that the exact artifact was previously deployed successfully in the environment for which rollback is being requested.

The deployment record also preserves provenance through:

```text
task-definition ARN
workflow run ID
verification timestamp
```

Those fields explain where the evidence came from, but the historical task definition is not automatically restored.

---

## Why must rollback verification still run?

Historical evidence answers:

```text
Did this artifact work successfully before?
```

It does not answer:

```text
Does this artifact work correctly now?
```

Conditions can change after the original successful deployment.

Examples include:

```text
runtime configuration
secrets
IAM
networking
dependencies
data/schema compatibility
platform behavior
```

For that reason, a known-good record authorizes the rollback attempt but does not declare the rollback successful.

Success still requires fresh:

```text
ECS stability
expected task-definition verification
external /health
exact external /version
```

The successful experiment proved both stages independently:

```text
historical evidence
    ↓
Rollback eligibility: ELIGIBLE
    ↓
new deployment
    ↓
fresh health verification
    ↓
fresh exact-version verification
```

---

## Mistake / knowledge gap discovered

The real missing-record experiment exposed a behavior that the initial mocked tests did not model.

The GitHub Actions role intentionally has object-level:

```text
s3:GetObject
```

but no bucket-level:

```text
s3:ListBucket
```

When the deterministic deployment-record key did not exist, the operator-side administrator lookup could observe a `404`, but the constrained CI role received:

```text
AccessDenied
```

instead.

This is expected S3 behavior when the caller is not allowed to determine bucket contents.

The initial validator therefore correctly failed closed but produced a less useful diagnostic than intended.

The incorrect response would have been to grant:

```text
s3:ListBucket
```

solely to improve the missing-object message.

Instead, PR #65 kept the narrow IAM policy and taught the validator to interpret an inaccessible deterministic record as a rejected eligibility decision.

The resulting behavior is:

```text
Rollback eligibility: REJECTED
Deployment record unavailable or inaccessible ...
```

This preserves both:

```text
least privilege
AND
clear fail-closed behavior
```

The experiment also reinforced why real integration tests are still useful even when deterministic unit-style regression coverage exists.

---

## Acceptance criteria

Issue #41 required rollback to remain an explicit operator action with confirmation.

Verified:

```text
workflow_dispatch
confirmation == ROLLBACK
main branch restriction
```

---

The requested full SHA must exist in ECR.

Verified by experiment 1.

A real full Git SHA without an ECR image was rejected before deployment-record lookup or infrastructure creation.

---

A successful deployment record must exist for the requested SHA.

Verified by experiment 2.

SHA:

```text
1f743445334d04fc49ee42b21347178498a0dd79
```

had an immutable ECR image but no successful deployment record and was rejected.

---

The deployment record must match the target environment.

Verified by the `different environment` regression experiment in GitHub Actions run:

```text
35461746711
```

---

A SHA with an image but no successful verification record must be rejected.

Verified by rollback run:

```text
35461884393
```

The image lookup passed and eligibility failed before Terraform or ECS.

---

A previously verified SHA must be accepted as rollback-eligible.

Verified with:

```text
236147faca2751ed69bfa54e463ed5b63281e081
```

in rollback run:

```text
35462010501
```

The workflow reported:

```text
Rollback eligibility: ELIGIBLE
```

---

Rollback eligibility decisions must be clearly reported.

Verified through explicit workflow output:

```text
Rollback eligibility: REJECTED
```

and:

```text
Rollback eligibility: ELIGIBLE
```

---

Existing post-rollback external `/health` and exact `/version` verification must remain mandatory.

Verified by successful rollback run:

```text
35462010501
```

which reported:

```text
Observed health status: healthy
Observed version: 236147faca2751ed69bfa54e463ed5b63281e081
Health verification passed.
Version verification passed.
Deployment verification passed.
```

---

## Final cleanup state

After the successful known-good rollback experiment, independent AWS inspection returned:

```text
ECS desired: 0
ECS running: 0
ECS pending: 0
```

The temporary verification ALB returned:

```text
LoadBalancerNotFound
```

The ECS service retained:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:21
```

as its configured task-definition revision while scaled to zero.

No temporary ALB or running Fargate task remained.

---

## Next experiment

Issue #42 is:

```text
Test rollback compatibility with runtime configuration changes
```

Issue #41 proves:

```text
this exact image was previously verified
```

but it does not yet prove:

```text
this old image is compatible with the runtime configuration that exists today
```

The next controlled experiment will create:

```text
Version A
    ↓
runtime configuration contract A

Version B
    ↓
changed runtime configuration contract B

then test:

image A + current configuration from B
```

The experiment should determine whether an image alone is a sufficient rollback unit or whether release identity must also capture runtime/task configuration.

---

## Focused time

`~1h 15m`

---

## Result

Rollback selection has changed from:

```text
operator says this SHA was known-good
```

to:

```text
operator explicitly requests rollback
    ↓
workflow verifies immutable artifact
    ↓
workflow verifies durable historical deployment evidence
    ↓
workflow validates environment + SHA + artifact identity
    ↓
machine reports ELIGIBLE or REJECTED
```

A successful historical record is authorization to attempt rollback, not proof that the new rollback execution succeeded.

The final successful experiment demonstrated the complete model:

```text
verified historical release
    ↓
machine-verifiable rollback eligibility
    ↓
fresh task-definition revision
    ↓
temporary verification infrastructure
    ↓
ECS deployment
    ↓
fresh external /health
    ↓
fresh exact /version
    ↓
cleanup to ECS 0/0/0
    ↓
temporary ALB absent
```

Rollback eligibility is now based on durable evidence rather than operator memory.
