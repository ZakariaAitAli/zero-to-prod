# ADR 0002 — Work Items Web UI boundary

## Status

Accepted

## Context

Work Items already had:

~~~text
Go API
  ↓
PostgreSQL
  ↓
transactional outbox
  ↓
RabbitMQ
  ↓
worker
~~~

The system did not yet have a browser-facing client.

It also lacked a read endpoint that allowed a client to observe an accepted processing job until it reached a terminal state.

The engineering problem was:

> How should a browser-facing Work Items client be introduced without changing the existing asynchronous-processing semantics?

The goal is to broaden the reference system with a real Web UI boundary without turning Zero-to-Prod into a frontend-product project.

## Decision drivers

The implementation should:

- introduce a distinct browser-facing application artifact;
- preserve the existing Go API and asynchronous-processing architecture;
- make asynchronous processing visible to a user;
- introduce a real browser-to-API networking boundary;
- exercise frontend build and CI behavior independently;
- remain small enough that frontend product work does not dominate the project.

## Alternatives considered

### Separate browser client

A standalone browser client built independently from the Go API.

Representative implementations include React, Vue, and Svelte clients.

This model introduces:

- a distinct frontend artifact;
- an independent frontend build lifecycle;
- independent frontend CI;
- a browser-to-API networking boundary;
- client-side loading and failure handling;
- origin and ingress decisions;
- a future path toward independent static-asset delivery.

The main trade-off is another application toolchain and lifecycle to operate.

### Go-rendered UI with progressive enhancement

The Go application could render HTML directly and optionally use progressive enhancement such as htmx.

This would provide:

- fewer independently built artifacts;
- a smaller JavaScript toolchain;
- a simpler local networking model.

However, it would couple the UI lifecycle to the Go API and remove much of the independent frontend build, delivery, and browser-boundary learning surface that this phase is intended to introduce.

## Decision

Use a separate browser client artifact.

The current implementation uses:

- React;
- TypeScript;
- Vite;
- Tailwind CSS;
- shadcn/ui.

React, Vue, and Svelte expose broadly similar client-side application and static-artifact models for the problem being studied.

They were therefore treated as representative implementations of the same architectural approach rather than as separate architectures requiring individual experiments.

Next.js was not selected because the current problem does not require another application server or a server-side rendering boundary.

## API boundary

The existing command remains:

~~~text
POST /items/{id}/process
~~~

It returns a processing-job resource after PostgreSQL has durably accepted the processing responsibility.

The browser observes that resource through:

~~~text
GET /processing-jobs/{id}
~~~

This keeps command and observation responsibilities separate:

~~~text
command
POST /items/{id}/process
        ↓
processing job ID
        ↓
observation
GET /processing-jobs/{id}
~~~

The processing-job read endpoint does not mutate processing state.

Repeated polling therefore observes the durable job state without changing the async-processing semantics.

## Local browser-origin decision

The local Web UI runs at:

~~~text
http://localhost:5173
~~~

The local Go API runs at:

~~~text
http://127.0.0.1:8080
~~~

The browser calls paths under:

~~~text
/api/*
~~~

Vite proxies those requests to the Go API during local development:

~~~text
browser
  ↓
localhost:5173/api/*
  ↓
Vite development proxy
  ↓
127.0.0.1:8080
  ↓
Work Items API
~~~

A broad API CORS policy such as:

~~~text
Access-Control-Allow-Origin: *
~~~

is deliberately not enabled merely to support local development.

## Trade-offs

The separate frontend introduces:

- Node.js and pnpm as additional development dependencies;
- a separate dependency graph;
- another build artifact;
- frontend-specific CI;
- a browser/API integration boundary that must eventually be operated.

These costs are accepted because those are useful engineering surfaces for the reference system to learn.

## Experimental-scaffolding exit decision

The Vite proxy is local-development scaffolding.

Decision:

~~~text
local development
→ keep the Vite /api proxy

future deployed environment
→ make an explicit ingress/origin/CORS decision
~~~

The local proxy does not define future:

- production ingress;
- TLS termination;
- DNS or hostname layout;
- CDN behavior;
- reverse-proxy architecture;
- production CORS policy.

## Evidence

The local browser flow was exercised against the real local Work Items dependencies:

~~~text
Web UI
  ↓
Go API
  ↓
PostgreSQL
  ↓
transactional outbox
  ↓
RabbitMQ
  ↓
worker
  ↓
processing job reaches succeeded
  ↑
browser polling
~~~

The browser successfully exercised:

- Work Item listing;
- Work Item creation;
- asynchronous processing acceptance;
- processing-job polling;
- observation of terminal `succeeded` state.

Frontend production build and lint checks also pass.

## Current limitations

The current worker still performs the existing minimal successful processing behavior.

A processing job reaching:

~~~text
succeeded
~~~

does not imply that the Work Item business status changes from:

~~~text
pending
~~~

to:

~~~text
done
~~~

Those are separate state models.

The issue does not introduce:

- a real worker transformation;
- object storage;
- authentication;
- cloud deployment;
- production ingress;
- production CORS configuration;
- a full observability stack.

## Revisit conditions

Revisit this decision if:

- server-side rendering becomes a real requirement;
- the independent frontend artifact no longer provides useful engineering value;
- browser/API deployment requirements favor a different topology;
- the system requires authentication flows that materially change the browser boundary;
- operational evidence shows the separate frontend lifecycle creates unjustified complexity.

## Consequences

Work Items now has three explicit application roles:

~~~text
web
api
worker
~~~

This broadens the reference system while preserving the existing PostgreSQL, transactional-outbox, RabbitMQ, and worker reliability model.
