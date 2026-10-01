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

A processing job reaching `succeeded` does not imply that the Work Item business status becomes `done`.

The current worker still performs the existing minimal processing behavior.

Work Item business status and processing-job state are separate models.

## Related documentation

- `docs/architecture/work-items.md`
- `docs/guides/local-work-items.md`
- `docs/adr/0002-work-items-web-ui-boundary.md`
