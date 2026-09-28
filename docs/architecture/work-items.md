# Work Items architecture

Work Items is the current Zero-to-Prod reference system.

It is intentionally local-first. PostgreSQL and RabbitMQ run in repository-owned local Docker environments so the project can study portable stateful and distributed-system behavior without requiring cloud infrastructure for every experiment.

## System view

```text
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
```

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
```

## PostgreSQL boundary

PostgreSQL is the system of record for the current Work Items state exercised by the lab.

Schema changes are applied explicitly through repository-owned migrations. Application startup does not automatically migrate the database.

The local lab separates bootstrap administration, migration, and runtime application identities.

The API's readiness contract depends on the PostgreSQL schema it needs to serve the current application contract.

## Schema evolution

The Work Items schema evolved from the original table to an additive form containing a `status` field.

Sprint 03 tested compatibility between old and new application artifacts and the old/new schema boundary.

The current approach keeps application rollback and schema rollback as separate decisions.

## Backup and recovery

The tested backup model is migration-first and data-only for the scoped Work Item backup contract.

Recovery reconstructs the database foundation and schema from repository configuration and migrations, then restores the retained application data.

The backup is a manual logical snapshot, not point-in-time recovery.

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

The current experiments do not establish managed-database high availability, point-in-time recovery, production RabbitMQ operation, Kubernetes, multi-region recovery, or a production observability stack.

Those capabilities should be introduced when an engineering problem requires them, following the v2 specification.

## Related documentation

- [Local Work Items guide](../guides/local-work-items.md)
- [Local PostgreSQL guide](../guides/local-postgresql.md)
- [Database lifecycle and recovery](../concepts/database-lifecycle-and-recovery.md)
- [Reliable asynchronous processing](../concepts/reliable-asynchronous-processing.md)
- [PostgreSQL backup and destructive recovery](../sprint-03/postgresql-backup-restore.md)
- [PostgreSQL schema evolution](../sprint-03/postgresql-schema-evolution.md)
- [Reliable asynchronous processing experiments](../sprint-03/reliable-async-processing.md)
