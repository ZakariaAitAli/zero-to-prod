# Local operations and incident response

Owner: Zakaria Ait Ali (confirmed 2026-10-18). Scope: supervised local Docker
lab drills; no 24/7 support or production on-call commitment. No secondary
responder is assigned. If the owner cannot recover the lab, stop traffic and
preserve evidence; escalation to another person must be explicitly arranged,
not assumed. This is a production-readiness gap.

During a supervised drill the owner coordinates the incident, acknowledges the
symptom, records UTC timestamps, chooses the recovery action and confirms the
user-visible outcome. The assistant may inject faults and collect evidence.
One person filling every role is not independent incident management. Scripted
acknowledgment times must never be described as human MTTA.

Start from the alert or symptom. Inspect [alerts](http://localhost:19094/alerts),
[metrics](http://localhost:19094), [dashboards](http://localhost:13004),
[traces](http://localhost:16690) and [local notifications](http://localhost:18086).
Use `tools/operations-lab-local compose ps` to inspect this project only.
Do not print container environments, database URLs or runtime secret files.

## API

Business reads fail and API scrape is down. Check API container state and logs,
then `tools/operations-lab-local compose up -d --wait api`. Check `/ready`,
business list/create/process/result, then alert resolution. An API process
restart is not a fix for a database or broker failure.

## Database

Business errors plus `pg_up == 0` indicate datastore connectivity loss. Check
PostgreSQL state, logs, disk availability and exporter health before restarting.
For the controlled stopped-process drill:
`tools/operations-lab-local compose up -d --wait postgres`.
Do not remove volumes or restore a backup as the first response. A corrupt or
lost database requires the separate reviewed recovery procedure and explicit
RPO/data-loss decision. Confirm business processing, not merely pg_isready.

## Broker

Accepted jobs stop completing and the broker scrape disappears. Inspect broker
state and the outbox/backlog, then for the controlled stop drill run
`tools/operations-lab-local compose up -d --wait rabbitmq`.
Let confirmed-publication/outbox recovery and worker reconnect happen. Confirm
accepted jobs reach terminal state. Do not purge queues, ACK unseen deliveries,
weaken confirms or create new IDs to conceal lost work.

## Backlog

If the broker is healthy but accepted age grows, inspect worker state, queue
consumers, retry/DLQ counts and CPU saturation. For a stopped-worker drill:
`tools/operations-lab-local compose up -d --wait worker`.
Watch oldest accepted age and backlog drain. Increasing workers is justified
only after identifying the bottleneck; the capacity experiments show that a
broker CPU limit can dominate despite idle workers.

## Other incident classes

- Bad release: readiness refusal, business checks and rollback procedure in
  [delivery lab](../guides/local-delivery-lab.md). Database changes remain.
- Certificate failure: [security guide](../guides/local-security-lab.md), chain,
  name and expiry diagnosis; renew via existing tooling, never bypass trust.
- Credentials: identify service/role without logging values; follow protected
  rotation/rollback procedure. Restarting with the same invalid secret is not
  recovery.
- Saturation: [capacity experiment](../experiments/issue-123-capacity.md), check
  CPU quota, confirms, queue and outbox before adding replicas.
- Backup recovery: [availability runbook](local-availability-recovery.md), fence
  writers and select one authoritative timeline before activation.

Record detection, delivered alert, acknowledgment, diagnosis, action, first
successful business result and resolution separately. Detection time starts
at the injected fault only in controlled drills. MTTR ends at verified business
recovery; a container being up is insufficient. Review duplicate/noisy alerts,
missed failures, stale telemetry and runbook mistakes after each incident.

The local receiver proves webhook delivery to a loopback-readable file on the
same host. It does not prove phone/email delivery, off-host durability, waking
an operator, coverage schedules or real escalation. Short `LocalDrill*` alerts
are explicit test signals and do not replace the existing production-style
SLO burn thresholds.
