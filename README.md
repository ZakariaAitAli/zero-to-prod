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

Its implementation currently lives under `apps/demo-api`, a name inherited from the earlier project baseline. The directory name does not define the long-term application architecture.

The current system contains two executable processes:

- an HTTP API;
- an asynchronous worker.

It currently uses PostgreSQL for durable state and RabbitMQ for message delivery.

```text
Client
  │
  ▼
Work Items API
  │
  ├──────────────► PostgreSQL
  │                 ├─ Work Items
  │                 ├─ Processing Jobs
  │                 └─ Transactional Outbox
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
```

The API currently exposes health, readiness, version, Work Item, and processing operations.

The asynchronous path uses a transactional outbox so acceptance of processing responsibility is committed durably in PostgreSQL before publication to RabbitMQ.

The worker consumes with manual acknowledgement, bounded retry behavior, terminal failure handling, and guarded durable state transitions.

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
- logical Work Item backup and destructive recovery;
- transactional creation of processing jobs and outbox messages;
- confirmed RabbitMQ publication;
- separate RabbitMQ publisher and worker identities;
- manual-ACK worker consumption;
- bounded processing retries;
- terminal processing failure;
- idempotent processing effects;
- recovery across API, broker, worker, and datastore failures;
- restart and redelivery experiments;
- graceful in-flight worker shutdown behavior.

Important current limitations include:

- no exactly-once guarantee;
- no broker high-availability experiment;
- no horizontal worker-scaling experiment;
- no distributed tracing;
- no representative messaging-architecture comparison;
- no sustained-operation L6 claim;
- PostgreSQL backup currently protects Work Item data only, not the complete asynchronous processing state.

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
./tools/demo-api-local run
```

In another terminal, verify its basic runtime contract:

```bash
./tools/demo-api-local verify
```

The helper also provides:

```bash
./tools/demo-api-local test
./tools/demo-api-local build
```

### Worker

Run the asynchronous worker:

```bash
./tools/worker-local run
```

The helper also provides:

```bash
./tools/worker-local test
./tools/worker-local build
```

### Backup and recovery

Work Item data can be backed up using repository-owned tooling:

```bash
./tools/postgres-backup-local create <backup-path>
./tools/postgres-backup-local inspect <backup-path>
./tools/postgres-backup-local validate <backup-path>
./tools/postgres-backup-local restore <backup-path>
```

Recovery is migration-first: the target schema and privileges must already exist, and the Work Item restore target must be empty.

The current backup contract covers Work Item data and sequence state. It does **not** yet provide complete recovery of processing jobs or transactional outbox state.

See the [Sprint 03 PostgreSQL backup/restore experiment](docs/sprint-03/postgresql-backup-restore.md) and [Sprint 03 recovery runbook](docs/sprint-03/runbook.md).

## CI

GitHub Actions currently provides change-aware validation of the local-first system.

Application validation includes:

- Go formatting;
- `go vet`;
- Go tests;
- PostgreSQL integration setup and migrations;
- API integration tests;
- RabbitMQ integration setup;
- worker integration tests;
- container build validation.

The current CI path requires no AWS credentials and performs no cloud deployment.

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

The work so far includes:

- PostgreSQL persistence and migration foundations;
- runtime database privilege separation;
- backup and destructive recovery;
- additive schema evolution;
- application/schema compatibility;
- durable asynchronous processing;
- RabbitMQ delivery;
- distributed partial-failure and recovery experiments.

Sprint 03 remains active while the system continues to develop deeper engineering understanding rather than simply accumulating technologies.

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
│   └── demo-api/       current Work Items implementation
├── infra/
│   └── local/          local PostgreSQL and RabbitMQ infrastructure
├── docs/
│   ├── guides/         implementation and local-operation guides
│   ├── learning/       evidence-backed capability baseline
│   ├── rebaseline/     v2 mission and engineering model
│   ├── reference/      durable reference information
│   ├── sprint-01/      historical Sprint 01 experiments
│   ├── sprint-02/      historical Sprint 02 experiments
│   └── sprint-03/      current Sprint 03 experiments
├── evidence/           durable experiment and retirement evidence
├── scripts/            repository and CI controls
├── tools/              local development and operational helpers
└── .github/            continuous integration
```

The repository structure evolves only when a demonstrated architectural boundary justifies the change.

## Documentation

Start with:

- [Zero-to-Prod v2 rebaseline specification](docs/rebaseline/zero-to-prod-v2-specification.md) — mission, principles, learning model, environment strategy, and roadmap;
- [Current capability baseline](docs/learning/current-capability-baseline.md) — evidence-backed learning levels and remaining gaps;
- [Local demo API guide](docs/guides/local-demo-api.md) — current application workflow;
- [Local PostgreSQL guide](docs/guides/local-postgresql.md) — datastore and migration lifecycle;
- [Sprint 03 reliable asynchronous processing](docs/sprint-03/reliable-async-processing.md) — current asynchronous architecture and failure experiments.

Historical Sprint 01 and Sprint 02 documents are intentionally preserved as factual records of what was implemented and learned at the time.
