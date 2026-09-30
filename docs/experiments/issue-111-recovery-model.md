# Issue #111 — Accepted Async Work Recovery Experiment

## Question

Can asynchronous work that has received `202 Accepted` survive destructive
PostgreSQL data loss and resume processing after migration-first restoration
without client resubmission?

## Scope

Local-first experiment using the current Work Items architecture:

- PostgreSQL
- `work_items`
- `processing_jobs`
- `outbox_messages`
- transactional outbox
- API-embedded outbox publisher
- RabbitMQ
- separate worker

## Initial state

After `POST /items/1/process` returned `202 Accepted`:

- `work_items`: 1
- `processing_jobs`: 1, state `accepted`
- `outbox_messages`: 1, `published_at = NULL`
- RabbitMQ was unavailable

The accepted processing responsibility therefore existed in PostgreSQL while
publication was still pending.

## Recovery point

A validated logical backup was created containing:

- `work_items`
- `processing_jobs`
- `outbox_messages`
- sequence state for all three relations

## Destructive test

The PostgreSQL container, network and data volume were destroyed.

The recovery artifact remained available.

A new PostgreSQL volume was created and migrations 1–4 were applied before
restoring application data.

## Recovery result

The restore recovered:

- `work_item #1`
- `processing_job #1`, state `accepted`
- `outbox_message #1`

RabbitMQ was then started.

The recovered outbox message was published and `published_at` was recorded.

The worker subsequently consumed the message and the processing job reached:

`state = succeeded`

with:

`attempt_count = 1`

The RabbitMQ queue drained to zero messages.

No client resubmission was required.

## Result

The experiment demonstrated that the current recovery boundary can preserve
and resume an accepted asynchronous processing responsibility when the
relevant PostgreSQL state is included in the recovery point.

## Recovery guarantee demonstrated

For the current architecture:

`202 Accepted`
→ durable PostgreSQL acceptance
→ backup containing required state
→ destructive PostgreSQL loss
→ migration-first restore
→ outbox republished
→ worker resumes processing

## Limitations

This experiment does not establish:

- a production RPO;
- a production RTO;
- continuous recovery;
- PITR/WAL recovery;
- protection against loss occurring after the latest recovery point;
- RabbitMQ disaster recovery;
- production-scale recovery performance.

The recovery point remains the boundary of what this backup strategy can
recover.

## Important architectural observation

The recovery boundary must follow the application's durable state.

When asynchronous processing was introduced, `processing_jobs` and
`outbox_messages` became part of the state required to preserve accepted
work. The previous Work Item-only backup therefore no longer represented the
complete recovery boundary.

## Environment classification

LOCAL-FIRST
