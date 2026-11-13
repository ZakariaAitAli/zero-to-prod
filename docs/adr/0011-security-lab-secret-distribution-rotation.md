# ADR 0011 — Local secret distribution and recoverable database rotation

## Status

Proposed. Addresses #121's secret distribution/rotation decision and one actual
rotation experiment. Scope is local Docker; no production secret-manager claim.

## Alternatives

| Option | Assessment |
| --- | --- |
| Credentials in source, build args, or images | Couples artifacts to environments and exposes values through history/layers; rejected |
| Credentials in command arguments | Visible in process inspection and often logs; rejected |
| Environment variables | Convenient, but Docker metadata and process environments expose values; existing development defaults stay development-only |
| Protected files mounted per consumer | Existing local boundary; no external service, values outside Git, explicit consumer lifecycle; selected for this lab |
| External secret manager with workload identity | Better access auditing, versioning and dynamic credentials; requires a separate identity/distribution design and deployment environment |
| Overlapping database login roles | Can support staged rotation without terminating every old session; changes grants, role selection and pool ownership |
| Change one role and recreate its consumer | Simple, bounded scope, disruptive; selected for this local experiment |

## Distribution

Generate source values with cryptographic randomness under a mode-700,
owner-validated state directory outside the repository. Derived files expose
only the settings a service needs. Mount them read-only into their consumer;
do not bake state into images. Mode-644 consumer files are protected by their
host ancestors and container-specific mounts. Source values not required by a
container stay mode 600. CA keys stay host-only. Docker/host administrators
remain trusted; these mounts do not defend against a compromised Docker daemon.

The API and worker read credential files at startup, not on every request.
Atomic replacement of a file bind mount does not replace its inode inside an
existing container. Recreate that consumer after changing a derived file. The
PostgreSQL bootstrap source file is updated in place while its operator scripts
are quiescent, so its existing mount sees the new value as well.

## Rotation decision

`tools/security-lab-local rotate-app-db-password` rotates only
`postgres-app-password`, the API's database login. It requires marked,
owner-controlled state, a running target PostgreSQL, and an exclusive command
lock. Do not run start/init/other operator changes concurrently.

Persist a random planned value in a private pending journal and fsync its file
and parent directories **before** changing the server role. Change the role
through PostgreSQL's trusted local administrator, passing SQL on standard input,
with statement text logging disabled for that session. No value is printed or
passed as a process argument. Update the source password and atomically replace
the derived API URL. Terminate that role's existing connections to the target
database: password replacement alone does not revoke authenticated sessions.
Recreate the API, verify its database readiness, and only then remove the known
journal files. Do not delete unknown journal data.

On failure, keep the journal and fail. Rerunning repeats the same planned value
and finishes consumer recreation; it never silently restores the compromised
old password. There is no automatic rollback to old credentials. A missing or
corrupt journal after a server change requires operator recovery, not guessing.
This is process-interruption recovery; host/disk loss still requires protected
state backup and a separate recovery procedure.

## Observed behavior and limits

The experiment holds a real connection using the old password, injects failure
before API recreation, checks that the old backend was terminated and the old
password is refused on a new verified-TLS connection, and checks the new one
works. Resume preserves the planned value, recovers API readiness, and removes
the journal. A post-rotation Chromium run signs in, creates/processes work,
signs out, and exercises viewer denial. Source values and old/new passwords are
searched in command output and all nine service logs with a positive control.

The image audit streams every saved image layer, metadata and history without
extracting archive paths; it also scans source files and Git objects for known
configured values and private-key PEMs, raw/base64. An export failure or an
unsupported compressed layer makes the audit incomplete, never clean. This is
known-value detection, not a universal credential classifier.

This rotation can interrupt requests and roll back in-flight database
transactions. It does not promise zero downtime or rotate machine/client
secrets, user passwords, Valkey credentials, cookie keys, or CA roots. Those
need distinct propagation and revocation procedures; changing Keycloak's import
file does not update an existing realm. Certificate renewal is a separate
lifecycle, and a Valkey restart signs users out. The migrator's documented
credential-bearing URL argument remains a gap.

See the [experiment record](../experiments/issue-121-secret-rotation.md) and
[security lab guide](../guides/local-security-lab.md).

Sources: [PostgreSQL ALTER ROLE and password logging](https://www.postgresql.org/docs/18/sql-alterrole.html),
[backend termination](https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-ADMIN-SIGNAL),
and [Docker image save](https://docs.docker.com/reference/cli/docker/image/save/).
