# Terraform state locking and CI backend integration

## Objective

Issue #36 adds state-level mutation protection to the durable S3 backend introduced by Issue #35.

Issue #35 proved that authoritative Terraform state can survive the execution that created it and be recovered from a separate fresh execution.

That solves durability.

It does not by itself solve concurrent mutation.

The question for Issue #36 is:

    What happens when two Terraform executions try to operate on the
    same remote state at the same time?

The required behavior is fail-safe:

    one authoritative state
        +
    competing writer
        ↓
    reject, wait, or safely serialize
        ↓
    no independent concurrent mutation

The implementation also integrates the lock permissions and bounded lock waits into the existing deployment and rollback CI paths.

## Architecture

The development verification Terraform root remains:

    infra/terraform/development-verification

The authoritative backend remains:

    bucket:
    zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate

    key:
    development-verification/terraform.tfstate

    region:
    eu-west-3

Issue #36 does not introduce a second state backend.

Both deployment and rollback continue to use the same Terraform root and therefore the same backend configuration.

The resulting protection layers are:

    GitHub Actions concurrency
        ↓
    serializes deployment / rollback workflows
        ↓
    Terraform S3 state lock
        ↓
    serializes state-sensitive operations for the same backend key

These controls overlap but solve different problems.

## S3 native state locking

The S3 backend now declares:

    backend "s3" {
      bucket       = "zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate"
      key          = "development-verification/terraform.tfstate"
      region       = "eu-west-3"
      encrypt      = true
      use_lockfile = true
    }

Terraform therefore uses the native S3 lock object:

    development-verification/terraform.tfstate.tflock

No DynamoDB table was added.

No separate locking service or always-running infrastructure was introduced.

## Lock lifecycle

A normal Terraform plan produced a temporary S3 lock object.

Historical lock content showed:

    Operation = OperationTypePlan
    Version   = 1.15.9
    Path      = zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate/development-verification/terraform.tfstate

After the operation completed, the current lock object no longer existed.

Because S3 versioning is enabled, its lifecycle remained visible as:

    lock object version
        ↓
    delete marker

The observed historical lock version was approximately 300 bytes.

This proves that the configured backend was actually using the S3 lockfile mechanism rather than merely accepting the configuration syntactically.

## IAM boundary

The GitHub Actions role remains:

    zero-to-prod-github-actions

The state object permissions remain intentionally narrow.

For the authoritative state object:

    s3:GetObject     allowed
    s3:PutObject     allowed
    s3:DeleteObject  implicitDeny

Issue #36 adds a separate statement for exactly:

    development-verification/terraform.tfstate.tflock

with:

    s3:GetObject
    s3:PutObject
    s3:DeleteObject

The resulting effective permissions were simulated against the live role.

Observed state object permissions:

    s3:GetObject     allowed
    s3:PutObject     allowed
    s3:DeleteObject  implicitDeny

Observed lock object permissions:

    s3:GetObject     allowed
    s3:PutObject     allowed
    s3:DeleteObject  allowed

An unrelated state key remained:

    s3:PutObject -> implicitDeny

The policy therefore grants deletion only for the disposable lock object.

It does not grant deletion of the authoritative state.

The policy also does not broaden access to a state prefix wildcard.

## Why the lock needs DeleteObject

The state file and lock file have different lifecycles.

The state object is durable:

    read
    write
    retain

The lock object is temporary:

    create
    inspect
    delete

Terraform must delete the `.tflock` object when an operation releases the lock.

Granting:

    s3:DeleteObject

on the exact lock object is therefore necessary.

Granting it on:

    terraform.tfstate

is not necessary for the normal CI state lifecycle and remains denied.

## GitHub concurrency versus Terraform locking

Both deployment and rollback already use:

    concurrency:
      group: demo-api-development-deployment
      cancel-in-progress: false

GitHub concurrency protects the workflow-level deployment sequence.

For example:

    deploy workflow
        ↓
    verification infrastructure
        ↓
    ECS deployment
        ↓
    verification
        ↓
    cleanup

A rollback workflow using the same group cannot independently enter that sequence at the same time.

Terraform state locking is narrower.

It protects an individual Terraform state operation for the same backend/key.

It also applies to writers outside GitHub Actions.

For example:

    GitHub Actions terraform apply
                  +
    operator laptop terraform apply
                  ↓
           same S3 state key
                  ↓
             lock conflict

GitHub concurrency cannot protect against the operator laptop.

Terraform locking can.

Conversely, a Terraform lock is not held for the entire deployment workflow.

The workflow may perform:

    terraform plan
        ↓
    other deployment checks
        ↓
    terraform apply
        ↓
    application deployment
        ↓
    terraform destroy

Each state-sensitive Terraform operation acquires its own lock.

Terraform locking therefore does not replace workflow-level serialization.

The design intentionally retains both controls.

## Bounded CI lock waits

Deployment and rollback runtime Terraform commands now use:

    -lock-timeout=60s

on:

    terraform plan
    terraform apply
    terraform destroy

This gives transient contention a bounded opportunity to clear.

If the lock remains unavailable, Terraform fails rather than waiting indefinitely or disabling locking.

The workflows do not use:

    -lock=false

PR Terraform validation remains:

    terraform init -backend=false

and therefore does not require AWS access.

## What the lock protects

For operations using the same backend and state key, the lock protects against concurrent Terraform state mutation.

The experiments proved that a second writer cannot independently proceed while another Terraform apply owns:

    development-verification/terraform.tfstate.tflock

The state remained usable after the conflict.

## What the lock does not protect

The lock does not protect against every infrastructure failure.

It does not prevent:

    manual AWS mutations outside Terraform
    AWS CLI operations outside Terraform
    application deployments that do not use this state
    choosing the wrong Terraform state key
    two executions intentionally using different state keys
    hard runner termination after infrastructure creation
    state corruption or privileged state replacement
    incorrect Terraform configuration

In particular:

    same key      -> same lock domain
    different key -> different lock domain

Terraform does not understand that two different state keys are logically intended to represent the same environment.

That requires separate configuration and IAM controls.

## Backend reconfiguration

Adding:

    use_lockfile = true

changed backend configuration but did not change the bucket or state key.

Terraform therefore required:

    terraform init -reconfigure

rather than a state migration.

Before reconfiguration, Terraform refused to use the working directory because the backend block had changed.

It reported that initialization was required and explicitly stated that no existing state had been changed.

After reconfiguration, the backend cache showed:

    bucket       = zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate
    key          = development-verification/terraform.tfstate
    region       = eu-west-3
    encrypt      = true
    use_lockfile = true

The authoritative state remained:

    serial    = 5
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

No persistent current `.tflock` object remained after initialization.

## Failure experiment 1 — concurrent mutation

### Expectation

While one Terraform apply holds the S3 lock, a second state mutation against the same backend/key must not proceed independently.

### Experiment

The first process ran:

    terraform apply

and was intentionally left waiting for interactive approval after acquiring the lock.

The live lock reported:

    ID        = 36ae636f-9d35-1e33-b5e6-f52735e9af9c
    Operation = OperationTypeApply
    Version   = 1.15.9
    Path      = zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate/development-verification/terraform.tfstate

A second process then ran another apply against the same state with a bounded lock timeout.

### Observation

The second apply failed with:

    Error acquiring the state lock

S3 returned:

    StatusCode: 412
    PreconditionFailed

Terraform surfaced the lock metadata from the first process.

The second process exited:

    1

The first apply was then answered:

    no

and Terraform reported:

    Apply cancelled.

### State evidence

Before the experiment:

    serial    = 5
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

After both processes exited:

    serial    = 5
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

The current `.tflock` object returned:

    404 Not Found

### Result

Pass.

The competing writer did not mutate state or infrastructure.

This experiment was intentionally executed outside GitHub Actions.

It therefore directly proves that Terraform state locking protects the backend independently of GitHub workflow concurrency.

## Failure experiment 2 — missing lock permission

### Expectation

An identity that can access the authoritative state but cannot create the lock must fail before the Terraform operation proceeds.

### Experiment

A temporary IAM role was created:

    zero-to-prod-issue36-no-lock-test

Its permissions deliberately allowed:

    state GetObject
    state PutObject

while denying all access to:

    terraform.tfstate.tflock

IAM simulation confirmed:

    state GetObject     -> allowed
    state PutObject     -> allowed
    state DeleteObject  -> implicitDeny

    lock GetObject      -> implicitDeny
    lock PutObject      -> implicitDeny
    lock DeleteObject   -> implicitDeny

The role was then assumed with temporary credentials and used for:

    terraform plan -lock-timeout=5s

### Observation

Terraform failed with:

    Error acquiring the state lock

The decisive AWS error was:

    s3:PutObject
    development-verification/terraform.tfstate.tflock
    403 AccessDenied

Terraform exited:

    1

The error also contained a denied bucket-list attempt while Terraform tried to inspect the unavailable lock.

The lock-object PutObject denial is the relevant failure for this experiment.

### State evidence

Before:

    serial    = 5
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

After:

    serial    = 5
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

No current `.tflock` existed.

### Cleanup

The temporary inline IAM policy was deleted.

The temporary role was deleted.

A subsequent lookup returned:

    NoSuchEntity

### Result

Pass.

State access alone is insufficient once S3 locking is enabled.

The CI role must have the specific lock-object permissions.

## Failure experiment 3 — incorrect state key

### Expectation

A different state key should not silently mutate the intended state.

The experiment must also show the boundary of state locking:

    locking protects a selected key
    locking does not validate that the selected key is correct

### Experiment

A clean temporary Terraform directory was initialized against:

    issue-36-wrong-key/terraform.tfstate

using a privileged sandbox operator identity.

No apply was performed.

Terraform successfully initialized the different key and produced the same fresh-environment plan:

    Plan: 2 to add, 0 to change, 0 to destroy.

The wrong key acquired its own lock:

    issue-36-wrong-key/terraform.tfstate.tflock

and released it after planning.

No current wrong-key state object was written by the plan.

### Intended-state evidence

Before the experiment, the intended object had:

    VersionId   = Q_QjipZKcfVEeo.X026hz2K4VBy_MxAM
    ETag        = 9de140f855712c59c5e1095d7de6c776
    Size        = 181
    LastModified = 2026-09-05T18:15:47+00:00

and:

    serial    = 5
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

After the wrong-key plan, every one of those intended-state values remained unchanged.

### CI safety boundary

The real GitHub Actions role was separately simulated against an unrelated state key.

Observed:

    s3:PutObject -> implicitDeny

Therefore a privileged operator can intentionally point Terraform at a different state key, but the normal CI role is restricted to the configured authoritative state and lock objects.

### Cleanup

The temporary wrong-key lock version and its S3 delete marker were explicitly removed.

A subsequent version listing returned:

    Versions      = null
    DeleteMarkers = null

The intended state object remained unchanged.

### Result

Pass.

The experiment demonstrated:

    wrong key
        ↓
    different state
        ↓
    different lock

State locking is not environment identity validation.

Exact-key IAM is an independent safety boundary.

## Normal plan, apply, and destroy lifecycle

After the failure experiments, a normal lifecycle was executed using the same lock behavior configured for CI.

### Before

Remote state:

    serial    = 5
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

### Plan

Terraform reported:

    Plan: 2 to add, 0 to change, 0 to destroy.

The planned managed resources were:

    aws_lb.verification
    aws_lb_listener.http

### Apply

The saved plan was applied with:

    -lock-timeout=60s

Terraform reported:

    Apply complete! Resources: 2 added, 0 changed, 0 destroyed.

The verification endpoint was:

    http://zero-to-prod-dev-alb-597812662.eu-west-3.elb.amazonaws.com

Remote state became:

    serial    = 6
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 6

The six state entries represented four data sources plus:

    aws_lb.verification
    aws_lb_listener.http

### Destroy

Destroy also used:

    -lock-timeout=60s

Terraform reported:

    Destroy complete! Resources: 2 destroyed.

Final state:

    serial    = 7
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

Independent AWS verification returned:

    LoadBalancerNotFound

for:

    zero-to-prod-dev-alb

The current lock object returned:

    404 Not Found

### Result

Pass.

Normal locked state mutation and cleanup remain functional.

## Fresh-execution recovery

A new temporary directory was created containing only committed Terraform configuration and:

    .terraform.lock.hcl

Before initialization it contained no:

    .terraform/
    terraform.tfstate
    terraform.tfstate.backup

The fresh execution ran the real S3 backend initialization.

It then recovered:

    Terraform state version = 4
    Terraform version       = 1.15.9
    serial                  = 7
    lineage                 = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources               = 0

No previous working directory was copied.

This proves that the authoritative state remained available to a separate fresh execution after the lock integration.

## Runner-local backend metadata nuance

After remote backend initialization Terraform creates:

    .terraform/terraform.tfstate

This filename can be misleading.

Inspection showed:

    version      = 3
    serial       = null
    lineage      = null
    backend_type = s3
    has_resources = false
    has_modules   = false

Its backend configuration contains:

    bucket
    key
    region
    use_lockfile = true

The authoritative S3 state, in contrast, contained:

    version   = 4
    serial    = 7
    lineage   = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resources = 0

Therefore:

    .terraform/terraform.tfstate

is local backend initialization metadata for this remote-backend working directory.

It is not the authoritative infrastructure state.

Repository workflow search also returned:

    OK: no workflow depends on terraform.tfstate

The acceptance requirement is therefore satisfied as:

    no workflow depends on runner-local Terraform state for recovery

rather than the incorrect stronger claim:

    Terraform never creates a local file named terraform.tfstate

## Static CI validation

Issue #36 preserves Issue #43's no-AWS pull-request validation model.

Local validation passed for:

    terraform fmt -check -recursive infra/terraform
    bash syntax for CI policy scripts
    CI path-classifier regression tests
    CI required-gate regression tests
    actionlint v1.7.11

Clean-room Terraform validation also passed for:

    infra/terraform/bootstrap/development-verification-state
    infra/terraform/development-verification

using:

    terraform init -backend=false -input=false -lockfile=readonly
    terraform validate

This is important because an already initialized local working directory had stale backend metadata during an earlier experiment.

A fresh checkout, like a PR runner, does not inherit that cache.

## Deployment and rollback backend integration

The normal deployment workflow and rollback workflow both use:

    infra/terraform/development-verification

Both therefore use the same:

    S3 bucket
    state key
    use_lockfile configuration

Both also retain the same GitHub Actions concurrency group:

    demo-api-development-deployment

Both now use bounded lock waits for runtime:

    plan
    apply
    destroy

No separate rollback state backend was introduced.

No runner-local state recovery path was added.

## Mistakes and knowledge gaps

Several useful implementation mistakes or assumptions were exposed.

### Existing `.terraform` metadata affected `init -backend=false`

An initial local static-validation attempt used an already initialized working directory.

That directory contained cached backend metadata from the previous backend configuration.

After `use_lockfile` changed the backend block, Terraform tried to reconcile the cached backend state and encountered an expired default AWS credential path even though the command included:

    -backend=false

The same validation in a clean temporary directory succeeded without AWS access.

The lesson is:

    clean CI checkout behavior
    is not always identical to
    an already initialized local Terraform directory

Static PR validation should be assessed from a clean working directory when backend-cache behavior is in question.

### State cannot be read before required backend reconfiguration

An initial attempt tried:

    terraform state pull

before:

    terraform init -reconfigure

after changing the backend block.

Terraform correctly refused and reported:

    Backend initialization required

It also explicitly stated that no existing configuration or state had been changed.

The correction was to reconfigure first and then compare the authoritative state.

### IAM simulator command shape

An initial IAM custom-policy simulation passed the policy file using a `file://` value where that CLI argument expected JSON policy content.

AWS returned:

    InvalidInput

The policy itself was valid.

The corrected command compacted the JSON with `jq` and passed the JSON document itself.

The lesson is to distinguish AWS CLI parameters that support:

    file://...

from parameters that expect literal JSON policy content.

### Local `terraform.tfstate` filename was initially interpreted too broadly

A fresh-execution check initially expected there to be no file named:

    terraform.tfstate

after remote backend initialization.

Terraform created:

    .terraform/terraform.tfstate

Inspection showed that this was backend metadata rather than authoritative resource state.

The acceptance criterion is about recovery dependency, not filename absence.

### Wrong-key initialization is a dangerous success mode

A wrong bucket tends to fail loudly.

A reachable wrong key can look like a brand-new Terraform environment.

The wrong-key experiment reinforced the earlier Issue #35 lesson:

    successful backend initialization
    does not prove
    correct environment identity

Exact backend configuration and least-privilege IAM remain necessary.

## Cost impact

No new always-running compute or database was introduced.

Persistent changes are limited to the existing S3 backend behavior and IAM policy.

S3 locking creates very small temporary lock objects and S3 requests.

Because the backend bucket is versioned, historical lock-object versions and delete markers can accumulate.

At the current project scale these objects are only a few hundred bytes each, so storage impact is extremely small, but version accumulation remains a real lifecycle characteristic.

The normal acceptance lifecycle briefly created the existing temporary verification ALB and listener and destroyed them immediately after validation.

Independent AWS verification confirmed the ALB was absent afterward.

No temporary IAM experiment role remains.

No wrong-key S3 experiment objects remain.

Exact AWS billing impact should be verified from billing data rather than estimated from Terraform runtime alone.

## Acceptance evidence status

- [x] Development verification backend configured for durable S3 state.
- [x] S3 native lockfile mechanism enabled with `use_lockfile = true`.
- [x] Lock object lifecycle directly observed.
- [x] Concurrent state mutation rejected safely.
- [x] Missing lock permission fails before Terraform mutation.
- [x] Wrong state key tested without changing intended state.
- [x] Normal plan -> apply -> destroy succeeds with locking enabled.
- [x] State remains available to a separate fresh execution.
- [x] Deployment and rollback use the same Terraform backend configuration.
- [x] GitHub deployment concurrency retained.
- [x] Runtime Terraform lock waits are bounded.
- [x] CI backend IAM is exact-object and least-privilege reviewed.
- [x] State-object deletion remains denied to CI.
- [x] Wrong-key state writes remain denied to CI.
- [x] No workflow depends on runner-local Terraform state for recovery.
- [x] Pull-request Terraform validation remains backend-free.
- [x] Local workflow and Terraform static validation pass.
- [x] Pull-request `CI required` evidence recorded for the final implementation.
- [x] Main-branch OIDC runtime proves the GitHub Actions role can acquire and release the S3 lock.
- [x] Final focused time recorded.

## Final GitHub and AWS evidence

Implementation PR #55, `Add Terraform state locking`, validated commit:

    51f701bc4946ec6a19d271006b91194bffdfbde6

Pull-request workflow run:

    34279739813

completed successfully with the stable fail-closed `CI required` gate green.

The PR merged to `main` as:

    21bb0a0c9bc0d28a09314ab1b5dc6e6358aac809

The merge itself did not intentionally trigger a deployment because the change-aware CI classifier correctly keeps Terraform- and workflow-only changes at:

    deploy=false

A deliberate `main` runtime experiment therefore used the existing protected rollback workflow rather than weakening the classifier or introducing an unrelated application change.

Successful runtime:

    workflow: Demo API Rollback
    run:      34280243917
    ref:      main
    commit:   21bb0a0c9bc0d28a09314ab1b5dc6e6358aac809
    result:   success

The workflow assumed the intended GitHub Actions role through OIDC:

    arn:aws:sts::333534066371:assumed-role/zero-to-prod-github-actions/GitHubActions

Terraform 1.15.9 successfully initialized the S3 backend from a fresh GitHub-hosted runner.

The runtime then executed:

    terraform plan
        -lock-timeout=60s

    terraform apply
        -lock-timeout=60s
        verification.tfplan

    terraform destroy
        -lock-timeout=60s

The plan created two temporary verification resources.

The apply completed with:

    Apply complete! Resources: 2 added, 0 changed, 0 destroyed.

External verification passed for both:

    /health
    /version

The destroy completed with:

    Destroy complete! Resources: 2 destroyed.

### Independent remote-state verification

Before the GitHub runtime:

    serial         = 7
    lineage        = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resource_count = 0

After the GitHub runtime:

    serial         = 9
    lineage        = 71e55630-6ebf-c0b8-9bb1-50500e8816c4
    resource_count = 0

This is the expected state progression:

    serial 7
        ↓ apply
    serial 8
        ↓ destroy
    serial 9

The lineage did not change.

### GitHub lock lifecycle evidence

S3 version history during workflow run `34280243917` contained three lock-object versions and three corresponding delete markers.

Lock versions were created during the plan, apply, and destroy state operations.

The final delete marker was written after destroy.

After the workflow completed:

    HeadObject development-verification/terraform.tfstate.tflock
        -> 404 Not Found

This proves that the GitHub OIDC role had enough permission to acquire and release the native S3 state lock throughout the runtime.

It also confirms why GitHub workflow concurrency and Terraform locking remain separate controls:

- GitHub concurrency serializes the deployment/rollback workflow.
- Terraform locking serializes individual state operations against the selected backend key.
- The successful GitHub run exercised both controls together.

### Independent AWS cleanup verification

After the successful workflow:

    ECS desiredCount = 0
    ECS runningCount = 0
    ECS pendingCount = 0
    running tasks    = []

The temporary verification ALB returned:

    LoadBalancerNotFound

No active Terraform lock remained.

The rollback workflow registered task-definition revision:

    zero-to-prod-demo-api:11

This revision points to the same immutable rollback image used by the experiment.

It is retained ECS metadata only; no task or temporary verification compute remained running after cleanup.

## Focused time

Approximate focused time for Issue #36:

    ~1h 50m

This includes implementation, IAM review, three failure experiments, lifecycle testing, fresh-execution recovery, documentation, PR validation, and the final `main` OIDC runtime experiment.

CI and AWS resource provisioning wait time were part of the observed experiment but were not additional implementation complexity.

## Next experiment

Issue #37: fresh-runner recovery after hard interruption.

The next experiment should intentionally separate resource creation from the runner that performs cleanup.

The target question is:

    If the runner disappears after Terraform creates the temporary
    verification infrastructure, can a separate fresh runner reconnect
    to the same locked remote state, identify ownership, destroy the
    resources, and independently prove the low-cost baseline?

Issue #36 provides the durable and concurrency-safe state foundation required for that recovery experiment.
