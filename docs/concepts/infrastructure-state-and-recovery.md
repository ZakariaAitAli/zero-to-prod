# Infrastructure state and recovery

Terraform state is not merely a local implementation file. It is part of the recovery model for infrastructure managed by Terraform.

Sprint 01 exposed this when temporary verification infrastructure used runner-local state. Normal cleanup worked, but a hard runner loss could leave infrastructure behind without giving a fresh runner the state needed to manage it.

Sprint 02 addressed that limitation directly.

## What state represents

Terraform needs a durable relationship between configuration and the real objects it manages.

Conceptually:

```text
configuration
     +
state
     +
provider observations
     ↓
planned infrastructure change
```

Losing or selecting the wrong state can change what Terraform believes it owns.

That makes state an operational asset.

## Remote state changes the recovery boundary

Sprint 02 moved the relevant state to a versioned S3 backend.

The important property was not "S3 is better than a local file." It was that state survived the lifecycle of an individual CI runner.

That enabled a different execution to initialize the backend, recover the same ownership information, and clean up infrastructure created by an interrupted run.

## Backend bootstrap is separate

A remote backend has to exist before Terraform can use it to store the state for other infrastructure.

That creates a bootstrap boundary.

The project treated backend infrastructure separately instead of pretending the main Terraform state could create the storage it already depended on.

This is a general infrastructure problem: some foundational control-plane resources have a lifecycle outside the state that consumes them.

## Locking protects concurrent mutation

Remote durability alone does not prevent two Terraform processes from attempting to mutate the same state at the same time.

Sprint 02 exercised S3-native state locking and the permissions required for the lock lifecycle.

A lock protects Terraform state mutation.

It does **not** replace every other concurrency control. GitHub deployment concurrency and Terraform state locking protect different boundaries:

- workflow concurrency serializes delivery operations;
- Terraform locking protects concurrent Terraform state mutation.

## Wrong state can succeed

One of the more dangerous lessons from the experiments was that an incorrect state key is not guaranteed to fail loudly.

A valid but wrong key can initialize successfully and present an empty or different ownership view.

This is more dangerous than some obvious failures because the command can appear operationally successful while targeting the wrong state boundary.

Backend identity therefore deserves the same care as other deployment configuration.

## Fresh-runner recovery

Sprint 02 deliberately interrupted cleanup after Terraform had created verification infrastructure.

The subsequent recovery did not depend on the original runner's filesystem.

The fresh execution:

```text
checked out the repository
      ↓
initialized the remote backend
      ↓
recovered Terraform's ownership view
      ↓
planned destruction
      ↓
destroyed the orphaned infrastructure
      ↓
verified the external baseline
```

This turned remote state from a storage feature into demonstrated recovery capability.

## State recovery is not cloud discovery

Terraform state and cloud-provider discovery answer different questions.

Cloud discovery can show that a resource exists.

State records Terraform's ownership relationship and metadata for the managed object.

A recovery procedure should not casually substitute one for the other.

## Versioning and recovery administration

Versioning the remote state object adds another recovery layer when state itself is damaged or incorrectly changed.

Sprint 02 also kept recovery-administration capabilities distinct from normal CI permissions. Routine automation does not need every permission an operator might need during exceptional state recovery.

## Ownership boundaries matter

Sprint 02 also moved retained development runtime infrastructure under Terraform ownership deliberately rather than allowing overlapping tools to believe they controlled the same resources.

Infrastructure as Code is easier to recover when ownership is explicit:

```text
Who creates this?
Who changes it?
Where is its state?
Who may mutate that state?
How is it recovered after interruption?
```

## What to remember

- Terraform state is part of infrastructure recovery architecture.
- Remote state should survive the executor that created the infrastructure.
- Backend bootstrap is a separate lifecycle problem.
- State locking and deployment concurrency protect different things.
- A wrong state key can be a dangerous successful operation.
- Fresh-runner recovery is a stronger property than same-run cleanup.
- State is not interchangeable with provider discovery.
- Normal automation does not necessarily need state-recovery administration permissions.
- Explicit ownership reduces conflicting mutation paths.

## Zero-to-Prod deep dives

- [Remote Terraform state](../sprint-02/remote-terraform-state.md)
- [Terraform state locking](../sprint-02/terraform-state-locking.md)
- [Fresh-runner recovery](../sprint-02/fresh-runner-recovery.md)
- [Development runtime Terraform ownership](../sprint-02/development-runtime-terraform-ownership.md)
