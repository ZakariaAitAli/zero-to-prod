# Work Items architecture

Work Items is the current Zero-to-Prod reference system.

It is intentionally local-first. PostgreSQL and RabbitMQ run in repository-owned local Docker environments so the project can study portable stateful and distributed-system behavior without requiring cloud infrastructure for every experiment.

## System view

~~~text
                         ┌─────────────────────┐
                         │   Work Items Web UI │
                         │ React / Vite / TS   │
                         └─────────┬───────────┘
                                   │
                              HTTP / JSON
                                   │
                                   ▼
                         ┌─────────────────────┐
                         │    Work Items API   │
                         └─────────┬───────────┘
                                   │
                         runtime PostgreSQL role
                                   │
                                   ▼
                         ┌─────────────────────┐
                         │     PostgreSQL      │
                         │                     │
                         │ work_items          │
                         │ processing_jobs     │
                         │ outbox_messages     │
                         └─────────┬───────────┘
                                   │
                         outbox publisher
                                   │
                                   ▼
                         ┌─────────────────────┐
                         │      RabbitMQ       │
                         └─────────┬───────────┘
                                   │
                                   ▼
                         ┌─────────────────────┐
                         │  Work Items worker  │
                         └─────────┬───────────┘
                                   │
                             durable outcome
                                   │
                                   ▼
                              PostgreSQL
~~~

## API responsibilities

The API provides process health, dependency-aware readiness, version identity, Work Item persistence, and durable acceptance of asynchronous processing.

Relevant endpoints include:

```text
GET  /health
GET  /ready
GET  /version
POST /items
GET  /items
POST /items/{id}/process
GET  /processing-jobs/{id}
```

## Web UI boundary

The Web UI is a distinct application artifact under:

~~~text
apps/work-items/web/
~~~

It is responsible for browser-facing interaction and presentation.

The Go API remains the boundary for persistence and asynchronous-processing commands.

The browser can:

- list Work Items;
- create Work Items;
- submit a processing command;
- retain the returned processing-job identifier;
- read processing-job state until it reaches a terminal outcome.

For local development, the frontend calls `/api/*` and Vite proxies those requests to the Go API.

The API therefore does not enable a broad CORS policy merely to support the local frontend.

This proxy is a development convenience only. It does not define production ingress, TLS, CDN, hostname, reverse-proxy, or production CORS architecture.

## PostgreSQL boundary

PostgreSQL is the system of record for the current Work Items state exercised by the lab.

Schema changes are applied explicitly through repository-owned migrations. Application startup does not automatically migrate the database.

The local lab separates bootstrap administration, migration, API runtime, and worker runtime identities.

The API's readiness contract checks the columns it requires in `work_items`, `processing_jobs`, and `outbox_messages`.

## Schema evolution

The Work Items schema evolved from the original table to an additive form containing a `status` field.

Sprint 03 tested compatibility between old and new application artifacts and the old/new schema boundary.

The current approach keeps application rollback and schema rollback as separate decisions.

## Backup and recovery

### Durable accepted-work boundary

Once the API returns `202 Accepted`, the system has accepted responsibility for the processing request. That responsibility is held in PostgreSQL:

```text
work_items        the business object
processing_jobs   the accepted processing responsibility and its outcome
outbox_messages   the pending or completed publication to RabbitMQ
```

These three tables, plus their identity sequences, are the current logical recovery boundary ([ADR 0001](../adr/0001-work-items-async-recovery-boundary.md)).

### Backup model

`tools/postgres-backup-local` produces a migration-first, data-only logical archive of exactly that boundary.

Recovery reconstructs the database foundation and schema from repository configuration and migrations, then restores the archive into a target where all three tables exist and are empty.

The backup is a manual logical snapshot, not point-in-time recovery.

The [Issue #111 experiment](../experiments/issue-111-recovery-model.md) showed that an accepted, unpublished job captured in the archive survives destructive PostgreSQL loss and completes after restore without client resubmission.

### What PostgreSQL recovery does not cover

RabbitMQ state is outside the backup.

An outbox row already marked `published_at` is not republished after restore. If the broker lost that message, the restored job stays `accepted` and nothing currently reconciles it. That case needs a separate recovery or reconciliation design.

See the [PostgreSQL backup and recovery runbook](../sprint-03/runbook.md).

## Asynchronous acceptance

Processing acceptance uses PostgreSQL as the durability boundary.

A successful request commits both the processing responsibility and outbox state before returning `202 Accepted`.

RabbitMQ publication occurs afterward.

This means broker availability is not required for the API to durably accept processing responsibility when PostgreSQL can commit it.

## Transactional outbox

The outbox bridges the PostgreSQL transaction boundary and RabbitMQ without pretending they share one atomic transaction.

```text
API transaction
   ├── processing_jobs
   └── outbox_messages
        ↓
      COMMIT
        ↓
outbox publisher
        ↓
RabbitMQ
```

If RabbitMQ is unavailable, unpublished outbox state remains in PostgreSQL for retry.

## Worker boundary

The worker consumes RabbitMQ deliveries and records durable processing outcomes in PostgreSQL.

The consumer model assumes that messages may be delivered more than once.

Durable completion and idempotent handling make redelivery survivable, including the case where the durable effect succeeds but the worker fails before acknowledging the broker delivery.

## Failure model

Sprint 03 deliberately exercised failures around:

- database availability;
- insufficient schema;
- dirty migration state;
- destructive database loss and restore;
- failure before asynchronous acceptance;
- failure after acceptance but before publication;
- RabbitMQ outage;
- worker failure before durable completion;
- worker failure after durable effect but before ACK;
- duplicate delivery;
- transient processing failure;
- retry exhaustion;
- API and worker restart;
- graceful shutdown;
- malformed or unsupported messages.

The architecture is designed around explicit durability boundaries rather than an assumption that components fail together.

## Local infrastructure

The current local environment is defined under:

```text
infra/local/
```

Repository-owned helpers include:

```text
tools/postgres-local
tools/postgres-backup-local
tools/rabbitmq-local
tools/work-items-api-local
tools/work-items-worker-local
```

## Current scope boundary

This architecture does not claim production parity.

The current experiments do not establish managed-database high availability, point-in-time recovery, production RabbitMQ operation, production frontend delivery or origin policy, Kubernetes, multi-region recovery, or a production observability stack.

Those capabilities should be introduced when an engineering problem requires them, following the v2 specification.

## Related documentation

- [Work Items async recovery boundary ADR](../adr/0001-work-items-async-recovery-boundary.md)
- [Work Items Web UI boundary ADR](../adr/0002-work-items-web-ui-boundary.md)
- [Issue #111 recovery experiment](../experiments/issue-111-recovery-model.md)
- [PostgreSQL backup and recovery runbook](../sprint-03/runbook.md)
- [Local Work Items guide](../guides/local-work-items.md)
- [Local PostgreSQL guide](../guides/local-postgresql.md)
- [Database lifecycle and recovery](../concepts/database-lifecycle-and-recovery.md)
- [Reliable asynchronous processing](../concepts/reliable-asynchronous-processing.md)
- [PostgreSQL backup and destructive recovery](../sprint-03/postgresql-backup-restore.md)
- [PostgreSQL schema evolution](../sprint-03/postgresql-schema-evolution.md)
- [Reliable asynchronous processing experiments](../sprint-03/reliable-async-processing.md)
