# Work Items Web

Browser client for the Zero-to-Prod Work Items reference system.

## Purpose

This application provides the browser-facing Work Items interface while keeping the existing Go API, PostgreSQL, transactional outbox, RabbitMQ, and worker architecture intact.

The current UI can:

- list Work Items;
- create Work Items;
- start asynchronous processing;
- observe processing-job state;
- poll accepted jobs until they reach `succeeded` or `failed`;
- represent loading, empty, and request-failure states.

## Stack

- React
- TypeScript
- Vite
- Tailwind CSS
- shadcn/ui
- pnpm

## Local development

The frontend expects the Work Items API to be available at:

http://127.0.0.1:8080

From this directory:

corepack enable
pnpm install --frozen-lockfile
pnpm dev

Open:

http://localhost:5173

The browser calls API paths under `/api/*`.

Vite proxies those requests to the local Go API during development.

This avoids enabling a broad API CORS policy solely for the local development workflow.

The Vite proxy is development-only and does not define future production ingress, TLS, hostname, CDN, reverse-proxy, or CORS architecture.

## Checks

Run:

pnpm lint
pnpm build

Frontend-only repository changes have their own CI validation path and do not require the PostgreSQL/RabbitMQ integration suite.

## Current processing semantics

Work Item business status (`pending`, `done`) and processing-job state
(`accepted`, `succeeded`, `failed`) are separate models.

A job reaching `succeeded` means the worker committed a title-analysis result,
the Work Item became `done`, and the job became `succeeded` in one transaction.
`GET /items` returns that result, and the UI shows it on the item. A `failed`
job leaves the item `pending`, so it can be processed again.

Processing requests can return `409 processing_already_active` with the active
job ID, or `409 work_item_already_done`. The UI follows the active job or
refreshes the completed item.

See [ADR 0003](../../../docs/adr/0003-work-items-async-success-semantics.md).

## Related documentation

- `docs/architecture/work-items.md`
- `docs/guides/local-work-items.md`
- `docs/adr/0002-work-items-web-ui-boundary.md`
