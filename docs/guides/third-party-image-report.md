# Third-party image report

`tools/third-party-image-report` lists the vulnerability findings of the digest-pinned third-party
images that the labs run (PostgreSQL, RabbitMQ, Keycloak, oauth2-proxy, Valkey, nginx, Grafana,
Loki, Jaeger, the OpenTelemetry collector, Prometheus, Alertmanager, the PostgreSQL exporter and
the Python receiver image).

It is **informational**. The release gate for the images this project ships (API and worker) is
`tools/delivery-image-gate` in CI, and it fails on HIGH or CRITICAL findings. This report never
fails because of findings, so an unfixed third-party finding is visible without blocking unrelated
work. It does not analyse reachability: a finding is a candidate, not proof of exploitability.

## Run it locally

```bash
tools/third-party-image-report /absolute/new/report-directory
```

The directory must not exist. For each image the tool pulls it by digest, saves it to a temporary
tarball and scans that with a Trivy container pinned by digest (no Docker socket is mounted into
the scanner). The vulnerability database is cached in the named Docker volume
`zero-to-prod-trivy-cache`; remove it with `docker volume rm zero-to-prod-trivy-cache` when you no
longer need it.

Output: one JSON report per image, `summary.md` (a table of CRITICAL, HIGH and fixable counts per
image) and `summary.json`. Exit status 0 means the report was written; 2 means a pull or scan
failed (the partial report is still written).

`tools/third-party-image-report --self-test` checks the parser and renderer without Docker.

## In CI

`.github/workflows/third-party-image-report.yml` runs the same tool on demand
(`workflow_dispatch`) and weekly, publishes the table to the job summary and keeps the reports for
30 days. It is separate from the required CI gate.

## Using the result

Deciding which findings are acceptable for a local lab, which images to bump (and re-verify with
the lab suites that use them) and whether any image should become a gate is an owner decision; it is
the open question behind issue #125 and the integration gap about ungated images.
