# Current gaps and constraints

Work Items is a local reference workload. Its optional labs exercise individual
engineering concerns; they are not a fully integrated production deployment.

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
