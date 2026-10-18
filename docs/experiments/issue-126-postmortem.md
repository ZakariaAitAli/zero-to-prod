# Postmortem: broker budget exhausted during sustained local capacity run

Incident type: controlled local experiment, not a customer production incident.
Owner: Zakaria Ait Ali. Reviewed during #126 using the retained #123 run 2 and
run 3 records. No human MTTA is inferred from the automated experiment.

## Impact and detection

At 20 accepted jobs/second for 600 seconds, the 0.5-CPU RabbitMQ budget could not
sustain serial publisher confirms. All 36,000 HTTP requests and 12,000 jobs
eventually completed, but only 65.48% of jobs completed within 30 seconds, against
the initial 99% freshness target. Job p99 was approximately 246 seconds. The
absence of HTTP errors concealed the asynchronous user impact. Inspecting the
outbox and confirmation timing exposed the bottleneck; an empty broker queue
would not establish that no work was waiting in the publisher outbox.

## Contributing conditions

- A CPU quota constrained the broker even though application workers had spare
  capacity; scaling workers alone would not remove that bottleneck.
- The publisher serializes confirms, so increased confirm delay caps throughput.
- The freshness histogram records completed work; stuck work also needs an
  oldest-accepted-age alert to avoid being invisible before completion.
- The first run used an insufficient resource budget for the requested workload.
  The experiment correctly retained its failed SLO result rather than counting
  eventual completion as a success.

## Recovery and validation

After load stopped, raising only this lab's broker quota to 2 CPUs increased
observed drain throughput beyond 60 jobs/second. The backlog drained in about
196 seconds. This was a diagnostic intervention, not a successful run at the
original resource budget. A separate clean 600-second run with the 2-CPU broker
completed 12,000 jobs with every measured job below 30 seconds, job p99 about
0.521 seconds and final drain about 0.68 seconds. It also included the corrected
completion clock, whose dedicated regression distinguishes final-update time
from transaction-start time.

## Corrective actions and verification

| Action | Owner | State / evidence |
| --- | --- | --- |
| Preserve the failed run and quota intervention in capacity evidence | Project owner | Done in #143 |
| Use a measured 2-CPU broker budget for the clean comparison | Project owner | Tested once in #143; no production sizing claim |
| Observe outbox age, oldest accepted job and confirm latency, not only HTTP status/queue depth | Project owner | Mechanisms in #142; local operational delivery exercise in #126 |
| Reject a global zero-downtime/freshness claim from eventual completion | Review owner | Explicit in capacity and readiness records |
| Establish headroom across larger data and longer representative workload | Project owner | Open; #123/#126, local results insufficient for production |
| Test notification/escalation outside the service failure domain | Project owner | Open; #126, local receiver cannot prove this |

## Lessons and operational burden

The bottleneck was a resource allocation and observation problem, not evidence
that the broker or worker guarantees should be weakened. Do not disable confirms,
purge accepted work or hide failures by resetting the database. The useful next
step is to measure the current limiting stage and preserve business invariants.
Retained data, versions, CPU budgets and interventions make the comparison
reviewable. Repeating a successful small test adds less assurance than testing
the remaining failure domains and operational response.

## Alert-quality findings from the supervised operations drill

The owner opened the local notification endpoint and pasted the history. This
confirmed visibility on Windows, but the endpoint presented resolved and active
incidents together as raw JSON, and generator links named internal container
hosts. The response did not include an independent diagnosis or chosen recovery
action; those skills remain unproven. The assistant interpreted the active API
alert and performed recovery, with a new succeeded job as proof.

Corrective action: provide a browser view of latest active alerts with runbook
links, retain raw events separately, and configure loopback external URLs for
Prometheus and Alertmanager. Validate the corrected view and pipeline separately
after the timed run; do not attribute the earlier drill to the corrected UI.
Additional follow-up remains a human-led symptom-first drill with diagnosis and
action recorded, plus independent escalation/coverage.
