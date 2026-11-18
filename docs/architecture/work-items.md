# Work Items architecture

Work Items has three application processes: a Go HTTP API, a Go asynchronous
worker and a React browser UI. PostgreSQL owns durable state; RabbitMQ delivers
processing notifications. The UI is a separate build artifact.

## Request and processing flow

```text
Browser / client
      │ HTTP
      ▼
     API ───────────────► PostgreSQL
      │                    work_items
      │                    processing_jobs
      │                    outbox_messages
      │                    work_item_results
      │
      └─ outbox publisher ─► RabbitMQ ─► Worker ─► PostgreSQL
```

1. The API commits a processing job and outbox row together before returning
   acceptance. Broker availability is not the durable acceptance boundary.
2. The publisher sends an outbox message with broker confirmation, then records
   publication. A crash between those steps may cause publication again.
3. The worker consumes with manual acknowledgement. On success, one database
   transaction stores the result, marks the item done and the job succeeded.
4. The worker acknowledges only after the durable outcome is known. Redelivery
   must not create another business result.

This is at-least-once delivery with idempotent database effects. External side
effects and exactly-once delivery are not covered by that guarantee.

## State

| Model | States / responsibility |
|---|---|
| Work Item | `pending` or `done`: business state. |
| Processing Job | `accepted`, `succeeded` or `failed`: execution state. |
| Outbox message | Durable publication intent and delivery identity. |
| Result | Deterministic title analysis, associated with one successful job. |

A successful job, its result and the done item transition are atomic. A failed
job leaves the item pending. Business state must not be inferred from whether
a message was delivered. Attempt accounting and retry exhaustion follow the
[success semantics decision](../adr/0003-work-items-async-success-semantics.md).

## Boundaries

- **API:** HTTP validation, durable acceptance, status/result reads and outbox
  publication. It does not execute background work in the request handler.
- **Worker:** delivery validation, bounded retries, idempotent completion and
  graceful handling of in-flight work.
- **PostgreSQL:** application state, constraints, migrations and transaction
  boundaries. Bootstrap administration, migrations, API and worker access use
  separate identities.
- **RabbitMQ:** message delivery, acknowledgements and redelivery. Its state
  cannot replace a database record of accepted responsibility.
- **UI:** calls `/api/*`; the development server proxies to the API. The security
  lab supplies the authenticated ingress instead. Browser errors and job states
  remain separate concerns.

## Lifecycle and recovery

Schema changes run explicitly through the migration tool. Startup is not a
migration mechanism. Application/schema compatibility and rollback need separate
assessment; application rollback does not undo a schema change.

Logical recovery includes `work_items`, `processing_jobs`, `outbox_messages`,
`work_item_results` and sequence state for the identity-bearing tables. A database
restore does not restore RabbitMQ. The worker checks a random delivery identity
so stale broker messages cannot target reused numeric IDs. Offline redrive can
republish accepted work under explicit fencing and stopped-runtime conditions.

Use the [recovery runbook](../runbooks/work-items-recovery.md) for actual restore,
migration compatibility and redrive procedures. A readable backup is not proof
that recovery will succeed.

## Runtime profiles

The basic development profile uses local PostgreSQL and RabbitMQ with development
credentials and authentication disabled. Additional security, observability,
capacity, availability, delivery and operations labs have separate Compose
configurations and lifecycle tools. They are not interchangeable overrides of
one production deployment.

The [security architecture](security-lab.md) defines ingress, identity and TLS.
The [SLO reference](../reference/work-items-slos.md) defines the implemented
measurement semantics and their limitations.

## Limits

Single-host containers do not establish independent-host availability or off-site
recovery. Combining profiles requires resolving certificate names, queue types,
secret rotation, capacity through ingress and authenticated alerting. See the
[current gaps](../plans/local-production-readiness.md).
