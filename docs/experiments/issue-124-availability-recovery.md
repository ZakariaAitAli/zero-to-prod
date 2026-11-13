# Issue #124 — local availability and recovery experiment

Date: 2026-10-18. Requirements were written in [ADR 0014](../adr/0014-local-availability-recovery.md)
before topology/testing. Method: [lab guide](../guides/local-availability-lab.md).
Harness: `tools/availability-lab-experiments`. Evidence:
[`evidence/issue-124/run1/`](../../evidence/issue-124/run1/).

This is one recorded run on one Docker/WSL host with synthetic data,
authentication off and plaintext internal hops. It does not establish
independent-host/site availability or production SLOs.

## Results

| Experiment | Observation | Objective / result |
| --- | --- | --- |
| Topology and backup schedule | standby streaming/read-only, same cluster identifier; three quorum members; three verified generations retained; backup health fresh | passed |
| API instance killed under business reads | 131 observed requests, one error; last error 0.143 s after kill; newly accepted job completed with one survivor | <=10 s local recovery; passed, not zero downtime |
| Quorum leader killed | all 25 messages confirmed before failure completed; retained node rejoined | 9.26 s to completion, <=60 s; passed |
| Majority unavailable | ten previously confirmed jobs retained; another job accepted into DB but not marked published without quorum; all 11 completed after quorum returned | correct confirmation/availability tradeoff; resume 9.79 s |
| Backup/WAL transfer | SHA256 copy verification and `pg_verifybackup`; selected base backup frozen before named point; copy separate from primary mounts | 14.96 s transfer; local access boundary, not real off-site |
| Fenced PostgreSQL promotion | paused replay showed stale reads, resumed/caught up before fencing; old primary stopped; promoted standby preserved known markers; new job completed | 5.55 s local application progress, observed marker RPO 0; <=120 s passed |
| Owned primary volume destroyed, named-point PITR | exact project label/mount/stopped-state checks; primary volume removed; restored only from transferred copy into fresh volume; good marker restored, later markers and post-failover job absent | 13.18 s **database** RTO, <=600 s; intentional point recovery, not zero-loss service DR |

The scheduler ran at a 30-second interval for this experiment (default 300 s).
It uses streamed WAL in physical backups and verifies SHA256 manifests before
publishing the latest pointer; three base generations were retained. Archive
WAL remains conservatively retained, so total backup storage is not bounded.

The destroyed volume was only
`zero-to-prod-availability-lab_postgres_data`, created by this run's new lab.
No other project's container, volume or network was deleted. The restored
cluster's mount list was checked to exclude original backup/archive/standby
and primary volumes, proving it did not accidentally read another live copy.
Physical backups contain privileged data and remain outside Git; evidence
contains identifiers/counts/hashes, never backup contents or credentials.

## Provenance, refusal checks and cleanup

Runtime source hashes are recorded before the run. After all assertions,
inspection found that default Compose stop omitted the recovery profile,
leaving that one container running. The public stop and harness `finally`
now explicitly include recovery/tools profiles. Running corrected stop against
that retained container left no project container running; see cleanup.json.
The seven failure experiments were not repeated for this cleanup dispatch fix.

Additional real refusal checks passed: normal `up` refuses a lab that has been
promoted, and starting the recovery entrypoint on the initialized retained
restore exits 1 without overwriting it. The fixture is one-shot; use the
[runbook](../runbooks/local-availability-recovery.md) for branch selection and
reseed/activation, not a casual rerun. All other HA volumes, transferred copy,
archive, network and protected state are kept; every HA container is stopped.

## What remains unproven

- The caught-up failover observed zero marker loss; asynchronous replication
  does not guarantee <=5 s RPO when WAL was never received. Paused replay
  demonstrated stale reads, not a destructive unreceived-WAL-loss experiment.
- This is operator fencing, not automatic leader election, quorum-based
  PostgreSQL control, split-brain-safe failover, or tested failback/rewind.
- Broker tests killed/stopped local processes, restored their retained nodes
  and tested majority behavior. Permanent disk/member loss, partitions,
  multiple hosts and correlated infrastructure failure remain unproven.
- The copy is on the same physical host/disk. Its independent mount/transfer
  boundary does not prove off-site disaster survival or host-admin isolation.
- The PITR endpoint recovered a database. Application reconciliation/redrive
  and full service activation time after PITR were not measured. Named-point
  recovery intentionally loses later commits and needs external-effect review.
- Recovery and the earlier promoted standby can both use timeline 2 on
  different branches. They remain fenced; restored archiving is disabled.
  Reusing the original WAL namespace or starting both writable branches would
  be unsafe. Select a branch and reseed into a new archive namespace.
- Three base generations are retained, but WAL/timeline pruning, storage
  monitoring, backup alerts delivered to an operator, longer endurance and
  host/site DR are still operational/production gaps.

No production-ready or universal HA guarantee follows from these results.
