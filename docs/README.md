# Zero-to-Prod documentation

This is the documentation entry point for Zero-to-Prod.

The repository keeps two complementary kinds of documentation:

1. **Reusable engineering knowledge** — concepts, current architecture, guides, runbooks, learning state, and reference material.
2. **Sprint records** — the detailed experiments, failures, corrections, and implementation history that produced that knowledge.

The Sprint records remain valuable, but they are not the first place to look when revisiting a concept.

## Start here

If you want to understand an engineering idea, start with **Concepts**.

If you want to understand how the current Zero-to-Prod system fits together, start with **Architecture**.

If you want to perform a workflow, use **Guides**.

If something has failed or you need to recover a system, use the relevant **Runbook**.

If you want to inspect the exact experiment that established a behavior, follow the links from the reusable documentation into the Sprint records.

## Concepts

Concept documents explain portable engineering ideas and connect them to the way Zero-to-Prod exercised those ideas.

- [Identity and access](concepts/identity-and-access.md) — authentication, authorization, workload identity, trust, permissions, and least privilege.
- [Artifacts and delivery](concepts/artifacts-and-delivery.md) — artifact identity, build-once delivery, immutability, verified releases, and rollback.
- [Health and deployment verification](concepts/health-and-deployment-verification.md) — liveness, readiness, control-plane stability, external verification, and diagnostics.
- [Infrastructure state and recovery](concepts/infrastructure-state-and-recovery.md) — Terraform state, remote backends, locking, ownership, and fresh-runner recovery.
- [Database lifecycle and recovery](concepts/database-lifecycle-and-recovery.md) — migrations, schema compatibility, backup, restore, and recovery boundaries.
- [Reliable asynchronous processing](concepts/reliable-asynchronous-processing.md) — durable acceptance, transactional outbox, delivery ambiguity, idempotency, retries, and terminal outcomes.

## Architecture

Architecture documents describe the current or retained Zero-to-Prod system boundaries rather than teaching a generic technology.

- [Delivery architecture](architecture/delivery.md) — the delivery and recovery model established by Sprint 01 and Sprint 02.
- [Work Items architecture](architecture/work-items.md) — the current local-first Work Items system using PostgreSQL and RabbitMQ.

## Guides

- [Local Work Items development](guides/local-work-items.md)
- [Local PostgreSQL and schema migrations](guides/local-postgresql.md)

Guides are procedural: they answer **how do I perform this workflow?**

## Runbooks

Current recovery: [Work Items result recovery](runbooks/work-items-recovery.md).

Historical recovery procedures and experiments remain in the Sprint runbooks:

- [Sprint 01 operations runbook](sprint-01/runbook.md)
- [Sprint 03 PostgreSQL backup and recovery runbook](sprint-03/runbook.md) — current recovery procedure for the `work_items`, `processing_jobs`, and `outbox_messages` boundary

These can be consolidated later when the operational boundary, rather than the Sprint boundary, makes that more useful.

## Architectural decisions (ADRs)

ADRs answer **why was this decision made?** They live in [`adr/`](adr/).

- [ADR 0001 — Work Items async recovery boundary](adr/0001-work-items-async-recovery-boundary.md) — why the PostgreSQL backup covers `work_items`, `processing_jobs`, and `outbox_messages`.
- [ADR 0002 — Work Items Web UI boundary](adr/0002-work-items-web-ui-boundary.md) — why the browser UI is a separate artifact and how it reaches the API locally.

## Experiments

Experiment records answer **what did we test and what happened?** Focused experiments since the documentation reorganization live in [`experiments/`](experiments/). Earlier experiments are in the Sprint records below.

- [Issue #111 — accepted async work recovery](experiments/issue-111-recovery-model.md) — destructive PostgreSQL loss and restore of accepted, unpublished processing work.
- [Issue #118 — crash consistency and recovery](experiments/issue-118-crash-consistency.md) — real process crashes at observed points, uncertain commits, duplicate and concurrent delivery, PostgreSQL outage, and snapshot recovery with stale broker messages.

Experiment records describe what was true when the experiment ran. They are not rewritten when the system changes later.

## Evidence

Evidence answers **what proves the claim?** It currently lives in three places:

- [`../evidence/`](../evidence/) — durable, sanitized artifacts (for example Sprint 02 verified-deployment records and AWS retirement evidence);
- the **Evidence** sections of ADRs and the observations recorded in experiment and Sprint documents;
- repository tests and CI checks, such as the Go integration tests and `scripts/test-postgres-backup-validation.sh`.

The [current capability baseline](learning/current-capability-baseline.md) links each claimed level to its evidence.

## Learning and project rules

- [Current capability baseline](learning/current-capability-baseline.md) — what has actually been demonstrated and the current learning depth.
- [Zero-to-Prod v2 specification](rebaseline/zero-to-prod-v2-specification.md) — authoritative mission, principles, learning model, and environment strategy.
- [v2 engineering work contract](reference/v2-engineering-work-contract.md) — how the rebaseline is applied without turning engineering work into documentation bureaucracy.

The v2 specification remains authoritative. This documentation organization does not replace or redefine it.

## Detailed Sprint records

### Sprint 01 — delivery foundation

Sprint 01 established the GitHub-to-AWS delivery path: OIDC authentication, immutable image publication, ECS Fargate deployment, external verification, cleanup, and rollback.

Browse [Sprint 01](sprint-01/).

### Sprint 02 — recovery and operational depth

Sprint 02 deepened the delivery system with change-aware CI, Terraform ownership, CloudWatch logging, deployment diagnostics, remote state, locking, fresh-runner recovery, verified deployment records, rollback eligibility, and runtime-configuration compatibility.

Browse [Sprint 02](sprint-02/).

### Sprint 03 — stateful and asynchronous systems

Sprint 03 moved the active reference system local-first and introduced PostgreSQL persistence, migrations, destructive recovery, schema evolution, transactional outbox processing, RabbitMQ, idempotent consumption, and distributed failure experiments.

Browse [Sprint 03](sprint-03/).

## How to use the layers

A useful reading path is:

```text
engineering question
        ↓
concept
        ↓
Zero-to-Prod architecture
        ↓
guide or runbook when needed
        ↓
Sprint experiment for the full implementation and failure details
```

The goal is not to hide the detailed experiments. It is to make them optional when all you need is to understand or recall the engineering idea.
