# Production readiness review

Work Items is a local reference workload. Its optional labs exercise individual
engineering concerns; they are not a fully integrated production deployment.

**Verdict: suitable for controlled local engineering exercises; production
readiness is not established.** Do not use passing local tests to waive the
requirements below. Revisit this review when the deployment boundary changes.

| Issue | Implemented and locally testable | Still required |
|---|---|---|
| #124 Recovery | Multiple API processes, asynchronous PostgreSQL replication and fenced promotion, three-member quorum queue, scheduled verified backups, transferred-copy PITR. The default rehearsal preserves the primary volume. | Independent-site storage and recovery; explicitly authorized destructive DR with achieved RPO/RTO; application activation and reconciliation after PITR; bounded WAL retention. |
| #125 Delivery | Immutable API/worker image pins, blue/green traffic switch, readiness and gateway validation, failed-start refusal, rollback on an expanded schema, SBOM and vulnerability gates. | Production release provenance verification, deployment enforcement and rollback policy in the selected environment. These exceed the isolated local strategy experiment. |
| #126 Operations | Synthetic traffic, local alert delivery, four scripted service incidents, business recovery checks, SLO comparison and sampled resource/cost scenarios. | Symptom-led human drill, secondary responder and escalation agreement, notification delivery outside the host failure domain, representative long-duration observation. |

A report is valid only for its exact source, image IDs, workload and failure
domain. Keep generated measurements outside Git. An incomplete run is a failed
verification attempt; retain it locally for diagnosis and fix the cause before
rerunning. Short-window SLO comparisons include injected failures and must not
be substituted for a rolling 28-day objective. A usage-price scenario is not an
actual infrastructure bill.

## Release blockers

| Area | Remaining work |
|---|---|
| Integrated security and operations | Combine authenticated traffic, telemetry, alerts, rollout and recovery in one explicitly defined profile. |
| TLS and topology | Reconcile certificate names, trust boundaries and rotation with multiple API instances, database failover and extra broker nodes. |
| Messaging | Reconcile the security lab's classic queue with availability-lab quorum queues before combining them. |
| Admission and capacity | Add continuous ingress SLIs and validate secure multi-replica capacity in one profile; bounded authenticated comparisons do not establish those guarantees. |
| Secret lifecycle | Complete renewal/distribution procedures for all credentials and CAs, beyond application database rotation. |
| Recovery | Establish off-site backup storage, retention limits, authoritative recovery timelines and service activation after restore. |
| Operations | Establish independent diagnosis, a secondary responder, escalation and alert delivery outside the service's failure domain. |
| Frontend | Add browser/component and authenticated failure/recovery tests; client-boundary tests alone are insufficient. |
| Production sizing | Define workload, data retention, sustained observation and cost assumptions before setting production capacity or availability targets. |

Do not infer independent-host or regional availability from containers sharing
one machine. Do not infer a sustained SLO or on-call capability from a short drill.
Use isolated lab projects and ports, and preserve existing data during tests.
