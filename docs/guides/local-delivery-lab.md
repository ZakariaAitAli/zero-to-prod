# Local immutable delivery and patching

Refs #125; design: [ADR 0015](../adr/0015-safe-local-delivery.md).
This optional authentication-disabled experiment uses its own Compose project,
private secrets, volumes and subnet 10.213.23.0/24. The gateway binds loopback
18084. It does not combine the security lab's authentication/TLS profile with
availability or delivery guarantees. The host, gateway and database remain
single points of failure.

From the repository root:

```sh
tools/delivery-lab-local build
tools/delivery-lab-experiments /absolute/new/evidence-directory
```

The runner always stops the lab in its cleanup path, keeping volumes, network
and secrets. An interrupt that kills the runner itself may require
`tools/delivery-lab-local stop`. No prune or daemon configuration change is used.

`build blue` or `build green` builds one candidate. Each release contains the
source commit plus the hash of its tracked application diff and slot name.
The private `releases.env` records immutable image IDs; keep the corresponding
Git patch/source and scan reports as provenance for a dirty-tree experiment.
For a real release, require a clean signed source commit, build once in trusted
CI, publish by registry digest and sign/verify provenance before promotion.
Mutable tags in the build workspace are conveniences; containers use recorded
IDs. Images and secrets are separate artifacts.

The gateway changes only after the candidate answers `/ready` on its intended
port and the generated gateway configuration passes validation. Nginx reload
keeps existing workers serving while new workers take over. Readiness is a
prerequisite, not proof of business correctness. Business create/process/result
checks supplement it. There is no automatic semantic rollback or production
release controller. The two slots run the same application code with different
release identities for the baseline; the patched candidate changes the Go
runtime and dependency versions.

The runner adds a nullable `delivery_annotation` column to the shared database,
then exercises both binaries and rollback against that expansion. The column is
an experiment fixture retained in this project's volume; it is not a new
application feature/migration. This proves tolerance of that specific additive
change, not arbitrary schema compatibility. Never contract the schema while
old binaries/messages or the rollback window remain. Roll back the application
only; destructive data restoration is a separate operation with data-loss risk.

For each candidate:

```sh
cd apps/work-items
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
govulncheck ./...
go test ./...
cd ../..
tools/delivery-image-gate IMAGE_ID /absolute/new/scan-directory
```

Use the Go toolchain declared in go.mod. Govulncheck must itself be built with
a compatible toolchain. Plain-text mode fails on reachable findings; JSON mode
alone must not be used as an exit-code gate. The image gate saves a local image
and gives the pinned scanner read-only access to that archive, with no Docker
socket. It emits CycloneDX SBOM, high/critical vulnerability report, scanner
identity, image identity, database freshness metadata and exit codes. Scanner
errors and failed report creation fail closed. CI runs source and both image
gates and retains image reports for 14 days. The dependency service/database
is a required external dependency; unavailability blocks release validation.

Patch workflow: investigate vendor advisories and exposure; update module and
compiler/base pins; rebuild rather than modifying running containers; test and
rescan the exact images; deploy candidate under representative traffic; verify
release identity and business outcomes; retain the previous compatible image
for an explicitly assessed rollback. A previous image known vulnerable is only
a controlled local rollback target here, not a safe production fallback.

SBOMs describe contents, not trust. Scanner coverage, reachability analysis and
severity rankings have limits. This local experiment does not scan every retained
lab image, frontend dependency, host package or remote service. It does not
provide registry signatures, isolated build attestation, dependency-review
approval, production credentials or an enforced GitHub branch-protection rule.
The module-level unmaintained OpenPGP advisory has no fixed version; the
application does not import that package, as recorded by the verbose source
scan. New imports and newly published advisories require reevaluation.
