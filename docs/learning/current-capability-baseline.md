# Zero-to-Prod v2 — Current Capability Baseline

## Purpose

This document records the current evidence-backed learning baseline for Zero-to-Prod through Issue #103 in Sprint 03.

It applies the learning model defined in:

`docs/rebaseline/zero-to-prod-v2-specification.md`

The purpose is not to mark technologies as "learned."

The purpose is to identify specific engineering capabilities, the strongest level currently supported by evidence, and the important unknowns that remain.

This baseline is intended to guide subsequent engineering work.

It is not a certification matrix, résumé checklist, or claim of production readiness.

---

## Learning levels

| Level | Meaning |
| --- | --- |
| L0 — Uncovered | No meaningful hands-on exposure |
| L1 — Exposed | Encountered the concept |
| L2 — Explain | Can explain the problem, purpose, and mechanics |
| L3 — Implement | Can build a working implementation |
| L4 — Experiment | Has deliberately tested or broken it and understands important failure behavior |
| L5 — Compare & Decide | Can compare credible alternatives and defend an architectural choice |
| L6 — Operate | Has sustained operational experience with upgrades, incidents, maintenance, failure, and recovery |

A capability is not promoted merely because its implementation works.

L5 requires meaningful comparison and decision evidence.

L6 requires sustained operational experience and cannot be earned through a short experiment.

---

## Current evidence-backed capabilities

| Domain / capability | Current level | Evidence | Why this level | Important remaining unknowns |
| --- | --- | --- | --- | --- |
| CI/CD — build once and preserve artifact identity across jobs | L4 | `docs/sprint-01/reflection.md`, `docs/sprint-01/demonstration.md` | The pipeline builds once, transfers the artifact between jobs, publishes an immutable full-SHA image, and deliberately tested ECR tag immutability. | No comparison with other artifact-transfer or registry models; no broader release-platform comparison. |
| Software supply chain — immutable container artifact identity | L4 | `docs/sprint-01/reflection.md`, `docs/sprint-01/demonstration.md` | Full Git SHA identity, build-once behavior, immutable ECR tags, and overwrite failure were tested. | Image signing, provenance attestations, SBOM generation, vulnerability policy, and alternative registry models remain largely uncovered. |
| Cloud identity — GitHub Actions federated AWS authentication | L4 | `docs/sprint-01/reflection.md` | OIDC-based temporary credentials were implemented and both trust and authorization failure boundaries were encountered and diagnosed. | No cross-provider workload-identity comparison; limited organization-level IAM/SCP experience in this project. |
| IAM — least-privilege deployment authorization | L4 | `docs/sprint-01/reflection.md`, `docs/sprint-02/development-runtime-terraform-ownership.md` | Permissions were narrowed around concrete actions, policy behavior was simulated, denied behavior was preserved where appropriate, and runtime-state access is read-only. | Broader IAM governance, permission boundaries, SCPs, delegated administration, and multi-account design remain outside current evidence. |
| Release delivery — deploy immutable image to ECS Fargate | L4 | `docs/sprint-01/demonstration.md` | Normal deployment, controlled verification failure, recovery, cleanup, and subsequent successful deployment were exercised. | No comparison with VM, serverless, Kubernetes, or other container-orchestration delivery models. |
| Deployment verification — external health and exact version verification | L4 | `docs/sprint-01/demonstration.md`, `docs/sprint-02/development-runtime-terraform-ownership.md` | A healthy but intentionally wrong version was rejected. Current deployment also verifies health, readiness, and exact version externally. | No production traffic, TLS, private-ingress, multi-service, or multi-region verification model has been tested. |
| Application lifecycle — liveness and readiness semantics | L4 | `docs/sprint-02/development-runtime-terraform-ownership.md`, `docs/guides/local-demo-api.md`, `docs/guides/local-postgresql.md` | `/health` and `/ready` have separate lifecycle semantics; readiness now includes bounded verification of the critical PostgreSQL persistence contract. Database outage, missing-schema behavior, recovery without API restart, and graceful shutdown have been exercised. | Multiple critical dependencies, partial degradation, long-running requests, multi-instance rollout behavior, and cloud-hosted stateful readiness remain untested. |
| Local development — application and PostgreSQL integration loop | L4 | `docs/guides/local-demo-api.md`, `docs/guides/local-postgresql.md`, `tools/demo-api-local`, `tools/postgres-local` | The local workflow now covers native tests/builds, explicit migrations, a real PostgreSQL runtime dependency, persistence, dependency-aware readiness, container packaging, and deliberate dependency failure/recovery experiments without AWS infrastructure. | It remains a single-machine lab with one datastore; concurrency, load, network impairment beyond simple outage, and provider-managed database behavior remain untested. |
| Relational datastore — persistent application state and runtime access | L4 | `docs/guides/local-postgresql.md`, `apps/demo-api/migrations/`, `apps/demo-api/cmd/api/store.go` | Work Items persist in PostgreSQL across API and database restarts. Runtime access uses a dedicated least-privilege identity, and both allowed and denied operations have been exercised. | Connection-pool tuning under load, transaction design, indexing strategy, data growth, high availability, and managed-database operations remain untested. |
| Database migrations — explicit schema and privilege lifecycle | L4 | `docs/guides/local-postgresql.md`, `apps/demo-api/migrations/`, `tools/postgres-local`, `docs/sprint-03/postgresql-schema-evolution.md` | Version-controlled migrations are applied explicitly with a separate migrator identity. Fresh recreation, idempotent re-application, unavailable-database failure, startup before migration, runtime privilege separation, deliberate dirty-state creation, reviewed metadata recovery, and resumed normal migration operation have been exercised. | Concurrent migration execution, non-transactional partial migration recovery, production lock behavior, automated recovery, and managed-database migration behavior remain untested. |
| Database schema evolution — application/schema compatibility | L4 | `docs/sprint-03/postgresql-schema-evolution.md`, `docs/guides/local-postgresql.md`, `apps/demo-api/migrations/000003_add_work_item_status.up.sql` | An additive Schema A→AB change was implemented and exercised with preserved old and new application binaries. Old-app/new-schema compatibility, new-app/new-schema compatibility, fail-closed new-app/old-schema behavior, application rollback while retaining the expanded schema, least-privilege preservation, and current-version backup/restore regression were tested. | Destructive Schema B contraction, concurrent mixed-version traffic, production zero-downtime rollout, lock duration under load, arbitrary cross-version compatibility, and comparison of alternative evolution strategies remain untested; no L5 claim is justified. |
| Dependency resilience — PostgreSQL outage and recovery | L4 | `docs/guides/local-demo-api.md`, `docs/guides/local-postgresql.md` | PostgreSQL was deliberately stopped before and during API runtime. Liveness remained healthy, readiness failed closed, persistence operations returned bounded errors, failed writes were not reported as successful, and the same API process recovered when PostgreSQL returned. | Timeout tuning under slow rather than failed dependencies, retry/backoff policy, circuit breaking, multiple dependencies, and sustained fault operation remain untested. |
| Rollback — reuse a known immutable application artifact | L4 | `docs/sprint-01/demonstration.md`, `docs/sprint-01/reflection.md` | A controlled failure was followed by successful rollback using an already-published immutable image, with measured recovery time and external verification. | No automatic rollback, traffic-shifting strategy, database rollback, or historical full-runtime restoration. |
| Rollback eligibility — fail closed on unverified historical releases | L4 | `docs/sprint-02/rollback-eligibility.md`, `docs/sprint-02/verified-deployment-records.md` | Eligibility is machine-verified before runtime mutation and rejects releases without the required immutable evidence. | External mutable dependencies can still invalidate an otherwise eligible release. |
| Release identity — runtime configuration compatibility | L4 | `docs/sprint-02/runtime-config-rollback-compatibility.md` | A real incompatibility between historical image and changed runtime configuration was reproduced, then converted into `runtime_config_digest` fail-closed behavior. | Secret values behind unchanged references, database schema/content, and external API contracts are not represented by this identity. |
| Release evidence — durable verified deployment records | L4 | `docs/sprint-02/verified-deployment-records.md`, `docs/sprint-02/development-runtime-terraform-ownership.md` | Verified deployments produce durable machine-readable S3 records containing artifact and runtime identity, and their stored digest was independently checked against ECR. | Evidence retention policy, independent signing/attestation, and evidence portability beyond the current AWS implementation remain open. |
| Terraform — remote state and locking | L4 | `docs/sprint-02/remote-terraform-state.md`, `docs/sprint-02/terraform-state-locking.md` | Durable remote state and locking were implemented and exercised as part of real deployment/recovery workflows. | Terraform state architecture across multiple environments/teams and alternative state-management models have not been compared. |
| Terraform — fresh-runner recovery | L4 | `docs/sprint-02/fresh-runner-recovery.md` | A runner was deliberately interrupted after creating infrastructure; a fresh runner reconnected to remote state, reviewed a bounded destroy plan, and recovered the environment. | Recovery from corrupt state, unavailable backend, partial provider failure, or concurrent human changes remains unproven. |
| Terraform — brownfield resource adoption | L4 | `docs/sprint-02/development-runtime-terraform-ownership.md` | Thirteen existing runtime resources were modeled and imported with 0 add / 0 change / 0 destroy, followed by a stable zero-change plan and later deliberate in-place change. | Terraform refactoring, moved blocks, module extraction, large-scale import, and sustained drift-management workflows remain shallow or uncovered. |
| Terraform — ownership boundaries between infrastructure and deployment | L4 | `docs/sprint-02/development-runtime-terraform-ownership.md` | Long-lived runtime structure, deployment-owned task-definition/desired-count mutation, and temporary verification infrastructure have explicit separate ownership models that were tested together. | More complex shared-resource ownership and multi-environment/module boundaries remain untested. |
| Observability — application logging for deployment diagnosis | L4 | `docs/sprint-02/ecs-cloudwatch-logging.md`, `docs/sprint-02/failed-deployment-diagnostics.md` | Application logs and ECS evidence were correlated during controlled deployment failures and diagnostics were designed to remain bounded and cleanup-safe. | Metrics, traces, SLO-driven signals, dashboards, alerting, cardinality management, and broader observability architecture remain largely uncovered. |
| Incident/recovery practice — deployment failure diagnosis and cleanup | L4 | `docs/sprint-02/failed-deployment-diagnostics.md`, `docs/sprint-02/fresh-runner-recovery.md` | Multiple failure layers were investigated and recovery was performed from a fresh runner rather than relying on the original execution context. | This is experiment-based recovery, not sustained incident-response operation; no L6 claim is justified. |
| Backup and restore — local PostgreSQL application data recovery | L4 | `docs/sprint-03/postgresql-backup-restore.md`, `docs/sprint-03/runbook.md`, `docs/guides/local-postgresql.md`, `tools/postgres-backup-local` | A repository-owned data-only logical backup was implemented, PostgreSQL data-volume loss was exercised destructively, recovery was completed through explicit migrations plus restore, recovered rows were verified directly and through the API, sequence continuity was tested, and missing, corrupt, wrong-scope, pre-migration, and non-empty-target failures were deliberately exercised. Issue #101 also regression-tested current-version Schema AB backup/restore with both `pending` and `done` status values plus sequence continuity. | No PITR/WAL recovery, physical backup, scheduled retention, large-dataset recovery, managed PostgreSQL recovery, cross-region recovery, or representative recovery-architecture comparison has been tested; no L5 claim is justified. |
| Messaging / asynchronous processing | L4 | `docs/sprint-03/reliable-async-processing.md` | A durable PostgreSQL acceptance boundary, transactional outbox, confirmed RabbitMQ publication, manual-ACK worker, bounded retries, terminal failure, and idempotent completion were implemented. Pre-commit failure, post-commit/pre-publication recovery, broker outage, duplicate/redelivered delivery, retry exhaustion, API/worker restart, and graceful in-flight shutdown were deliberately exercised. | Exactly-once delivery/publication is not claimed. Dead-letter handling, long-term replay tooling, ordering guarantees, horizontal worker scaling, production broker operations, broker HA, and representative messaging-architecture comparison remain untested; no L5 claim is justified. |
| Distributed-system behavior — partial failure and recovery across API, broker, worker, and datastore | L4 | `docs/sprint-03/reliable-async-processing.md` | Failure behavior was deliberately exercised across independent API, RabbitMQ, worker, and PostgreSQL boundaries, including dependency outage, process restart, redelivery ambiguity, post-effect/pre-ACK recovery, durable recovery without caller retry, and explicit distinction between liveness, durable acceptance, publication, and processing outcome. | Network partitions and delay rather than simple outage, cross-job ordering, concurrent workers, load/backpressure, multi-node broker behavior, distributed tracing, and broader consistency-model comparison remain untested; no L5 claim is justified. |
| FinOps — cost-aware temporary cloud experiments | L3 | `docs/sprint-02/reflection.md`, `docs/sprint-02/development-runtime-terraform-ownership.md` | Actual AWS spend was inspected, expensive resources were identified, runtime is returned to zero, and the project deliberately avoids NAT and always-on ALB/Fargate resources. | No mature unit-cost model, automated attribution, forecasting practice, or architecture comparison based on measured cost. |

---

## Important uncovered or shallow capabilities

The strongest earlier evidence was concentrated around delivery, rollback, Terraform state, AWS IAM, deployment evidence, and PostgreSQL recovery. Issue #103 materially broadened the system into reliable local asynchronous processing and distributed failure behavior.

The following areas remain intentionally shallow and should influence future work.

| Capability | Current level | Current limitation |
| --- | --- | --- |
| Cache architecture | L0 | No cache exists in the reference system. |
| Application metrics | L0 | No meaningful application metrics capability is implemented. |
| Distributed tracing | L0 | No tracing implementation or experiment exists. |
| SLOs / error budgets | L0 | Concepts may have been encountered, but no evidence-backed implementation exists. |
| Performance/load testing | L0 | No durable application performance experiment exists. |
| Kubernetes/orchestration comparison | L0 | Kubernetes has not been implemented as a Zero-to-Prod capability and should not be introduced without a problem that justifies it. |
| Terraform modules/refactoring/testing | L1 | State and ownership mechanics are much stronger than reusable Terraform design/testing evidence. |
| Multi-environment infrastructure modelling | L1 | Current evidence centers on one development sandbox. |
| Application security depth | L1 | IAM and delivery security are stronger than application-security testing. |
| Software supply-chain attestations/signing | L0 | Immutable artifacts exist, but signing and provenance standards are not yet implemented. |
| Multi-cloud comparison | L0 | AWS is the only deeply exercised provider in the repository. |
| VM/serverless compute comparison | L0 | No representative alternatives have been implemented and measured. |
| Platform engineering / self-service | L1 | Existing scripts and workflows are project tooling, not yet an evaluated developer platform. |

These are gaps, not an instruction to implement all of them.

Future work should continue to select coherent engineering problems that advance several related capabilities at once.

---

## Why most strong capabilities stop at L4

The completed work contains substantial implementation and failure experimentation.

Examples include:

- immutable-tag overwrite rejection;
- wrong-version deployment rejection;
- controlled rollback;
- incompatible runtime-configuration rollback;
- hard runner interruption;
- fresh-runner Terraform recovery;
- failed-deployment diagnostics;
- brownfield Terraform import;
- IAM allow/deny verification;
- external health/readiness/version verification.

That supports L4 for those specific capabilities.

It does **not** automatically support L5.

L5 requires comparison of credible alternatives and an evidence-backed decision about when one approach fits better than another.

For example:

`ECS Fargate deployment = L4`

does not imply:

`container orchestration = L5`

because representative alternatives have not yet been implemented and compared.

Likewise:

`AWS OIDC federation = L4`

does not imply:

`workload identity = L5`

because provider and architecture alternatives have not yet been meaningfully compared.

---

## L6 status

No capability is currently marked L6.

Sprint 01 and Sprint 02 contain strong controlled experiments, but they do not represent sustained operation over enough time and operational variety to justify L6.

Possible long-term L6 candidates remain those described by the v2 specification, such as:

- Linux;
- containers;
- one primary orchestrator;
- one relational datastore;
- observability;
- CI/CD;
- incident/SRE practice;
- one primary cloud provider;
- the persistent Zero-to-Prod platform itself.

Those levels must be earned through sustained operation rather than assigned in advance.

---

## How to use this baseline

Before selecting future work:

1. identify the engineering problem;
2. identify which capabilities it would advance;
3. record their current levels;
4. state the expected post-experiment levels;
5. identify the evidence required to justify that change;
6. reject work that provides little capability gain and little learning gain.

The baseline should be revised only when new evidence materially changes a capability level or reveals that an existing level was overstated.

It should remain smaller than the complete 22-domain taxonomy.

Its job is to guide engineering decisions, not inventory every technology encountered by the project.

---

## Immediate implication for subsequent work

The current system is strongest around:

- immutable delivery;
- deployment verification;
- rollback safety;
- Terraform state and recovery;
- AWS IAM;
- release evidence;
- PostgreSQL persistence and recovery;
- reliable local asynchronous processing;
- distributed partial-failure recovery.

The asynchronous path is now evidence-backed at L4, so future work should not add messaging complexity merely to increase technology coverage.

The highest-leverage gaps remain capabilities that are still shallow or uncovered, selected only when they support a coherent engineering problem.

The v2 specification remains authoritative for future capability selection.
