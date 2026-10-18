# Local availability/recovery runbook

Scope: [availability lab](../guides/local-availability-lab.md), issue #124.
This is a tested-mechanism runbook, not evidence of an unassisted human on-call
exercise (#126). The harness knows the injected fault.

## API process loss

Check the gateway's request failures, live API replica count and accepted-job
state. One process can disappear while another answers; in-flight failures
are still possible. Do not retry ambiguous create requests blindly. Read the
existing item/job before deciding whether an acceptance must be retried.
Restart only the owned failed API instance and verify a newly accepted job
completes. The recorded run saw one brief 502, not zero downtime.

## Broker node or majority loss

Use management on a surviving loopback endpoint (15675/15676/15677); read the
runtime administrator password locally from its protected file, never place
it in a URL, terminal history or evidence. Inspect quorum leader, members and
live nodes, queue ready/unacked counts, outbox age and publisher confirmation
latency. The normal publisher/worker use restricted accounts, not this admin.

One failed node should leave a majority. Restore/rejoin the same node using
its retained data and identity; do not reset cluster membership or delete
volumes to silence alarms. With fewer than a majority, publishing/consumption
can stop. Accepted work should remain in the outbox without being falsely
marked confirmed. Restore quorum, verify the original confirmed message/job
identities and drain. Never convert the queue to classic or weaken confirms
as a recovery shortcut. Permanent member replacement requires a separately
reviewed procedure; this lab tested rejoining retained nodes only.

## PostgreSQL failover

1. Identify the current writable primary, system identifier, replay state and
   lag. A readable replica may be stale. Asynchronous replication does not
   bound unreceived WAL loss simply because a previous failover lost nothing.
2. Fence the old primary **before** promotion. In this lab the owned container
   is stopped and its state checked. This is operator fencing, not STONITH or
   an automatic split-brain-safe consensus system.
3. For a planned/caught-up failover, confirm known commit markers/replay LSN
   before calling `pg_promote`. Record which guarantees become unavailable
   if the source is lost before that check.
4. Change only the allowlisted database target in the protected
   `proxies.conf`, validate Nginx configuration, then reload. Recheck readiness
   and submit/process a new synthetic item. The proxy is still a SPOF.
5. Keep the old source fenced. Do not run normal `up`; the wrapper refuses it
   after promotion. The retained old source cannot simply rejoin as writable.

Failback is a data-lineage decision: select the authoritative branch,
reconcile external effects and unreplicated commits, take a verified backup,
and reseed a standby from that branch (or use a separately proven rewind
procedure with its prerequisites). Automatic failback/rewind was not tested.

## Backup failure or stale health

Inspect the last verified success, scheduler log, primary reachability and
free space. A `.partial` directory is not a backup. A stale `.backup-lock`
requires checking that no backup process remains before any narrowly scoped
manual removal; do not delete it merely because a retry wants the lock.
Retain the last verified generation while fixing the cause. Independently
verify a transferred copy and boot a test restore before trusting recovery.
WAL retention and archive/backup-age alert delivery still need production
operational design; the lab health check is not an on-call notification.

## PITR / destructive local recovery

Keep APIs/workers offline. Choose the restore point and quantify the commits
that will be deliberately discarded. Select a base backup preceding that
point; a newer backup cannot be rolled backward to an earlier target. Verify
the complete required WAL and hashes, then restore into an empty owned volume.
Check system identifier, recovery completion and known good/bad markers.
Measure database RTO separately from application activation/reconciliation.

The recorded copy predates promotion. Both the promoted standby and PITR
restore can independently choose timeline 2 with different history: matching
system/timeline numbers alone do not prove identical lineage. The restored
cluster has archiving disabled and all old writers are fenced/stopped. Do not
merge its WAL into the old archive or restart either writable branch beside
the other. A chosen restored service needs a new archive namespace and
reseeded replicas, plus a tested application activation plan.

Run the [offline restore-delivery procedure](../guides/restore-delivery-recovery.md)
for accepted work before reconnecting runtimes. Delivery UUID checks protect
against stale messages targeting reused IDs; they do not resurrect discarded
commits or reverse external effects. Reconcile requests and effects explicitly.
The lab demonstrated PostgreSQL PITR in about 13 seconds, not a complete
service restart in that time. Independent-host/site DR remains unproven.
