# Remaining production-engineering work — local Docker scope

User-selected scope (2026-10-18): local Docker lab first; document the limits.
This is a work tracker, not evidence that the capabilities below are complete.
No issue should close solely because a template or configuration exists.

| Sequence | Issue | Deliverable and proof | Status |
| --- | --- | --- | --- |
| 1 | PR #139 | Empty-certificate diagnostic and baseline-failure recovery, regression tests | Fixed in signed commit e85268e; all five CI checks passed |
| 2 | #131 | Compare restore isolation options; reject stale deliveries after ID reuse; preserve legitimate duplicates | Implemented; 42-check destructive experiment passed; PR review pending |
| 3 | #130 | Explicit, auditable offline redrive; recover the same blocked accepted job; preserve ADR 0003 | Implemented; offline refusal and redrive tests passed; PR review pending |
| 4 | #121 | Rate limiting and overload failure; secret distribution/rotation ADR and experiment; remaining transport and threat-model gaps | Pending |
| 5 | #122 | Useful correlated logs, metrics, async trace, user SLIs/SLOs and alert investigation | Pending |
| 6 | #123 | Reproducible workload, baseline, saturation, API/worker scaling, queue/pool behavior and cost model | Pending |
| 7 | #124 | Local failure-domain topology, replication/failover, verified automated backups, PITR and measured recovery | Pending |
| 8 | #125 | Immutable release identity, compare and exercise rollout strategies and rollback, patch/security workflow | Pending |
| 9 | #126 | Operator-owned runbooks, supervised game days and postmortem, sustained traffic and readiness review | Pending |
| Umbrella | #120 | Reconcile the capability map and final readiness verdict against evidence | Open until underlying work is assessed |

Use isolated project names, explicit free subnets, and separate ports. Preserve
retained projects and volumes. Record code versions, method, positive controls,
failures, reruns, and cleanup for every experiment. Never revise historical
evidence to claim that it tested later code.

A single-host lab can prove process/container recovery and local behavior.
It cannot prove independent-host or regional availability, off-site disaster
survival on the same disk, internet ingress safety, cloud billing, or human
on-call readiness without an operator exercise. Sustained traffic duration,
workload, SLO targets, and cost assumptions must be explicit before measuring.
