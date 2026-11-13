# Supervised local operations lab

Owner and response boundaries: [runbook](../runbooks/local-operations.md).
[Readiness review](../experiments/issue-126-readiness-review.md) and
[postmortem](../experiments/issue-126-postmortem.md) distinguish observed and
unproven outcomes.

This lab is separate from retained security, recovery, observability, capacity,
availability and delivery projects. It uses `zero-to-prod-operations-lab`, subnet
10.213.24.0/24, loopback API 18085, notifications 18086, Prometheus 19094,
Alertmanager 19095, Grafana 13004 and Jaeger 16690. Authentication is disabled and
internal hops are plaintext, as in the controlled capacity/observability lab;
this is not a public deployment profile.

Prepare patched application images and verify their reports first:

```sh
tools/delivery-lab-local build
tools/delivery-image-gate zero-to-prod-delivery-api:green /absolute/new/api-scan
tools/delivery-image-gate zero-to-prod-delivery-worker:local /absolute/new/worker-scan
tools/operations-lab-local up
tools/operations-lab-experiments /absolute/new/operations-evidence 1200
tools/operations-lab-report /absolute/new/operations-evidence
```

The application images come from the delivery build; the operations lab does not
silently compile another application. Record the exact IDs, source and scans.
The first start persists selected IDs in private `images.env`; subsequent
recovery commands reuse those pins even if build tags move. Selecting another
release requires a stopped lab and explicit review of its private pins. Secrets are
created in the private `~/.local/state/zero-to-prod/operations-lab` directory,
never in the repository. Builds, clients and earlier labs are outside the timed
resource sample. Stop preserves all volumes, private state and evidence.

The runner sends a nominal four business iterations per second sequentially;
request latency/outages reduce actual arrival rate. Each iteration creates,
requests processing and reads items. It tests API, PostgreSQL, broker and worker
stop/restart under traffic. For each fault it waits for delivered firing alerts,
restarts the affected service, proves a newly accepted job succeeds and waits
for a delivered resolution. These are known-fault scripted drills, not human
MTTA. CPU and memory snapshots are sampled every ten seconds for this project's
containers only. Readiness and create/process/result verification supplement
telemetry, whose scrape cannot see every failed user request.

Existing SLO burn-rate rules retain their original thresholds. `LocalDrill*`
rules use short windows solely to exercise the notification path in bounded
local runs. Prometheus sends to Alertmanager, which posts to an isolated local
receiver. It sends no email, chat, phone notification or third-party message.
A browser gets an active-alert page; `/events` retains raw notification history.
Neither a blank page nor absence of alerts proves the monitoring pipeline is
healthy. The receiver and monitoring share the host and are not independently
available during host loss.

A supervised human exercise should begin from the alert or user symptom without
revealing the injected component. Record the alert, operator acknowledgment,
diagnosis and chosen action, then verify actual business recovery and resolution.
Use a bounded automatic recovery deadline for an unattended drill; expiry is
not operator acknowledgment. Reopening and pasting the alert feed establishes
visibility, not independent diagnosis. No production on-call or L6 claim follows.

Always stop with `tools/operations-lab-local stop` after a manually managed drill.
If a runner is killed or its cleanup fails, inspect this project's state and
stop it explicitly. Never prune unrelated projects or remove retained volumes.
