# Sprint 02 — Test Rollback Compatibility with Runtime Configuration Changes

## Purpose

Issue #42 tests whether an application image alone is a sufficient rollback unit when ECS runtime configuration evolves independently.

The experiment intentionally created two application/runtime contracts:

```text
Version A expects RUNTIME_CONTRACT=A
Version B expects RUNTIME_CONTRACT=B
```

Both versions were first deployed successfully with matching runtime configuration.

The critical rollback experiment then attempted:

```text
historical image A
+
current runtime configuration B
```

The result proved that an artifact can be historically verified and still be incompatible with the current runtime configuration.

---

## Previous rollback behavior

Before Issue #42, the rollback workflow did not restore the historical ECS task definition.

Its behavior was:

```text
select historical verified image
    ↓
read CURRENT repository task-definition template
    ↓
replace only demo-api image
    ↓
register fresh ECS task-definition revision
    ↓
deploy
```

Therefore rollback actually meant:

```text
historical application image
+
current runtime/task configuration
```

The historical `task_definition_arn` stored in the deployment record was provenance only.

It was not resolved or restored during rollback.

---

## Controlled runtime contract

The experiment used an explicit environment variable:

```text
RUNTIME_CONTRACT
```

The application also contained its expected contract.

Startup succeeds only when:

```text
expected runtime contract == configured runtime contract
```

A mismatch fails fast with exit code `1`.

This made the configuration compatibility boundary deterministic and directly observable.

`PORT` was intentionally not used for the experiment because changing the listening port would mix application configuration compatibility with ECS port mappings, health checks, and load-balancer behavior.

---

## Version A

Application expectation:

```text
RUNTIME_CONTRACT=A
```

ECS task-definition configuration:

```text
RUNTIME_CONTRACT=A
```

Release SHA:

```text
6d31dd64653de544172affede7c33f6881dd2e70
```

Deployment workflow run:

```text
35465641667
```

Registered task definition:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:22
```

Image digest:

```text
sha256:d7252d89ad7e30d5c356c0b5ee4689109cd51c6e2575c7c32640564711bdb3b6
```

The deployment completed successfully.

Runtime logs confirmed:

```text
runtime contract verified: expected="A" observed="A"
```

The persisted deployment record reported successful health and exact-version verification.

At this point deployment records were still schema version `1`.

---

## Version B

Application expectation:

```text
RUNTIME_CONTRACT=B
```

ECS task-definition configuration:

```text
RUNTIME_CONTRACT=B
```

Release SHA:

```text
d5950df34b6248ce810a83293fa0da9ec1199d5f
```

Deployment workflow run:

```text
35466549066
```

Registered task definition:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:23
```

Image digest:

```text
sha256:23cbe8d768e5f0841f988579cf220fa2abd63774c99ae26713d44140a8332613
```

The deployment completed successfully.

Runtime logs confirmed the B/B contract.

---

## Incompatible rollback experiment

The rollback target was Version A:

```text
6d31dd64653de544172affede7c33f6881dd2e70
```

The repository task-definition template was now Version B configuration:

```text
RUNTIME_CONTRACT=B
```

Rollback workflow run:

```text
35467167587
```

Rollback eligibility passed under the previous schema-v1 policy because the historical record proved:

```text
correct environment
correct Git SHA
correct image URI
correct image digest
historical healthy deployment
historical exact version match
```

It did not prove that the historical artifact was compatible with the current runtime configuration.

The workflow therefore rendered and registered:

```text
task definition :24
```

with:

```text
image = Version A
RUNTIME_CONTRACT = B
```

This produced exactly the intended incompatible hybrid:

```text
application contract A
+
runtime configuration B
```

---

## Failure evidence

The Version A container failed fast under Version B configuration.

CloudWatch logs reported:

```text
invalid runtime configuration: RUNTIME_CONTRACT mismatch: expected "A", got "B"
```

Stopped tasks reported:

```text
EssentialContainerExited
exit code: 1
```

During the failed rollout, ECS showed:

```text
new :24 deployment
desired = 1
running = 0
failedTasks = 2
```

while the previous Version B task-definition deployment remained available.

The rollback workflow failed at:

```text
Wait for rollback service stability
```

and therefore skipped:

```text
task-definition verification
HTTP /health verification
HTTP /version verification
```

Cleanup steps still succeeded.

The failed rollback path took approximately 15 minutes because ECS service stabilization waited for the incompatible tasks before the workflow reached cleanup.

---

## Cleanup observation

After cleanup:

```text
desired = 0
running = 0
pending = 0
```

and the temporary verification ALB was absent.

One important observation was that scaling the service back to zero did not restore the ECS service pointer to the previous task definition.

After the failed experiment, the service still referenced the failed fresh revision:

```text
:24
```

even though no tasks were running.

This means cleanup restores the project's zero-runtime cost baseline, but it is not equivalent to restoring the previous ECS service configuration.

A later normal deployment replaces the service task-definition pointer again.

---

## What the experiment proved

The experiment proved:

```text
historically verified image
!=
complete rollback-safe release
```

The same image can be:

```text
healthy under runtime configuration A
```

and:

```text
unusable under runtime configuration B
```

Therefore image identity alone is insufficient for rollback eligibility.

The historical task-definition ARN is also insufficient as the decision key.

An ARN identifies one registered revision, but rollback does not currently restore that revision and a revision number does not directly express the semantic compatibility contract.

---

## Release / rollback identity

For the current Zero-to-Prod architecture, rollback identity is now modeled as:

```text
target environment
+
immutable application image digest
+
runtime configuration digest
```

The Git SHA remains the operator-facing release identifier and is bound to the immutable image through the deployment record.

Conceptually:

```text
release identity
=
(environment, image_digest, runtime_config_digest)
```

The historical `task_definition_arn` remains provenance.

It is not the configuration rollback mechanism.

---

## Runtime configuration digest

Issue #42 adds:

```text
scripts/runtime-config-digest.sh
```

The helper computes a deterministic SHA-256 digest over the ECS task-definition registration document.

Before hashing, only the `demo-api` application image is normalized to:

```text
__RELEASE_IMAGE_EXCLUDED__
```

The document is then serialized with:

```text
jq -S -c
```

and hashed with SHA-256.

This means changes such as the following affect runtime configuration identity:

```text
environment variables
CPU / memory
port mappings
health checks
commands
entrypoints
IAM role references
log configuration
sidecar definitions and images
other task-definition fields
```

Changing only the `demo-api` application image does not change the runtime configuration digest.

The digest is intentionally conservative.

JSON object key order and formatting are normalized.

Array order is not semantically normalized, so an equivalent array reorder may produce a different digest and reject a rollback.

That is a fail-closed false negative rather than an unsafe false positive.

---

## Digest experiment

Current Version B runtime configuration digest:

```text
sha256:024366f5afead2046b14092e5131b63df2bcd89775f6244845ff41ec7c195991
```

Synthetic Version A runtime configuration digest:

```text
sha256:ccc5b9309c64a9c4bdf5e3a775887d6e684ba17932477eefecee152f915703ee
```

Regression tests proved:

```text
application image change only
    → same runtime config digest

RUNTIME_CONTRACT B → A
    → different runtime config digest

JSON formatting / object key ordering change
    → same runtime config digest

missing demo-api container
    → rejected
```

---

## Deployment record schema v2

Verified deployment records now use:

```text
schema_version = 2
```

and include:

```text
runtime_config_digest
```

A rollback record must now prove:

```text
schema_version == 2
verification_status == verified
environment matches
Git SHA matches
image URI matches
image digest matches
runtime_config_digest matches CURRENT runtime config digest
historical health is healthy
historical exact version verification succeeded
required provenance exists
```

Schema-v1 records do not contain runtime configuration identity.

They are therefore rejected fail-closed for rollback under the new policy.

No historical schema-v1 record is silently upgraded or inferred.

---

## First real schema-v2 deployment

PR:

```text
#71 — Make rollback eligibility runtime-config aware
```

Merge SHA:

```text
968ff40d753f273c9172e5a58b51a334115db258
```

Main workflow run:

```text
35468705581
```

Registered task definition:

```text
arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:25
```

Image digest:

```text
sha256:216de012b4fe7928cfc648671967673afee172fb9f1883be12c3ecdc92f4a639
```

Persisted runtime configuration digest:

```text
sha256:024366f5afead2046b14092e5131b63df2bcd89775f6244845ff41ec7c195991
```

The durable record is:

```text
s3://zero-to-prod-333534066371-eu-west-3-deployment-records/development/968ff40d753f273c9172e5a58b51a334115db258.json
```

It reported:

```text
schema_version: 2
verification_status: verified
environment: development
health.status: healthy
version.expected: 968ff40d753f273c9172e5a58b51a334115db258
version.observed: 968ff40d753f273c9172e5a58b51a334115db258
version.matches: true
workflow_run_id: 35468705581
```

The recorded runtime configuration digest exactly matched a fresh digest calculated from the current repository task-definition template.

---

## Live schema-v2 eligibility verification

The real schema-v2 record was passed through:

```text
scripts/verify-rollback-eligibility.sh
```

using the current ECR image digest and current runtime configuration digest.

Result:

```text
Rollback eligibility: ELIGIBLE
```

with:

```text
Target SHA:
968ff40d753f273c9172e5a58b51a334115db258

Image digest:
sha256:216de012b4fe7928cfc648671967673afee172fb9f1883be12c3ecdc92f4a639

Runtime config digest:
sha256:024366f5afead2046b14092e5131b63df2bcd89775f6244845ff41ec7c195991
```

This check did not create Terraform, ALB, ECS, or Fargate runtime resources.

---

## New rollback behavior

The rollback decision is now:

```text
explicit rollback request
    ↓
valid full SHA
    ↓
immutable ECR image exists
    ↓
compute CURRENT runtime configuration digest
    ↓
retrieve historical verified deployment record
    ↓
require schema v2
    ↓
require historical image identity match
    ↓
require historical runtime config digest == current runtime config digest
    ↓
ELIGIBLE
    ↓
only then create verification infrastructure and deploy
```

An incompatible or unproven configuration is rejected before:

```text
Terraform apply
temporary ALB creation
task-definition registration
Fargate execution
```

This avoids repeating the approximately 15-minute incompatible runtime failure observed in the original experiment.

---

## What rollback restores

The current rollback implementation restores:

```text
historical immutable demo-api image
```

provided its verified runtime configuration identity matches the current runtime configuration.

The workflow then registers a fresh ECS task-definition revision.

---

## What rollback does NOT restore

The current implementation does not restore:

```text
historical ECS task-definition JSON
historical environment variable values independently of the current template
historical Secrets Manager / Parameter Store secret values
historical external service configuration
database contents
database schema
external dependency versions
IAM policy state outside the task-definition document
network infrastructure outside the task-definition document
```

A secret reference ARN present in the task definition contributes to the digest.

The secret value behind an unchanged ARN does not.

Database/schema compatibility is also outside this digest.

Therefore:

```text
matching runtime_config_digest
```

means:

```text
the task-definition configuration represented by this project matches
```

It does not mean every external runtime dependency has been restored or proven compatible.

---

## Rollback safety claim

Issue #42 does **not** implement full historical configuration rollback.

The tested and implemented guarantee is narrower:

```text
artifact-only rollback is allowed only when the historical verified
task-definition configuration identity matches the current one
```

If configuration identity differs, rollback fails closed.

The project does not claim production-safe configuration restoration.

---

## Cost behavior

The compatibility gate runs before temporary deployment infrastructure.

Rejected runtime configuration compatibility therefore requires only control-plane/read operations.

It does not intentionally create:

```text
verification ALB
running Fargate task
fresh ECS task-definition revision
```

The real incompatible experiment occurred before this safeguard and therefore incurred runtime time while ECS repeatedly attempted incompatible tasks.

---

## Post-merge cleanup evidence

After workflow run:

```text
35468705581
```

ECS reported:

```text
desired = 0
running = 0
pending = 0
taskDefinition = zero-to-prod-demo-api:25
```

The temporary verification ALB lookup returned:

```text
LoadBalancerNotFound
```

The development environment therefore returned to its expected low-cost runtime baseline.

---

## Mistake / knowledge gap discovered

The earlier rollback design implicitly treated:

```text
known-good image
```

as though it represented:

```text
known-good release
```

That assumption was incomplete.

The rollback workflow rebuilt runtime state from the current repository template, so a historical application image could be combined with configuration it had never successfully run under.

The experiment converted that hidden assumption into an explicit compatibility failure.

The resulting model is:

```text
artifact identity
+
runtime configuration identity
```

rather than artifact identity alone.

Another important distinction is that:

```text
task_definition_arn
```

is useful provenance, but it is not equivalent to semantic configuration identity when the rollback workflow does not restore that historical task definition.

---

## Acceptance criteria

Current rollback behavior documented precisely:

```text
verified
```

Controlled runtime/task configuration change between application versions:

```text
verified with Version A and Version B
```

Older image rolled back under newer configuration:

```text
verified
```

Incompatibility failure captured:

```text
verified
```

Release/rollback unit defined:

```text
environment + image digest + runtime configuration digest
```

Deployment metadata extended:

```text
schema v2 + runtime_config_digest
```

Rollback behavior explains restored and non-restored configuration:

```text
verified
```

No claim of full production-safe configuration rollback:

```text
explicitly documented
```

---

## Next experiment

The remaining compatibility boundary is outside the ECS task-definition document.

A useful next experiment is to test rollback behavior when an external dependency changes while both:

```text
image identity
runtime_config_digest
```

remain otherwise valid.

Examples include:

```text
secret value rotation behind the same secret ARN
database/schema change
external API contract change
```

That would test whether release identity needs an additional external dependency or schema compatibility mechanism.
