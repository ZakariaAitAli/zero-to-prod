# Documentation

Start with the [local setup guide](guides/local-work-items.md). Read the
[architecture](architecture/work-items.md) when you need to understand the design.
Everything else is organized by the task you want to perform.

## Run and change the system

| Task | Document |
|---|---|
| Run the API, worker and UI | [Local development](guides/local-work-items.md) |
| Manage PostgreSQL and migrations | [PostgreSQL guide](guides/local-postgresql.md) |
| Understand state and message processing | [Work Items architecture](architecture/work-items.md) |
| Understand a design choice | [Decision index](adr/README.md) |

## Operate and recover

| Task | Document |
|---|---|
| Recover application data and accepted work | [Recovery runbook](runbooks/work-items-recovery.md) |
| Redrive deliveries after a restore | [Restore delivery recovery](guides/restore-delivery-recovery.md) |
| Investigate service symptoms | [Observability runbook](runbooks/work-items-observability.md) |
| Handle an incident | [Operations runbook](runbooks/local-operations.md) |
| Perform failover or PITR | [Availability recovery](runbooks/local-availability-recovery.md) |
| Interpret SLIs and alert thresholds | [SLO reference](reference/work-items-slos.md) |

## Optional labs

[Security](guides/local-security-lab.md) ·
[Observability](guides/local-observability-lab.md) ·
[Capacity](guides/local-capacity-lab.md) ·
[Availability](guides/local-availability-lab.md) ·
[Delivery](guides/local-delivery-lab.md) ·
[Operations](guides/local-operations-lab.md)

The security lab has its own [architecture](architecture/security-lab.md),
[security model](architecture/security-lab-security-model.md).
Use the [third-party image report](guides/third-party-image-report.md) to inspect
pinned dependency images; its findings are informational, not a release gate.

## Remaining work

[Current gaps and constraints](plans/local-production-readiness.md). A local lab
configuration or passing test does not establish production readiness.
