# Local Production Readiness Review

Review date: 2026-10-18. Scope explicitly selected by the owner: local Docker
first, with limits documented. Owner: Zakaria Ait Ali; supervised drills only,
no 24/7 coverage and no assigned secondary responder. This is a review of
proposed stacked draft PRs, not a statement that main contains those changes.

**Verdict: controlled local experimentation only; not ready for unattended or
public production.** Mechanisms, tests and unproven guarantees are separated
below. Issue closure and production deployment require review of the relevant
changes and acceptance of the remaining risks.

| Area | Implemented | Tested evidence | Unproven / decision required |
| --- | --- | --- | --- |
| Trust boundary (#121) | Private ingress, authentication/authorization, server-verified TLS on application/datastore/identity hops, admission, protected application DB rotation | #139/#141 and earlier merged security experiments, browser runner and permission matrix | Two remaining plaintext hops; other secret and CA lifecycles; Windows desktop CA trust; combined deployment profile |
| Restore delivery safety (#130/#131) | Persisted delivery identity and explicit offline redrive | #140 destructive reused-ID/lost-delivery and refusal tests | Operational restore/reconciliation still requires deliberate fencing and audit |
| Observability (#122) | Structured logs, correlated async traces, metrics, SLIs/SLOs, runbooks and burn alerts | #142 fault scenarios and completion-clock regression | Production sampling/retention; traffic-free SLIs; scrape blindness; human diagnosis |
| Capacity (#123) | Workload/invariants, bounded arrival client, admission comparisons, replica/pool/broker budgets | #143 matrix and clean 600-second run, 12,000 completed jobs; failed broker-budget run retained | Internet clients, larger data, multi-host limits, 28-day SLO performance, cloud prices |
| Availability (#124) | Two APIs, async physical standby, manually fenced promotion, three-node quorum broker, verified scheduled backups and local transfer/PITR | #144 seven experiments; 5.55-second promotion and 13.18-second database-only PITR | Host/site loss, permanent broker-disk replacement, automatic fencing, arbitrary async RPO bound, full PITR application activation, finite WAL retention |
| Delivery (#125) | Immutable local image IDs, readiness gate, application rollback, source/image vulnerability gates and SBOMs | #145 final 1,004 requests without observed errors, bad candidate refused, real Go/x/net patch/rebuild/redeploy | Registry signatures/provenance, production approvals, arbitrary schema changes, authenticated combined rollout |
| Operations (#126) | Local ownership and incident runbook; local webhook receiver and short-window drill alerts | Four local automated drills plus owner-visible supervised alert; 20-minute traffic, resource and SLO measurement; post-run corrected-interface check | Independent human diagnosis/action and production MTTA, escalation, meaningful service history, 24/7 coverage |

## What the evidence supports

Individual labs demonstrate process/container failures and recovery, business
correctness under their recorded workloads, explicit trust boundaries and
mechanism failures. They do not establish that separately tested security,
HA, observability and delivery profiles compose into one production service.
A successful readiness endpoint, empty scanner report or green CI check is
not an independent production-readiness verdict.

The initial SLOs are availability 99.5%, latency 99% under 250 ms, and accepted
job freshness 99% under 30 seconds, over 28 days. Ten- or twenty-minute tests
compare observed performance with those thresholds; they cannot establish a
28-day SLO. Fault-drill failures must remain in the measured incident window;
reporting only recovered steady state would conceal the cost of failures.

## Cost and toil

The clean capacity run sampled approximately 1.265 CPU cores and 0.925 GiB for
the application plus telemetry stack: about 63.3 CPU-seconds and 0.0129 GiB-hours
per 1,000 successful jobs. These are local sampled resource units, not a cloud
bill or reserved-resource charge. Host, traffic generator, builds, stored
backups/WAL, energy, networking and operator time are excluded. Marginal
utilization and provisioned capacity must both be priced before deployment.
The operations run adds alert delivery and fault response; report its separate
resource sample rather than substituting the capacity figure.

Toil includes GPG unlocks during long experiments, manual CA issuance/renewal,
secret lifecycle coordination, manually fenced failover, backup verification
and retained WAL management, rebuilding/pinning/scanning releases, and runbook
maintenance. Automating dangerous steps without safe prerequisites would
reduce command count while increasing incident risk. Backups accumulating on
one disk and unbounded WAL retention remain concrete operational risks.

## Required decisions before changing the verdict

- Integrate the selected profiles and test their full authenticated business
  lifecycle, deployment and recovery together.
- Assign production coverage, a secondary responder, escalation and response
  expectations; rehearse symptom-first diagnosis with a human who did not inject
  the fault. A script's immediate acknowledgment is not human MTTA.
- Resolve or explicitly accept remaining transport, secret and CA lifecycle
  gaps, and test alert delivery beyond the service's own failure domain.
- Establish independent backup storage, a retention budget, restore activation
  and authoritative timeline procedures; test failure domains beyond one host.
- Define production load/data/retention and prices; accumulate enough history
  to evaluate the stated SLO window and capacity safety margin.
- Review and merge the staged work deliberately, preserving CI gates, signed
  provenance and evidence limits. No issue is closed by this document alone.

## Twenty-minute operations measurement

The run recorded 12,439 client requests, including 1,348 connection failures or
5xx responses across four known-fault automated drills and the separate
supervised API incident. Observed request availability was **89.16%**, below the
initial 99.5% threshold. 99.90% of recorded requests took at most 250 ms; fast
failures do not establish successful service. Of 3,695 accepted jobs created in
the timed window, all eventually succeeded and none remained accepted, but only
3,594 (97.27%) finished within 30 seconds, below the initial 99% threshold.
Injected outages are part of these figures, not removed to manufacture success.

A separately labeled period starting 60 seconds after the last supervised
business recovery recorded 7,060 requests, no connection/5xx errors and all
recorded requests below 250 ms. Its 2,353 accepted jobs all succeeded within 30 seconds; counts are retained separately. This
shows recovery under this load; it does not replace the incident-window SLI or
establish a 28-day SLO.

Sampled own-container resource usage over about 1,201 seconds totaled about
554.28 CPU-seconds and 0.2531 GiB-hours: means of 0.462 CPU cores and 0.759 GiB.
That is approximately 150.0 CPU-seconds and 0.0685 GiB-hours per 1,000 succeeded
jobs, including monitoring and alert delivery. This workload is smaller than
the capacity run, so its fixed telemetry overhead per job is higher. Samples
are estimates, not billing, reserved resource costs or a cross-environment
benchmark. Host/client/build/storage/network/energy/operator costs are excluded.

The four automated drills delivered notifications in approximately 31.29, 29.54,
28.99 and 49.01 seconds for API, database, broker and worker/backlog respectively.
Verified new-job recovery occurred in 38.73, 36.52, 36.93 and 52.16 seconds.
The clock starts before the stop command, not at an exact process-death event.
Script acknowledgment follows polling and is not human MTTA.

During the supervised incident the owner opened the local feed and pasted its
notifications. Local delivery took 36.02 seconds, the assistant observed a
response about 103.47 seconds after notification and 76.71 seconds after the
chat prompt, and a new job succeeded about 155.09 seconds after the stop action.
Those chat timings include delivery and assistant-processing delay. The owner
did not independently supply a diagnosis or recovery action; the assistant
interpreted the alert and restored the API. Visibility is tested; independent
on-call diagnosis, escalation and response coverage remain unproven.

The first attempt was interrupted before fault drills when static review found
incorrect timestamp column names in the final report query. Its cleanup query
then failed, and containers were explicitly stopped. Partial evidence and
manual cleanup are retained. Run 2 used the corrected `finished_at-created_at`
query and SQL cleanup guard; it is the source of the reported measurements.

The raw feed and internal-container generator links proved awkward for the
operator. After the timed run, a separate correction adds a browser active-alert
view and mounted runbook, loopback generator/Alertmanager URLs, and stable
private image-ID pins for future recovery commands. A separate interface check
exercises actual alert firing/resolution and database invariants; earlier
measurements are not attributed to this corrected interface. The original
receiver source and monitoring commands are retained in provenance.

Final cleanup: the corrected-interface check stopped all operations containers.
No volumes, networks, backups or private state were removed. Application images
remain pinned in private lab state; retained projects were not pruned. Known
local password values were scanned in raw/base64 form with a positive control;
no matches were found in this slice's evidence. This is a known-value scan,
not a universal secret-detection guarantee.
