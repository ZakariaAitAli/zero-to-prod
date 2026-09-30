# Archify experiment: Work Items asynchronous processing

**Status:** experiment output. Not architecture documentation and not a source of truth.

The authoritative sources are the code, migrations, and tooling at the pinned revision, explained by:

- [Work Items architecture](../../../architecture/work-items.md)
- [Reliable asynchronous processing](../../../concepts/reliable-asynchronous-processing.md)
- [Reliable asynchronous processing experiments](../../../sprint-03/reliable-async-processing.md)

## Question

Does an Archify architecture diagram make the Work Items durability, asynchronous, acknowledgement, and recovery boundaries easier to understand than the existing Markdown documentation?

## Provenance

| Item | Value |
|---|---|
| Repository revision cited | `76b8b87bf483a737ac66285e45a67d9b210acf66` |
| Archify | `tt-a1i/archify` at `d5a1333d7447c866a765adac7d4d062f2f02e4d2`, run from a disposable clone |
| Diagram type | `architecture` |
| Source link mode | `local-only` (SSH origin; Archify only builds web links for HTTPS origins) |
| Update check | disabled with `ARCHIFY_UPDATE_CHECK_DISABLED=1` |

## Files

| File | Role |
|---|---|
| `work-items-async.architecture.json` | Hand-authored diagram input with source citations |
| `work-items-async.architecture.html` | Rendered, self-contained diagram |
| `*.finalize*.json`, `*.delivery.json`, `*.browser-check.json` | Archify gate receipts for the rendered file |

## Result of the Archify gates

| Gate | Result |
|---|---|
| validate (showcase) | pass |
| deliver | pass |
| check, including source-reference verification | pass — 22 references resolved at the pinned revision |
| browser-check | skipped — no Chrome/Chromium in this environment |

## What "verified" means here

Two negative controls were run outside the repository:

| Control | Change | Archify result |
|---|---|---|
| A | A citation pointing at a line past the end of the file | Rejected (`repository-evidence/line-out-of-range`) |
| B | A false edge, "worker updates `work_items.status`", citing real lines | Accepted by every gate |

Archify verifies that a cited location exists at the pinned revision. It does not verify that the diagram's claim is true. Correctness of the diagram depends entirely on the author reading the code.

## Recorded architectural gap

The diagram shows a current, repository-backed recovery gap. It is documented here, not fixed:

- `tools/postgres-backup-local` backs up only `public.work_items` (`pg_dump --data-only --table=public.work_items`).
- `processing_jobs` and `outbox_messages` are outside that backup scope.
- A `202 Accepted` is durable against API restart, worker restart, and RabbitMQ outage, but destructive PostgreSQL loss followed by restore does not preserve accepted-but-unfinished asynchronous work.

## Reproduce

From the repository root, with Archify cloned to `$ARCHIFY`:

```bash
ARCHIFY_UPDATE_CHECK_DISABLED=1 node "$ARCHIFY/archify/bin/archify.mjs" finalize architecture \
  docs/experiments/archify/work-items/work-items-async.architecture.json \
  docs/experiments/archify/work-items/work-items-async.architecture.html \
  --repo-root . --quality showcase --json
```

`browser-check` refuses to replace an existing receipt from an earlier run; remove `work-items-async.architecture.browser-check.json` before re-running.

## Exit decision

Pending (spec principle P15: keep, generalize, replace, or remove).
