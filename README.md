# Zero-to-Prod

A local DevOps lab for building and operating a stateful application. It covers
container delivery, persistence, asynchronous processing, security, observability
and recovery using a small reference system called **Work Items**.

## Start here

1. [Run Work Items locally](docs/guides/local-work-items.md).
2. [Understand the architecture](docs/architecture/work-items.md).
3. [Choose a lab or operational task](docs/README.md).

You need Docker with Compose, Bash, curl, Go 1.27.4, Node.js 22 and pnpm 12.8.1.
Local operation requires no cloud account.

## The system

```text
Browser → Go API → PostgreSQL
                     │ transactional outbox
                     ▼
                  RabbitMQ → Go worker → PostgreSQL
```

The API stores Work Items and accepts processing jobs durably before publishing.
The worker writes a title-analysis result, marks the item done and completes the
job in one transaction before acknowledging the message. Delivery is at least
once; database effects are idempotent. Exactly-once delivery is not promised.

The repository contains the API, worker, React UI, migrations, Docker lab
configurations and operational tools.

## Explore further

| Goal | Guide |
|---|---|
| Add identity, ingress and TLS | [Security lab](docs/guides/local-security-lab.md) |
| Investigate logs, metrics and traces | [Observability lab](docs/guides/local-observability-lab.md) |
| Measure saturation and scaling | [Capacity lab](docs/guides/local-capacity-lab.md) |
| Work through failover and restore | [Availability lab](docs/guides/local-availability-lab.md) |
| Compare release and rollback behavior | [Delivery lab](docs/guides/local-delivery-lab.md) |
| Respond to incidents | [Operations runbook](docs/runbooks/local-operations.md) |

These are separate, opt-in local labs. Do not assume their configurations combine
into one deployment. Use the guide for the lab you are running.

## Checks and limits

CI runs change-aware backend, frontend and workflow checks, including real
PostgreSQL/RabbitMQ integration tests and scans of the application images.
Frontend client tests do not replace browser or end-to-end tests.

This is a single-host learning environment. Production traffic, independent-host
availability, off-site disaster recovery and sustained on-call coverage are not
established. Development credentials and private lab CAs are not production defaults.

[Current gaps](docs/plans/local-production-readiness.md) ·
[Recovery runbook](docs/runbooks/work-items-recovery.md) ·
[Design decisions](docs/adr/README.md)
