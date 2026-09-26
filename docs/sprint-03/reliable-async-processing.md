# Reliable asynchronous Work Item processing

Issue: #103 — Process Work Item jobs asynchronously without losing accepted work

## Problem

The Demo API previously had no asynchronous processing path.

Issue #103 introduced a local-first processing path with one primary reliability requirement:

> once the API reports that processing was accepted, that accepted work must survive ordinary API, broker, and worker failures without requiring the caller to submit the request again.

The implementation also had to avoid claiming exactly-once delivery.

RabbitMQ may redeliver a message when acknowledgment is uncertain, so the worker must tolerate duplicate delivery while preventing duplicate durable completion.

The implementation was developed and failure-tested locally.

No AWS messaging, worker, or other cloud infrastructure was introduced for this issue.

Expected AWS cost for this increment: `$0`.

---

## Reliability contract

The implemented contract is:

1. the API reports `202 Accepted` only after durable PostgreSQL acceptance succeeds;
2. accepted processing state and publication intent are committed atomically;
3. RabbitMQ publication happens after durable acceptance;
4. publication failure does not erase accepted work;
5. the worker uses explicit message settlement rather than automatic acknowledgment;
6. durable worker outcome is written before RabbitMQ ACK;
7. retryable failures are requeued;
8. malformed or permanently invalid messages are rejected without requeue;
9. processing retries are bounded;
10. duplicate/redelivered messages must not duplicate durable completion;
11. Work Item business status remains separate from asynchronous processing lifecycle;
12. recovery after accepted work does not require the caller to repeat the request.

This is an **at-least-once delivery** design with idempotent durable completion.

It is not an exactly-once delivery claim.

---

## Architecture

The local processing path is:

```text
Client
  |
  | POST /items/{id}/process
  v
Demo API
  |
  | one PostgreSQL transaction
  v
+------------------------------------+
| processing_jobs                    |
| outbox_messages                    |
|                                    |
| durable job + publication intent   |
+------------------------------------+
                  |
                  | background outbox publisher
                  v
          RabbitMQ durable queue
          work_item_processing
                  |
                  | manual-ACK delivery
                  v
               Worker
                  |
                  | durable outcome first
                  v
            PostgreSQL
                  |
                  | ACK only afterward
                  v
              RabbitMQ
```

The API process contains two supervised responsibilities:

```text
Demo API process
  ├── HTTP server
  └── outbox publisher
```

The worker is a separate process:

```text
worker
  ├── PostgreSQL worker identity
  └── RabbitMQ worker identity
```

---

## Durable acceptance boundary

`processing_jobs` records the system-owned processing lifecycle.

The states are:

- `accepted`
- `succeeded`
- `failed`

The row also records:

- `attempt_count`;
- `last_error_code`;
- `created_at`;
- `finished_at`.

`outbox_messages` records durable publication intent.

Each outbox row records:

- the processing job;
- event type;
- JSON payload;
- publish attempts;
- last publication error;
- creation time;
- publication time.

There is one outbox message per processing job.

The API accepts processing using one PostgreSQL transaction:

```text
BEGIN
  insert processing_jobs
  insert outbox_messages
COMMIT
```

The event envelope is:

```json
{
  "type": "work_item.process",
  "version": 1,
  "job_id": "<processing-job-id>",
  "work_item_id": "<work-item-id>"
}
```

Therefore:

```text
job exists + outbox exists
```

or:

```text
neither exists
```

A failure while creating the outbox does not leave a falsely accepted processing job behind.

---

## Work Item state is intentionally separate

Existing Work Item status remains the client-controlled business field:

```text
pending | done
```

Asynchronous processing lifecycle is stored separately in `processing_jobs`.

The worker does not use Work Item status as its delivery/retry state.

The processing experiments verified that Work Item status remained unchanged while asynchronous processing succeeded, retried, failed, and recovered.

---

## Outbox publication

The API's background publisher scans unpublished outbox rows.

A publication attempt increments `publish_attempts`.

RabbitMQ publication uses:

- the default exchange;
- routing to `work_item_processing`;
- persistent messages;
- mandatory routing;
- publisher confirms.

An unroutable publication is treated as failure and is not marked published.

After a confirmed publish, the outbox row is marked with `published_at`.

The publisher reconnect policy is:

- RabbitMQ reconnect delay: `1s`;
- idle outbox poll delay: `500ms`;
- PostgreSQL/store retry delay: `1s`.

A broker failure closes the publisher connection and causes a later reconnect.

A PostgreSQL-side outbox error does not automatically imply that the broker connection is broken.

---

## Important publication ambiguity

There is deliberately no exactly-once publication claim.

This failure window can occur:

```text
RabbitMQ accepted message
        |
        | publisher confirmation received
        v
API process fails before published_at is stored
```

After restart, the outbox row can still appear unpublished and may be published again.

Therefore duplicate RabbitMQ delivery is a normal condition the worker must tolerate.

Correctness is provided by worker idempotency, not by assuming that publication can happen only once.

---

## RabbitMQ local runtime

The local broker is pinned to:

```text
rabbitmq:4.3.6-management
```

with an immutable image digest.

Host exposure is loopback-only:

```text
127.0.0.1:5672
127.0.0.1:15672
```

The virtual host is:

```text
zero_to_prod
```

The queue is:

```text
work_item_processing
```

The queue is durable and is not auto-deleted.

Local broker lifecycle is owned by:

```text
./tools/rabbitmq-local start
./tools/rabbitmq-local stop
./tools/rabbitmq-local destroy
./tools/rabbitmq-local status
./tools/rabbitmq-local bootstrap
./tools/rabbitmq-local diagnostics
```

`start` waits for RabbitMQ health and then idempotently ensures runtime users, permissions, and queue topology.

---

## RabbitMQ least privilege

Separate runtime identities are used.

### Publisher identity

`zero_to_prod_publisher`:

- cannot configure topology;
- can publish only through RabbitMQ's default exchange;
- cannot consume.

### Worker identity

`zero_to_prod_worker`:

- cannot configure topology;
- cannot publish;
- can consume only from `work_item_processing`.

Neither runtime identity receives RabbitMQ management tags.

The administrative RabbitMQ identity is used for local topology/bootstrap and test-fixture orchestration, not normal application behavior.

---

## PostgreSQL least privilege

The application and worker also use separate database identities.

The application can create processing jobs and outbox publication intent and update only the outbox publication lifecycle fields it owns.

The worker can:

- read Work Items;
- read processing jobs;
- update processing lifecycle fields.

The worker cannot:

- update Work Item business state;
- read the outbox;
- create processing jobs.

The application cannot mutate worker-owned processing outcome fields arbitrarily.

Manual negative privilege experiments verified that PostgreSQL itself rejects these forbidden operations rather than relying only on application conventions.

---

## Worker message contract

The worker accepts:

```text
type    = work_item.process
version = 1
```

with:

```text
job_id
work_item_id
```

Malformed, unsupported, unknown, or mismatched messages do not create successful processing state.

---

## Consumer behavior

RabbitMQ consumer QoS is:

```text
prefetch_count = 1
```

Automatic acknowledgment is disabled:

```text
autoAck = false
```

The worker therefore decides when each delivery is settled.

Settlement operations are:

```text
success / already-terminal
    -> ACK
       delivery.Ack(false)

permanent invalid delivery
    -> reject without requeue
       delivery.Reject(false)

retryable delivery
    -> NACK with requeue
       delivery.Nack(false, true)
```

This is important because an ACK means the broker may remove the delivery permanently.

The worker must not ACK before the durable processing outcome is known.

---

## Durable completion and idempotency

Successful processing transitions:

```text
accepted
  -> succeeded
```

and increments `attempt_count`.

The completion update is guarded by:

```text
state = accepted
```

If the same job is delivered again after it is already terminal:

```text
succeeded
or
failed
```

the worker recognizes the terminal state and does not apply another durable completion.

The duplicate-delivery integration experiment verified:

```text
first completion:
  state         = succeeded
  attempt_count = 1

duplicate/redelivery:
  state         = succeeded
  attempt_count = 1
```

Therefore broker redelivery does not duplicate the durable completion effect.

---

## Processing failure policy

Processing failures use:

```text
last_error_code = processing_failed
```

The maximum processing attempt count is:

```text
3
```

A retryable processing failure increments `attempt_count`.

Before exhaustion:

```text
state       = accepted
finished_at = NULL
```

At the third failed processing attempt:

```text
state       = failed
finished_at = set
```

Once the job is terminal, another delivery does not create a fourth processing attempt.

Dependency failure before a processing outcome is recorded does not invent a processing attempt.

---

## Worker lifecycle

The worker retries failed delivery sessions after:

```text
1s
```

It responds to:

- `SIGINT`;
- `SIGTERM`.

RabbitMQ connection close is bounded to:

```text
2s
```

The worker launcher is:

```text
./tools/worker-local test
./tools/worker-local build
./tools/worker-local run
```

Its default local dependencies are:

- PostgreSQL worker identity;
- RabbitMQ worker identity;
- `work_item_processing`.

---

## Failure experiments

Issue #103 deliberately exercised the important ambiguous and failure boundaries.

### 1. Failure before durable acceptance

A temporary PostgreSQL trigger forced the outbox insert to fail after the processing-job insert was attempted.

Observed:

```text
HTTP response != 202
processing job = absent
outbox row     = absent
Work Item      = unchanged
```

This proved that partial acceptance is rolled back.

### 2. Failure after durable acceptance but before publication

A job and outbox row were committed without publication.

The original store/process was stopped.

A fresh publisher later discovered the durable outbox row and published it without another client request.

This proved that accepted work survives the post-commit/pre-publication failure window.

### 3. RabbitMQ unavailable while PostgreSQL remains usable

RabbitMQ was stopped while PostgreSQL remained available.

Observed:

```text
API /health = healthy
API /ready  = ready
processing request = 202
processing job     = accepted
outbox row         = unpublished
```

After RabbitMQ returned, the same API process published the existing outbox row and the worker completed the original job.

RabbitMQ is therefore not required for safe acceptance.

PostgreSQL is the durable acceptance boundary.

### 4. Worker failure before durable completion

A real RabbitMQ delivery was received but not processed/acknowledged.

The first worker session was closed.

Before restart:

```text
job state     = accepted
attempt_count = 0
finished_at   = NULL
```

A fresh worker session received the same delivery with RabbitMQ redelivery semantics and completed it.

This proved recovery from the pre-completion worker failure window.

### 5. Worker failure after durable effect but before ACK

The worker durably completed the job, but the RabbitMQ ACK was intentionally lost.

RabbitMQ redelivered the message.

The second processing pass recognized that the job was already terminal.

Observed final state:

```text
state         = succeeded
attempt_count = 1
```

This proved that the ambiguous post-effect/pre-ACK window does not duplicate durable completion.

### 6. Duplicate delivery

The same processing identity was completed again after terminal success.

The durable result did not change.

This directly verified idempotent completion independently of the broker restart experiment.

### 7. Transient processing failure followed by success

The processing function failed once.

Observed after first attempt:

```text
state           = accepted
attempt_count   = 1
last_error_code = processing_failed
```

The delivery was requeued.

The next processing attempt succeeded.

Observed final state:

```text
state           = succeeded
attempt_count   = 2
last_error_code = NULL
finished_at     = set
```

### 8. Retry exhaustion

Processing was forced to fail repeatedly.

Attempts one and two remained retryable.

Attempt three transitioned the job to:

```text
state         = failed
attempt_count = 3
finished_at   = set
```

A later delivery did not create a fourth attempt.

### 9. Worker restart

The worker-restart integration test uses a real RabbitMQ delivery and a durable PostgreSQL processing job.

A first session leaves the delivery outstanding.

A second session receives it as a redelivery and completes the existing job.

The test owns and cleans its PostgreSQL and RabbitMQ fixture state.

### 10. API restart

The API accepted work while RabbitMQ was unavailable.

The original API process was stopped.

A fresh API process started without repeating the request.

After RabbitMQ returned, the fresh process published the already-durable outbox message and the worker completed the original processing job.

This proved that recovery does not depend on caller retry or original API process lifetime.

### 11. PostgreSQL unavailable

Earlier Sprint 03 persistence experiments established the API behavior when PostgreSQL is unavailable:

- process liveness remains distinct from dependency readiness;
- readiness becomes false;
- persistence operations do not falsely succeed;
- recovery occurs after PostgreSQL returns.

Issue #103 additionally exercised worker-side PostgreSQL loss:

- the worker did not fabricate completion;
- the RabbitMQ delivery was requeued;
- processing attempts were not falsely incremented;
- the same work recovered after PostgreSQL returned.

### 12. Graceful shutdown while work is in flight

A PostgreSQL lock deliberately blocked worker completion while RabbitMQ showed one unacknowledged delivery.

`SIGTERM` was sent to the worker.

Observed:

```text
worker exit         = clean
delivery            = requeued
processing state    = accepted
processing attempts = 0
```

A fresh worker recovered the same delivery and completed it once.

### 13. Malformed / unsupported message

Malformed and unsupported messages were rejected without requeue.

They did not mutate processing state.

The worker remained available for subsequent valid work.

---

## Additional publisher failure evidence

The publisher was tested against an intentionally nonexistent queue.

Observed after the failed publish:

```text
publish_attempts = 1
last_error_code  = broker_publish_failed
published_at     = NULL
```

After retrying against the valid queue:

```text
publish_attempts = 2
last_error_code  = NULL
published_at     = set
```

The publisher did not change Work Item business state or worker processing state.

---

## Integration-test isolation

The initial worker integration tests depended on externally prepared numeric Work Item/job IDs.

That made the tests non-hermetic and unsuitable as strong CI evidence.

The worker integration fixtures were changed so tests now own:

- their PostgreSQL Work Item;
- their processing job;
- their RabbitMQ test message;
- their cleanup.

Broker tests refuse to begin when the shared queue already contains unrelated messages or consumers.

They do not silently purge unknown work.

The administrative RabbitMQ test-fixture identity performs setup/cleanup while the behavior under test still uses the restricted worker identity.

---

## Shared RabbitMQ test boundary

API publisher tests legitimately leave messages in `work_item_processing` because they test publication, not consumption.

The worker integration tests deliberately require an empty broker boundary.

Running both packages against one shared queue without orchestration produced the expected isolation failure.

The validated CI boundary is therefore:

```text
API integration tests
        |
        v
explicitly purge shared test queue
        |
        v
verify queue = 0 ready / 0 unacked / 0 total / 0 consumers
        |
        v
worker integration tests
```

Only RabbitMQ requires this package boundary.

A PostgreSQL reset between API and worker integration phases was experimentally shown to be unnecessary.

---

## CI validation

Commit:

```text
2e7a768 ci: run local async integration tests
```

extended `app-validation`.

The job now:

1. runs ordinary dependency-free Go validation;
2. starts local PostgreSQL;
3. builds and applies migrations;
4. starts and bootstraps local RabbitMQ;
5. runs API integration tests with required environment variables;
6. fails if an API integration test is skipped;
7. purges and verifies the shared RabbitMQ test boundary;
8. runs worker integration tests with required environment variables;
9. fails if a worker integration test is skipped;
10. always runs the local dependency cleanup step;
11. continues to the normal Docker image build validation.

The exact lifecycle was rehearsed locally from:

```text
clean runtime
  -> dependency startup
  -> migrations
  -> API integration
  -> RabbitMQ boundary cleanup
  -> worker integration
  -> dependency teardown
```

Observed:

```text
API integration exit code       = 0
API unexpected skips            = 0
worker integration exit code    = 0
worker unexpected skips         = 0
final RabbitMQ queue            = 0|0|0|0
leftover worker DB fixtures     = 0
RabbitMQ cleanup exit code      = 0
PostgreSQL cleanup exit code    = 0
```

---

## CI deployment boundary

The repository deployment mode remains:

```text
local-only
```

CI path-classification regression tests passed after the async integration change.

The workflow classification remained:

```text
app=true
terraform=true
workflow=true
deploy=false
```

Therefore Issue #103's local asynchronous-processing work does not trigger:

- ECR publication;
- ECS deployment;
- Terraform apply;
- new AWS runtime infrastructure.

---

## Cost

Issue #103 uses local Docker infrastructure for the asynchronous runtime experiments:

- PostgreSQL;
- RabbitMQ;
- API;
- worker.

No AWS messaging or worker infrastructure was introduced.

Expected incremental AWS cost:

```text
$0
```

---

## What this proves

The implemented path provides evidence for:

- durable asynchronous acceptance;
- transactional outbox publication intent;
- confirmed broker publication;
- bounded publication recovery;
- manual message acknowledgment;
- bounded worker retry;
- explicit terminal failure;
- redelivery recovery;
- idempotent durable completion;
- API restart recovery;
- worker restart recovery;
- partial dependency failure handling;
- graceful in-flight worker shutdown;
- runtime least privilege;
- deterministic integration testing;
- CI execution of the real PostgreSQL/RabbitMQ path.

The key invariant is:

```text
accepted by API
    =>
durable PostgreSQL state exists
    =>
caller retry is not required for recovery
```

---

## What this does not prove

Issue #103 does not claim:

- exactly-once message delivery;
- exactly-once broker publication;
- a dead-letter queue design;
- long-term failed-job replay tooling;
- horizontal worker scaling behavior;
- ordering guarantees across different jobs;
- backpressure behavior under production load;
- broker clustering or broker high availability;
- production RabbitMQ operations;
- Kubernetes worker orchestration;
- managed-cloud broker behavior;
- cross-region recovery;
- production SLOs;
- L5 mastery.

The worker's current processing function is intentionally minimal.

The goal of Issue #103 is the reliable delivery and recovery boundary, not domain-specific background computation.

---

## Capability conclusion

The evidence supports **L4 — failure-tested** for the implemented local asynchronous Work Item processing path.

The system was not only implemented and tested on the happy path; deliberate experiments covered:

- pre-commit failure;
- post-commit/pre-publication failure;
- broker outage;
- PostgreSQL outage;
- API restart;
- worker restart;
- pre-completion failure;
- post-effect/pre-ACK failure;
- duplicate delivery;
- transient processing failure;
- retry exhaustion;
- graceful shutdown;
- malformed delivery;
- least-privilege boundaries;
- CI isolation.

This does not support L5.

Representative alternative architectures were not implemented and compared, and the broader production-operability questions remain intentionally out of scope.
