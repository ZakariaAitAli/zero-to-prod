# Local availability and recovery lab

Issue #124, [ADR 0014](../adr/0014-local-availability-recovery.md),
[experiment](../experiments/issue-124-availability-recovery.md),
[recovery runbook](../runbooks/local-availability-recovery.md).

This is a **one-shot, isolated learning lab**, not a production deployment.
Authentication and internal TLS are disabled; all published ports bind to
loopback. It demonstrates process loss, manual database promotion, broker
quorum and a real physical backup/WAL restore. Its Docker host, disk,
proxies and operator remain shared failure points.

## Run

```sh
export HA_BACKUP_INTERVAL_SECONDS=30
bash tools/availability-lab-local up
# Wait for at least two verified backup generations (default production-like
# lab interval is 300 seconds; 30 seconds accelerates only this experiment).
python3 tools/availability-lab-experiments evidence/issue-124/new-run
bash tools/availability-lab-local stop
```

The output directory must not exist. Read the harness before running: it
intentionally destroys **only the new project's primary database volume**
after validating its exact name, project label, stopped container/mount and
recorded cluster identity. It does not prune Docker or delete other projects.
Other lab state is preserved for review. The stop command explicitly includes
the recovery/tools profiles, so restored database containers are stopped too.

Project `zero-to-prod-availability-lab` uses subnet 10.213.22.0/24; the driver
rejects Docker overlaps. Check host/VPN routes if networking changes. Loopback
ports: API 18083, RabbitMQ management 15675–15677, Prometheus 19092, Grafana
13002, Jaeger 16688. State/secrets live under
`~/.local/state/zero-to-prod/availability-lab` (private directory outside Git).
Do not print, copy into evidence, or commit runtime secret files or backups.
Only generated hashes/identities/counts and synthetic markers belong in Git.

The base observability Compose file is overlaid with two APIs, a streaming
standby, three broker nodes and routing proxies. Application database and
broker URLs point to the proxies. Those proxies are themselves single
instances. Three broker nodes use a shared private Erlang cookie; queue
creation waits for all three and explicitly chooses a three-member quorum.
Clustering alone would not replicate a classic queue.

## Backup and retention

A dedicated scheduler takes a physical `pg_basebackup` with streamed WAL and
SHA256 manifest checksums, then runs `pg_verifybackup` before publishing the
latest-success pointer. It retains three verified base backups. Partial
attempts and a crash's stale lock are preserved, not mistaken for successful
backups or removed automatically. Health becomes unhealthy if no verified
success occurred within three intervals; do not treat a running container
as proof of successful backups.

WAL is archived durably to a separate volume using immutable filenames and
content checks before accepting an existing name. This demonstration retains
WAL conservatively, including timeline history: **WAL storage is not bounded**.
A production policy needs required-WAL-aware retention, lag/backup-age and
storage alerts, and tested failure handling. Three base backups do not imply
three-backup-sized total storage. Restore cannot be promised if archive files
required since the selected base backup are missing.

The transfer job receives read-only source mounts and writes a separate
local copy. It hashes/verifies that copy. The recovery container mounts only
the transferred copy and a fresh output volume; it does not mount the old
primary, standby, original archive or original backup volume. This proves a
local transfer/mount boundary, **not physical off-site survival**: host/Docker
administrators still control all volumes and the disk is shared.

## After the experiment

Normal `up` is deliberately refused after the failover marker is written.
The recovery entrypoint also refuses an already initialized target rather
than overwriting the retained cluster. Do not remove those guards to make a
rerun convenient. Follow the runbook to choose a recovery branch and reseed;
use a separately reviewed fresh project/state for a new full experiment.
A bare `compose up` can bypass the wrapper and is not a safe failback tool.

The named-point restore deliberately discards later committed data. It proves
a database recovery boundary, not automatic application reconciliation or
zero-loss service DR. Before any application is reconnected, inspect/redrive
accepted jobs offline using ADR 0009 and reconcile lost requests, results,
messages and external effects. The experiment does not measure full
application-service RTO after PITR.
