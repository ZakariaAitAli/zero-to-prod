# Run 1: stopped after E4

Run 1 was the first execution of `tools/observability-lab-experiments`. It was
stopped at 2026-10-18T14:27:13Z, during E5, because it had exposed defects in
the lab and the harness that made later results unreliable. The JSON files
here are kept unchanged as the record of what that run observed; run 2 is the
clean run of the corrected files.

Defects found by this run (all fixed before run 2):

1. Freshness recording rules matched `le="30"`, but Prometheus 3 stores the
   bucket as `le="30.0"`, so the freshness SLI had no data. Fixed in the rules
   and reloaded at about 14:02 UTC, during E1.
2. E2 limited the API to 0.02 CPU at 4 iterations per second. The API used
   about 0.014 CPU, so it was never saturated and the latency alert correctly
   did not fire. Run 2 uses 0.01 CPU at 8 iterations per second.
3. E2 restored with `docker update --cpus 0`, which left the 0.02 limit in
   place. The API stayed limited through E3 and the start of E4 until it was
   raised to all 20 Docker VM CPUs at 14:16:24 UTC. The harness now restores
   to the VM's CPU count and checks it.
4. Waiting for "all alerts resolved" also waited for ticket-severity slow-burn
   alerts, whose 30-minute window keeps them firing long after recovery. The
   harness now waits only for pages and records open tickets.
5. The E4 trace lookup used the newest error log, whose spans had not reached
   Jaeger yet (SDK and collector batching). The harness now retries over the
   logged trace IDs.

The API container was left stopped by the interrupted E5 and was recreated
from Compose before run 2.
