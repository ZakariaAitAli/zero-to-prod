# Issue #121 — recoverable API database password rotation

Date: 2026-10-18. [ADR 0011](../adr/0011-security-lab-secret-distribution-rotation.md).
Local Docker scope, same separate `zero-to-prod-security-lab-limit` lab used
for ingress admission and real authentication regression. No retained lab was
started or changed.

## Rotation and failure recovery

The [12-check test](../../evidence/issue-121/secret-rotation/rotation.json)
connected with the old password over verified TLS and held that backend open.
It injected a Docker consumer-recreation failure after changing the database
role, updating source/derived files, and terminating old sessions. It confirmed
that the pending private journal retained the planned new value, the held old
backend was gone, the old password failed a new verified connection, and the
new password succeeded. Rerunning the public command resumed with the same
planned value, recreated the API, verified database readiness and removed the
journal. A positive-controlled known-value scan found no source value or old/
new password in command output or logs from any of the nine services.

The final run includes fsync of the planned-value file, its journal directory,
and the containing secrets directory before server mutation. This is a real
process-failure/resume experiment, not a simulated disk or power-loss test.
Concurrent operator actions and loss of the protected state volume remain
outside the guarantee. Rotation is deliberately disruptive; no zero-downtime
claim is made. Normal DB transaction and outbox guards handle retries; no new
processing job is created by rotation.

## Post-rotation browser flow

[Chromium results](../../evidence/issue-121/secret-rotation/browser/browser-results.json):
**17 passed, 7 observations, 0 failed**. Verified public TLS, actual Keycloak
sign-in, UI create/process to done, sign-out/password prompt, viewer denial,
and an untrusted-CA negative control passed. The runner/script are copied into
this experiment's directory so historical application-TLS evidence is not
overwritten. Chromium is headless in a container, not a Windows desktop test.
The browser run followed a completed rotation before the final journal-fsync
refinement; that refinement was covered by the final 12-check rotation rerun.
It changes host journal durability, not the running browser/API configuration.

## Artifact and source audit

[Image/source audit](../../evidence/issue-121/secret-rotation/image-audit.json):
nine image identities, 15 source secrets, private-key PEM blocks, raw/base64
patterns, image metadata/history and every saved layer, tracked/nonignored
source files, and Git objects. All known-value match counts were zero; the
scanner positive control passed. Archives were streamed without extraction or
execution. Unsupported compression or export failure is an incomplete audit,
not a clean result. The final audit used the state after the final rotation.

The audit detects configured known values, not all conceivable credentials or
encodings. It does not establish absence of a vulnerability or compromised
build tool. The migrator's credential-bearing URL argument, other secret types'
rotation, secret-manager integration, CA consolidation and automated certificate
lifecycle remain explicit gaps.

## Cleanup

The admission fixture removed its own containers/network/state. The browser's
temporary npm/trust directory and test password files were removed. The separate
nine-service `-limit` lab is stopped with its three volumes, six explicit-subnet
networks (10.213.7–12.0/24), and protected state retained for review. Custom
`:limit` image tags keep these builds separate from retained lab tags. The
rotation lock file remains inside the private secrets directory and contains
no value; no pending password journal remains. Retained projects and Docker
daemon settings were not modified.
