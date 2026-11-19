# ADR 0002 — Separate browser UI artifact

Status: Accepted.

## Decision

Build the React/Vite/TypeScript browser UI separately from the Go API and worker.
The API remains a JSON interface used by both the UI and other clients.
The UI lives in `apps/work-items/web`; its build and runtime assumptions must
not change the backend's durable acceptance or processing semantics.

For local development, Vite proxies `/api/*` to the API. This avoids a broad CORS
policy simply to connect the development UI. The proxy is not a production ingress
or frontend-delivery decision. The security lab serves the built UI through its
HTTPS ingress with a session gateway and same-origin protections.

## Rationale and alternatives

A separate artifact makes browser behavior, accessibility and delivery independently
reviewable. Embedding static assets in the Go binary would couple their release
lifecycles. Server-rendered pages would reduce frontend build tooling but provide
a different interaction model. Those are alternatives, not additional workloads
to implement without a concrete need.

## Consequences

The UI must distinguish request failure, business status and job execution status.
It handles polling and authentication-service failure but does not own backend
retries or durable processing. Client tests do not establish browser behavior.

Separate builds introduce dependency, origin, deployment and version-compatibility
work. Define those boundaries before exposing a deployment. Reconsider this choice
if independent frontend releases add more operational cost than they provide value.

See the [local guide](../guides/local-work-items.md) and
[frontend README](../../apps/work-items/web/README.md).
