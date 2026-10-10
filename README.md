# Zero-to-Prod

Zero-to-Prod is an evidence-driven DevOps and Cloud Engineering learning laboratory for understanding how real software systems are designed, built, secured, tested, deployed, observed, operated, recovered, evolved, compared, and eventually retired.

The project is deliberately technology- and provider-neutral.

Technologies are introduced because an engineering problem justifies them, not because the project needs to collect tools.

Zero-to-Prod develops production-oriented engineering skills and reference implementations, but it does **not** claim production readiness without evidence.

The long-term mission, engineering principles, learning model, environment strategy, and capability roadmap are defined in the [Zero-to-Prod v2 rebaseline specification](docs/rebaseline/zero-to-prod-v2-specification.md).

## Engineering approach

Zero-to-Prod follows a problem-driven learning cycle:

```text
understand the problem
        ↓
identify constraints
        ↓
identify viable architectures
        ↓
examine credible alternatives
        ↓
implement representative solutions
        ↓
break and measure them
        ↓
compare trade-offs
        ↓
make a defensible decision
        ↓
operate the chosen system
        ↓
recover, evolve, migrate, or retire it
```

Core principles include:

- problem before technology;
- concepts before providers;
- alternatives before decisions;
- evidence before claims;
- failure as part of architecture;
- measurement before optimization;
- local-first experimentation where behavior is portable;
- real providers where provider semantics are part of the lesson;
- security, observability, operations, and cost as cross-cutting concerns;
- explicit lifecycle decisions for experimental scaffolding.

## Learning model

Capabilities are tracked using evidence-backed learning levels rather than marking entire technologies as "learned."

| Level | Meaning |
| --- | --- |
| L0 — Uncovered | No meaningful hands-on exposure |
| L1 — Exposed | Encountered the concept |
| L2 — Explain | Can explain the problem, purpose, and mechanics |
| L3 — Implement | Can build a working implementation |
| L4 — Experiment | Has deliberately tested or broken it and understands important failure behavior |
| L5 — Compare & Decide | Can compare credible alternatives and defend an architectural choice |
| L6 — Operate | Has sustained operational experience with upgrades, incidents, maintenance, failure, and recovery |

The current evidence-backed levels and remaining unknowns are maintained in the [current capability baseline](docs/learning/current-capability-baseline.md).

## Current reference system — Work Items

The current reference system is **Work Items**.

Its implementation lives under `apps/work-items`:

- `api/` — the HTTP API executable;
- `worker/` — the asynchronous worker executable;
- `web/` — the browser Web UI (React / Vite / TypeScript);
- `migrations/` — version-controlled PostgreSQL migrations.

The current system contains two backend executable processes:

- an HTTP API;
- an asynchronous worker.

A separate browser Web UI calls the API. See [ADR 0002](docs/adr/0002-work-items-web-ui-boundary.md).

It currently uses PostgreSQL for durable state and RabbitMQ for message delivery.

```text
Web UI or other client
  │
  ▼
Work Items API
  │
  ├──────────────► PostgreSQL
  │                 ├─ Work Items
  │                 ├─ Processing Jobs
  │                 ├─ Transactional Outbox
  │                 └─ Title-analysis results
  │
  │                 durable acceptance
  │                       │
  │                       ▼
  └──── API-side outbox publisher
                          │
                          ▼
                       RabbitMQ
                          │
                          ▼
                        Worker
                          │
                          ▼
                      PostgreSQL
            (result + done item + succeeded job
                  in one transaction)
```

The API currently exposes health, readiness, version, Work Item, and processing operations.

The asynchronous path uses a transactional outbox so acceptance of processing responsibility is committed durably in PostgreSQL before publication to RabbitMQ.

The worker consumes with manual acknowledgement, bounded retry behavior, terminal failure handling, and guarded durable state transitions.

Successful processing commits a title-analysis result, changes the Work Item from `pending` to `done`, and marks the job `succeeded` in one PostgreSQL transaction before the worker acknowledges the message. The state rules are in the [Work Items architecture](docs/architecture/work-items.md#state-semantics).

The design currently provides **at-least-once processing with idempotent effects**. Exactly-once delivery or publication is not claimed.

## Current implemented capabilities

Sprint 03 has expanded the reference system into a local-first stateful application backbone.

Evidence-backed capabilities currently include:

- persistent Work Items in PostgreSQL;
- explicit version-controlled database migrations;
- separate database administration, migration, application, and worker identities;
- least-privilege runtime database access;
- dependency-aware readiness;
- additive schema evolution and application/schema compatibility experiments;
- logical backup and destructive recovery of the persisted accepted-work and result state (`work_items`, `processing_jobs`, `outbox_messages`, `work_item_results`);
- a durable title-analysis result committed atomically with the Work Item and job state transitions;
- transactional creation of processing jobs and outbox messages;
- confirmed RabbitMQ publication;
- separate RabbitMQ publisher and worker identities;
- manual-ACK worker consumption;
- bounded processing retries;
- terminal processing failure;
- idempotent processing effects;
- recovery across API, broker, worker, and datastore failures;
- restart and redelivery experiments;
- crash-consistency experiments with real process crashes, uncertain commits, and concurrent delivery ([experiment](docs/experiments/issue-118-crash-consistency.md));
- graceful in-flight worker shutdown behavior;
- a browser Web UI for listing and creating Work Items, submitting processing, and polling job state (implementation level, exercised manually; lint and build in CI).

Important current limitations include:

- no exactly-once guarantee;
- no broker high-availability experiment;
- no horizontal worker-scaling experiment;
- no distributed tracing;
- no representative messaging-architecture comparison;
- no sustained-operation L6 claim;
- snapshot recovery remains limited to the captured recovery point;
- restoring PostgreSQL does not restore RabbitMQ: a job whose message was already delivered can restore blocked ([#130](https://github.com/ZakariaAitAli/zero-to-prod/issues/130)), and a stale queued message can target reused IDs ([#131](https://github.com/ZakariaAitAli/zero-to-prod/issues/131)); both are unmitigated;
- no automated frontend behavioral tests.

These limitations are experiment boundaries rather than hidden production-readiness assumptions.

## Local development

The current implementation is local-first.

PostgreSQL and RabbitMQ are real local dependencies rather than cloud-service emulations.

### PostgreSQL

Start PostgreSQL and apply migrations explicitly:

```bash
./tools/postgres-local start
./tools/postgres-local build-migrate
./tools/postgres-local migrate-up
```

Useful PostgreSQL operations include:

```bash
./tools/postgres-local status
./tools/postgres-local migrate-version
./tools/postgres-local roles
./tools/postgres-local schema
```

Schema migrations are an explicit lifecycle operation. Application startup does not automatically migrate the database.

See the [local PostgreSQL guide](docs/guides/local-postgresql.md).

### RabbitMQ

Start RabbitMQ and ensure the repository-defined runtime topology:

```bash
./tools/rabbitmq-local start
```

Inspect the local broker with:

```bash
./tools/rabbitmq-local status
./tools/rabbitmq-local diagnostics
```

The local topology uses separate publisher and worker identities with different permissions.

### API

Run the API:

```bash
./tools/work-items-api-local run
```

In another terminal, verify its basic runtime contract:

```bash
./tools/work-items-api-local verify
```

The helper also provides:

```bash
./tools/work-items-api-local test
./tools/work-items-api-local build
```

### Worker

Run the asynchronous worker:

```bash
./tools/work-items-worker-local run
```

The helper also provides:

```bash
./tools/work-items-worker-local test
./tools/work-items-worker-local build
```

### Security lab (opt-in)

Issue #121 adds a separate security lab: Work Items behind one HTTPS ingress on
host loopback, with private backend services, a private lab CA, and its own
runtime secrets. It is intermediate and has no authentication yet. See the
[security lab guide](docs/guides/local-security-lab.md).

### Backup and recovery

The persisted accepted-work state can be backed up using repository-owned tooling:

```bash
./tools/postgres-backup-local create <backup-path>
./tools/postgres-backup-local inspect <backup-path>
./tools/postgres-backup-local validate <backup-path>
./tools/postgres-backup-local restore <backup-path>
```

The current backup is a data-only logical archive of:

- `work_items`, `processing_jobs`, `outbox_messages`, and `work_item_results` table data;
- sequence state for the three identity-bearing tables.

Migration 5 requires an empty application database; pre-result backups require their historical schema/application.

Recovery is migration-first: migrations must already be applied, and all four tables must exist and be empty before restore.

`validate` checks archive scope and that the full archive payload is readable. It does not prove successful recovery.

The original accepted-work boundary was defined by [ADR 0001](docs/adr/0001-work-items-async-recovery-boundary.md) after the [Issue #111 recovery experiment](docs/experiments/issue-111-recovery-model.md) showed that restoring `work_items` alone lost accepted asynchronous work.

PostgreSQL restore recovers persisted accepted-work and result state. It does **not** restore RabbitMQ, and a successful restore does not by itself make resuming safe: a job whose message was already delivered can remain blocked ([#130](https://github.com/ZakariaAitAli/zero-to-prod/issues/130)), and a stale queued message can target IDs that new work reuses ([#131](https://github.com/ZakariaAitAli/zero-to-prod/issues/131)). No reconciliation or redrive exists yet.

ADR 0003 adds the result to that boundary. See the [current recovery runbook](docs/runbooks/work-items-recovery.md). The original Work-Item-only design is preserved in the [Issue #99 backup/restore experiment](docs/sprint-03/postgresql-backup-restore.md).

## CI

GitHub Actions (`.github/workflows/work-items-ci.yml`) provides change-aware validation of the local-first system. A `Detect changes` job classifies the changed paths, three validation jobs run when their paths change, and `CI required` fails unless every required job succeeded.

**Validate Work Items backend**

- Go formatting, `go vet` (including a compile check of the opt-in crash-experiment harness), and dependency-free Go tests;
- PostgreSQL and RabbitMQ integration setup with explicit migrations;
- PostgreSQL archive scope and payload validation;
- API and worker integration tests, failing on any skipped test;
- result migration and backup/restore compatibility (`scripts/test-async-result-recovery.sh`);
- API and worker container image builds, requiring the worker image to run as its unprivileged user.

**Validate Work Items frontend**

- `pnpm install --frozen-lockfile`, lint, and production build.

**Validate CI workflows**

- shell syntax checks for the CI scripts, local tools, and security lab tooling;
- Compose model validation for the development lab and the security lab;
- CI path-classifier, required-gate, workflow, backup-validation, and security lab state-safety regression tests;
- `actionlint`.

CI does not start the security lab or build its ingress image. It requires no AWS credentials and performs no cloud deployment.

## Historical capability layers

Zero-to-Prod preserves completed experiments as historical evidence even after their implementation is no longer part of the active system.

### Sprint 01

Sprint 01 explored an AWS-based immutable application delivery path, including:

- immutable container artifacts;
- GitHub Actions OIDC authentication;
- ECS Fargate deployment;
- external runtime verification;
- controlled deployment failure;
- operator-assisted rollback;
- cleanup.

Its documentation remains under [`docs/sprint-01`](docs/sprint-01).

### Sprint 02

Sprint 02 deepened the AWS delivery system through experiments involving:

- recoverable Terraform state;
- state locking;
- fresh-runner recovery;
- brownfield Terraform ownership;
- deployment diagnostics;
- durable verified deployment records;
- rollback eligibility;
- runtime-configuration compatibility;
- IAM boundaries;
- cloud cost controls.

Its documentation remains under [`docs/sprint-02`](docs/sprint-02), with durable historical evidence under [`evidence/sprint-02`](evidence/sprint-02).

The pre-rebaseline AWS runtime, delivery infrastructure, IAM identity, container registry, Terraform state backends, and executable AWS/Terraform implementation have since been deliberately retired.

Their removal does not invalidate the capabilities learned through those experiments.

### Sprint 03

Sprint 03 moved the project into a local-first stateful-system backbone.

It covered:

- PostgreSQL persistence and migration foundations;
- runtime database privilege separation;
- backup and destructive recovery;
- additive schema evolution;
- application/schema compatibility;
- durable asynchronous processing;
- RabbitMQ delivery;
- a browser Web UI;
- durable business results and business/execution state separation;
- distributed partial-failure, crash-consistency, and recovery experiments.

Sprint 03's engineering work is complete. Work Items becomes a stable reference workload rather than a product under feature development; the [Sprint 03 summary](docs/sprint-03/README.md) records the outcome, the decision, and the open limitations.

## Current direction

Future work is selected from engineering problems and capability gaps rather than from a predetermined technology roadmap.

A new technology should answer questions such as:

```text
What problem are we solving?
What constraints exist?
What credible alternatives exist?
What would this technology teach us?
How does it fail?
How is it secured and observed?
What operational burden does it introduce?
What evidence would justify keeping it?
When would we choose differently?
```

Local environments are preferred where the important behavior is portable.

Real cloud providers are introduced when provider-specific behavior is itself part of the learning objective.

The project aims for broad **L5 — Compare & Decide** capability across important engineering domains and selected **L6 — Operate** depth earned through sustained operation.

## Repository map

```text
zero-to-prod/
├── apps/
│   └── work-items/     current Work Items implementation
├── infra/
│   ├── local/          local PostgreSQL and RabbitMQ infrastructure
│   └── security-lab/   opt-in HTTPS ingress security lab (Issue #121)
├── docs/
│   ├── README.md       documentation entry point and learning map
│   ├── concepts/       reusable engineering knowledge
│   ├── architecture/   Zero-to-Prod system structure and boundaries
│   ├── adr/            architectural decision records
│   ├── experiments/    focused experiment records
│   ├── guides/         implementation and local-operation guides
│   ├── learning/       evidence-backed capability baseline
│   ├── rebaseline/     v2 mission and engineering model
│   ├── reference/      durable reference information
│   ├── sprint-01/      historical Sprint 01 experiments
│   ├── sprint-02/      historical Sprint 02 experiments
│   └── sprint-03/      Sprint 03 summary and experiments
├── evidence/           durable experiment and retirement evidence
├── scripts/            repository and CI controls
├── tools/              local development and operational helpers
└── .github/            continuous integration
```

The repository structure evolves only when a demonstrated architectural boundary justifies the change.

## Documentation

Start with the [documentation map](docs/README.md).

It provides the learning-oriented path through reusable concepts, current architecture, guides, runbooks, the v2 project rules, and the detailed Sprint experiments.

Key project references:

- [Zero-to-Prod v2 rebaseline specification](docs/rebaseline/zero-to-prod-v2-specification.md) — authoritative mission, principles, learning model, environment strategy, and roadmap;
- [Current capability baseline](docs/learning/current-capability-baseline.md) — demonstrated learning levels and remaining gaps;
- [Local Work Items guide](docs/guides/local-work-items.md) — current application workflow;
- [Local PostgreSQL guide](docs/guides/local-postgresql.md) — datastore and migration lifecycle.

Historical Sprint documents remain intact as the detailed record of implementation, failures, corrections, and experiments. Reusable documentation is the faster path for revisiting what those experiments taught.
