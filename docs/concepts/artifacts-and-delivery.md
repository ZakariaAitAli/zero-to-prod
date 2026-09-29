# Artifacts and delivery

A delivery system needs to answer more than "did CI run?"

It needs to know what was built, whether the same artifact moved through later stages, what was actually deployed, whether that deployment was verified, and what can safely be selected during recovery.

## Artifact identity

Sprint 01 identified container images using the full Git commit SHA.

That creates a useful relationship:

```text
source commit
    ↓
full Git SHA
    ↓
container image tag
```

But a meaningful identifier is not the same thing as immutability.

If a registry allows the tag to be reassigned, the same name could later point to different content. Zero-to-Prod therefore combined SHA identity with ECR tag immutability.

## Build once

CI jobs do not automatically share Docker daemon state.

If one job tests an image and another job rebuilds it before publication, the second artifact is a new build even when it comes from the same source commit.

The Sprint 01 workflow corrected this by transferring the already-built image between jobs rather than silently rebuilding it.

The property is:

```text
build
  ↓
test
  ↓
transfer the exact artifact
  ↓
publish
  ↓
deploy
```

This is stronger than "run the same Docker build command twice."

## Artifact identity is not release identity

Sprint 02 deepened this model.

An image existing in ECR proves that an artifact was published. It does not prove that the artifact was successfully deployed and externally verified in a particular environment.

Zero-to-Prod introduced machine-readable verified deployment records so that a known image and a known-good release were not treated as the same thing.

The distinction became:

```text
built
  ≠
published
  ≠
deployed
  ≠
verified
```

Each state establishes a different fact.

## Rollback selects existing history

Sprint 01 rollback deliberately reused an existing immutable image rather than rebuilding an old commit.

Rebuilding during recovery would create a new artifact from historical source and could introduce differences from the artifact that was originally tested.

The safer model exercised by the project is:

```text
select existing immutable artifact
        ↓
apply deployment configuration
        ↓
deploy
        ↓
verify again
```

## Known-good still needs context

Sprint 02 showed that even a previously verified image is not automatically safe under every current runtime configuration.

Application artifact and runtime configuration can evolve independently. A rollback can therefore select a known-good image that is incompatible with the current configuration contract.

The project added runtime-configuration identity to the release/rollback model and tested incompatible rollback behavior.

This does not mean configuration must always be rolled back together with an image. It means compatibility must be part of the recovery reasoning.

## Rollback eligibility is not rollback success

Machine-verifiable eligibility can reject candidates that lack the required release evidence or no longer match the expected artifact identity.

It still does not prove that a rollback will work now.

The selected release must be deployed and verified again because environment state and dependencies may have changed since the original successful deployment.

## Delivery concurrency

Deployment and rollback both mutate the same runtime.

Sprint 01 serialized those operations with one GitHub Actions concurrency group and did not cancel an active deployment in favor of a newer one.

The broader concept is that concurrency control is part of delivery correctness when multiple automation paths mutate the same environment.

## What to remember

- A source identifier is not automatically an immutable artifact.
- Build once means later stages consume the exact artifact that earlier stages produced.
- Published does not mean deployed.
- Deployed does not mean verified.
- A known image is not automatically a known-good release.
- Rollback should normally select an existing artifact rather than recreate history.
- A previously verified release can still be incompatible with current runtime configuration.
- Rollback eligibility and successful rollback are different claims.
- Mutating the same environment concurrently creates a correctness problem, not merely a CI inconvenience.

## Zero-to-Prod deep dives

- [Immutable ECR publication](../sprint-01/ecr-immutable-push.md)
- [Deployment safety and rollback](../sprint-01/deployment-safety-rollback.md)
- [Verified deployment records](../sprint-02/verified-deployment-records.md)
- [Rollback eligibility](../sprint-02/rollback-eligibility.md)
- [Runtime configuration and rollback compatibility](../sprint-02/runtime-config-rollback-compatibility.md)
