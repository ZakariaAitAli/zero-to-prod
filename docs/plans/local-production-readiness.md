# Remaining production-engineering work — local Docker scope

User-selected scope (2026-10-18): local Docker lab first; document the limits.
This is a work tracker, not evidence that the capabilities below are complete.
Status as of 2026-11-14: the implementation PRs (#139–#146) are merged into `main`; follow-up tooling fixes landed in #151 (native Linux `verify`), #152 (rotation test under rootless Docker) and #153 (ingress base nginx 1.30.6). The combined review in draft PR #149 stays open as an evidence artifact; its integration defects D1–D3 are fixed on `main`, and gaps G1–G5 and G7 are unresolved design decisions.
No issue should close solely because a template or configuration exists.

| Sequence | Issue | Deliverable and proof | Status |
| --- | --- | --- | --- |
| 1 | PR #139 | Empty-certificate diagnostic and baseline-failure recovery, regression tests | Fixed in signed commit e85268e; merged in #139 (`1186814`) after merging `main` into the slice (D1 resolved in `api/main.go`) |
| 2 | #131 | Compare restore isolation options; reject stale deliveries after ID reuse; preserve legitimate duplicates | Implemented; 42-check destructive experiment passed; merged in #140 (`75010ea`) |
| 3 | #130 | Explicit, auditable offline redrive; recover the same blocked accepted job; preserve ADR 0003 | Implemented; offline refusal and redrive tests passed; merged in #140 (`75010ea`) |
| 4 | #121 | Rate limiting and overload failure; secret distribution/rotation ADR and experiment; remaining transport and threat-model gaps | Merged PRs #139 (`1186814`) and #141 (`a896c74`) implement application-hop TLS, admission, application DB rotation and threat model; remaining hops/secret and CA lifecycles stay open |
| 5 | #122 | Useful correlated logs, metrics, async trace, user SLIs/SLOs and alert investigation | Implemented (ADR 0012); lab experiment run 2 recorded in `evidence/issue-122/run2`; merged in #142 (`a2cbbe0`). Not covered: notification delivery and on-call (#126), production sampling; multi-replica aggregation exercised separately by #123 |
| 6 | #123 | Reproducible workload, baseline, saturation, API/worker scaling, queue/pool behavior and cost model | Implemented and merged in #143 (`525ed5c`): matrix/invariants recorded; 0.5-CPU broker sustained failure retained; clean 2-CPU broker ten-minute run completed 12,000 jobs within initial thresholds. Local scope, no production sizing claim |
| 7 | #124 | Local failure-domain topology, replication/failover, verified automated backups, PITR and measured recovery | Implemented local mechanisms (merged in #144, `2072166`); seven recorded experiments passed, database PITR 13.18 s and fenced promotion 5.55 s. Independent-site survival, automatic fencing and full service activation after PITR remain unproven |
| 8 | #125 | Immutable release identity, compare and exercise rollout strategies and rollback, patch/security workflow | Local experiment complete: final 1,004 requests without observed errors; failed candidate refused; real vulnerable baseline detected, Go/x/net patched, rebuilt API/worker passed scans; merged in #145 (`1f11d66`) after two follow-up fixes (Go 1.27.4 for newly reported standard-library advisories; Alpine base and `golang.org/x/net` for new image findings). Third-party lab images are still not covered by an image gate |
| 9 | #126 | Operator-owned runbooks, supervised game days and postmortem, sustained traffic and readiness review | Local ownership confirmed; four automated drills, owner-visible alert and 20-minute traffic measured. Full fault window misses availability/freshness thresholds; post-recovery subset passes. Independent diagnosis/escalation and production readiness remain unproven; merged in #146 (`9ed57c8`) |
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
