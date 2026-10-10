# Reliable asynchronous processing

Asynchronous processing changes the question from:

> Did this HTTP request finish the work?

to:

> Once the system accepts responsibility for the work, how does that responsibility survive partial failures until a durable outcome exists?

The Work Items system exercises that problem with PostgreSQL, a transactional outbox, RabbitMQ, and a separate worker.

## Durable acceptance

The API exposes:

```text
POST /items/{id}/process
```

A `202 Accepted` response is returned only after PostgreSQL has committed the processing responsibility and corresponding outbox state.

The acceptance boundary is therefore the database transaction, not RabbitMQ publication.

Conceptually:

```text
request
   ↓
PostgreSQL transaction
   ├── processing responsibility
   └── outbox message
   ↓
COMMIT
   ↓
202 Accepted
```

If the transaction does not commit, the API has not durably accepted the work.

## Why not write the database and publish directly?

Consider:

```text
write business state
      ↓
publish message
```

Those are operations in two different systems.

If the process fails between them, one system can record the change while the other never receives the message.

Reversing the order creates the opposite inconsistency.

The transactional outbox places the business responsibility and publication intent in the same PostgreSQL transaction. A separate publisher can then retry broker publication from durable database state.

## Broker availability is outside acceptance

RabbitMQ is not part of the API's acceptance transaction.

That is deliberate.

If PostgreSQL can commit the responsibility while RabbitMQ is unavailable, the API-side publisher can retry later. This is why RabbitMQ availability is not part of the current API readiness contract.

The system separates:

```text
durably accepting responsibility
            from
delivering that responsibility to a worker
```

## Publication ambiguity

The outbox does not create exactly-once delivery.

There is an unavoidable boundary around broker publication and recording that publication back in PostgreSQL.

For example:

```text
publish message
      ↓
broker confirms
      ↓
process crashes before database marks it published
```

After restart, the publisher may send the same message again because the database still represents it as unpublished. Zero-to-Prod reproduced this window in the [crash-consistency experiment](../experiments/issue-118-crash-consistency.md) by killing the publisher and terminating its in-flight publication-marker statement. In the observed run, killing the publisher alone let PostgreSQL finish the marker, so no duplicate was published.

The correct design question is therefore not "how do we guarantee the message is never duplicated?" but "how does the consumer behave safely when a message is delivered again?"

## At-least-once-style delivery and idempotency

The Work Items worker is designed around possible redelivery.

Durable completion state in PostgreSQL allows the consumer to recognize work that has already reached its durable effect and avoid applying that effect a second time.

This is idempotent processing at the business-operation boundary.

The system deliberately exercised duplicate delivery and worker failure after durable effect but before acknowledgement.

That failure is important because the broker may redeliver even though the database already contains the completed effect.

## Acknowledgement follows durable outcome

A message acknowledgement tells RabbitMQ that the delivery no longer needs to be retried.

The worker therefore needs to reason about acknowledgement relative to the durable processing result.

Acknowledging before durable completion risks losing responsibility.

Failing after durable completion but before ACK risks duplicate delivery.

The second case is recoverable when processing is idempotent.

## Retry is a policy

Not every failure should retry forever.

The Sprint 03 experiments distinguish transient processing failure from retry exhaustion and malformed or unsupported messages. The processing failures were deliberately exercised with test-injected processors; the production title analysis has no failure path, so no naturally occurring processing failure has been observed.

A reliable worker needs a defined policy for:

- which failures are retryable;
- how attempts are counted;
- what terminal failure means;
- what durable state records that terminal outcome.

Retries without a terminal policy can turn a permanent defect into an infinite processing loop.

## Partial failure is normal

The architecture deliberately tests failures at different points:

- before durable acceptance;
- after acceptance but before publication;
- RabbitMQ unavailable;
- worker failure before durable completion;
- worker failure after durable effect but before ACK;
- duplicate delivery;
- transient processing failure (test-injected processor);
- retry exhaustion (test-injected processor);
- API restart;
- worker restart;
- PostgreSQL unavailable;
- graceful shutdown with in-flight work;
- malformed or unsupported messages.

These are not all the same "queue failure." Each crosses a different durability boundary.

## Graceful shutdown

Asynchronous components also need lifecycle behavior.

A process shutting down should stop taking new responsibility and give bounded in-flight operations a chance to reach a safe boundary before exit.

The project tests graceful shutdown while work is in flight rather than assuming process termination is harmless.

## What the current design does not claim

The lab does not claim exactly-once messaging.

It also does not establish every production messaging concern. The Sprint document records the current implementation boundaries and remaining unknowns.

The useful learning result is the failure model:

```text
durable acceptance
      ↓
eventual publication
      ↓
possible redelivery
      ↓
idempotent durable processing
      ↓
ACK / terminal outcome
```

## What to remember

- `202 Accepted` should correspond to a real durability boundary.
- Database commit and broker publication are separate failure domains.
- A transactional outbox makes publication intent recoverable with the accepted database state.
- An outbox does not create exactly-once delivery.
- Broker confirmation and database publication state can become ambiguous across a crash.
- Consumers should expect redelivery.
- Idempotency makes duplicate delivery survivable.
- ACK timing must follow the durability model.
- Retry needs a terminal policy.
- Distributed reliability comes from understanding partial-failure boundaries, not from assuming the queue makes work reliable.

## Zero-to-Prod deep dives

- [Work Items architecture](../architecture/work-items.md)
- [Local Work Items development](../guides/local-work-items.md)
- [Reliable asynchronous processing experiments](../sprint-03/reliable-async-processing.md)
- [ADR 0003 — async success semantics and result ownership](../adr/0003-work-items-async-success-semantics.md)
- [Crash-consistency experiment](../experiments/issue-118-crash-consistency.md)
