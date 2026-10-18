# ADR 0014: local availability and recovery objectives

- Date: 2026-10-18
- Status: accepted for the local experiment; guarantees require recorded tests
- Issue: #124

## Requirements before topology

The local workload uses durable accepted jobs and idempotent results. A lost
application process should not lose accepted work. For this learning lab:

| Event | Objective | Boundary |
| --- | --- | --- |
| One API process lost | requests recover within 10 s; accepted work preserved | in-flight HTTP errors permitted and measured |
| PostgreSQL primary lost | manual fenced promotion within 120 s; observed RPO <=5 s | async replication cannot guarantee a bound under arbitrary partition |
| One broker node lost | confirmed work preserved, progress resumes within 60 s | requires a live majority of a three-member quorum |
| Local primary data destroyed | verified restore within 10 min; explicit marker-based RPO | archive/base backup remain available on this same host |
| Backup scheduling | successful verified backup within two intervals; retain three successful generations | failed/stale backups remain visible; do not discard the last good backup |

The objectives prioritize correctness over pretending all outages are
invisible. They are initial local test thresholds, not an SLA. A Docker host,
its disk, the routing proxy and the operator remain single points of failure.
Independent-site backup survival cannot be proved by separate volumes.

## Decision

Use two application processes, PostgreSQL physical streaming replication
with manual fencing/promotion, and three RabbitMQ nodes with a quorum queue.
Use publisher confirms and the existing outbox/delivery identity rather than
weakening acknowledgements during failure. Retain a base backup and WAL for
actual PITR, independently verify backup contents and boot a restored cluster.
Demonstrate backup transfer/access isolation locally and explicitly leave
physical off-site durability unproven.

Manual promotion avoids implying an automatic leader-election/fencing system
that this lab does not implement. Asynchronous replication keeps writes
available without a standby, but exposes data loss if it falls behind. The
experiment must distinguish a caught-up failover from deliberately missing
replication. Never start an old unfenced primary after promotion; failback
requires reconciliation/reseeding.

## Alternatives

Single instances plus restore are simpler and cheaper, but recovery is slow.
Synchronous replication can protect acknowledged commits at the cost of
blocking when the required standby disappears. An automatic PostgreSQL HA
controller and independent consensus/fencing infrastructure would remove
operator steps but introduce a larger failure-domain problem than this
single-host experiment can validate. Managed services shift some operations
and costs; no cloud environment was selected. A three-node broker with a
classic unreplicated queue does not replicate message durability merely by
being clustered.

## References and limits

- [PostgreSQL 18 streaming standby](https://www.postgresql.org/docs/18/warm-standby.html)
- [PostgreSQL 18 WAL archive and PITR](https://www.postgresql.org/docs/18/continuous-archiving.html)
- [RabbitMQ quorum queues](https://www.rabbitmq.com/docs/quorum-queues)

Configurations are not evidence. Record actual timelines, markers, failures,
RPO/RTO, acknowledgement semantics and cleanup before claiming any objective.
