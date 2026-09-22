# Zero-to-Prod v2 — Current Capability Baseline

## Purpose

This document records the current evidence-backed learning baseline for Zero-to-Prod after Sprint 01, Sprint 02, Issue #88, and the beginning of the v2 rebaseline implementation.

It applies the learning model defined in:

`docs/rebaseline/zero-to-prod-v2-specification.md`

The purpose is not to mark technologies as "learned."

The purpose is to identify specific engineering capabilities, the strongest level currently supported by evidence, and the important unknowns that remain.

This baseline is intended to guide Sprint 03 selection.

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
| Application lifecycle — liveness and readiness semantics | L4 | `docs/sprint-02/development-runtime-terraform-ownership.md`, `docs/guides/local-demo-api.md` | `/health` and `/ready` have separate lifecycle semantics; ECS container health remains liveness-based while load-balancer routing uses readiness. Local graceful shutdown behavior has also been exercised during Issue #92. | Dependency-aware readiness, startup dependencies, degraded states, long-running requests, and real stateful dependencies remain untested. |
| Local development — native demo API test/build/run/verify loop | L3 | `docs/guides/local-demo-api.md`, `tools/demo-api-local` | A cloud-independent native workflow exists for testing, building, running, and verifying the current application. | The local environment currently represents only one stateless service; there is no integration lab with real dependencies yet. |
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
| FinOps — cost-aware temporary cloud experiments | L3 | `docs/sprint-02/reflection.md`, `docs/sprint-02/development-runtime-terraform-ownership.md` | Actual AWS spend was inspected, expensive resources were identified, runtime is returned to zero, and the project deliberately avoids NAT and always-on ALB/Fargate resources. | No mature unit-cost model, automated attribution, forecasting practice, or architecture comparison based on measured cost. |

---

## Important uncovered or shallow capabilities

The strongest Sprint 01 and Sprint 02 evidence is concentrated around delivery, rollback, Terraform state, AWS IAM, and deployment evidence.

The following areas remain intentionally shallow and should influence future work.

| Capability | Current level | Current limitation |
| --- | --- | --- |
| Relational datastore operation | L0 | No real application datastore in the reference system. |
| Database migrations | L0 | No migration lifecycle has been implemented or broken. |
| Backup and restore of application data | L0 | No stateful reference workload exists yet. |
| Cache architecture | L0 | No cache exists in the reference system. |
| Messaging / asynchronous processing | L0 | No broker, queue, or worker path exists. |
| Distributed-system behavior | L0 | The implemented application remains a single small stateless service. |
| Application metrics | L0 | No meaningful application metrics capability is implemented. |
| Distributed tracing | L0 | No tracing implementation or experiment exists. |
| SLOs / error budgets | L0 | Concepts may have been encountered, but no evidence-backed implementation exists. |
| Performance/load testing | L0 | No durable application performance experiment exists. |
| Resilience under dependency failure | L0 | The demo API currently has no meaningful runtime dependency to fail. |
| Kubernetes/orchestration comparison | L0 | Kubernetes has not been implemented as a Zero-to-Prod capability and should not be introduced without a problem that justifies it. |
| Terraform modules/refactoring/testing | L1 | State and ownership mechanics are much stronger than reusable Terraform design/testing evidence. |
| Multi-environment infrastructure modelling | L1 | Current evidence centers on one development sandbox. |
| Application security depth | L1 | IAM and delivery security are stronger than application-security testing. |
| Software supply-chain attestations/signing | L0 | Immutable artifacts exist, but signing and provenance standards are not yet implemented. |
| Multi-cloud comparison | L0 | AWS is the only deeply exercised provider in the repository. |
| VM/serverless compute comparison | L0 | No representative alternatives have been implemented and measured. |
| Platform engineering / self-service | L1 | Existing scripts and workflows are project tooling, not yet an evaluated developer platform. |

These are gaps, not an instruction to implement all of them.

Sprint 03 should select a coherent engineering problem that advances several related capabilities at once.

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

## Immediate implication for Sprint 03

The current system is strongest around:

- immutable delivery;
- deployment verification;
- rollback safety;
- Terraform state and recovery;
- AWS IAM;
- release evidence.

The highest-leverage gaps are now outside that narrow release path.

Sprint 03 should therefore prefer a problem that broadens the engineering surface of the reference system rather than adding another layer of delivery mechanics.

The v2 specification remains authoritative for the final Sprint 03 decision.
