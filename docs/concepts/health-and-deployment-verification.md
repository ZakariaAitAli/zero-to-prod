# Health and deployment verification

A deployment system needs to distinguish several different questions:

```text
Is the process alive?
Is the application ready for traffic?
Did the platform converge?
Can a client reach the application?
Is the intended version actually running?
```

These are related signals, but they are not interchangeable.

## Liveness

Liveness answers whether the application process is alive enough to continue running.

The Work Items API exposes:

```text
GET /health
```

A dependency outage does not necessarily mean the process itself should be considered dead.

## Readiness

Readiness answers whether an instance should currently accept the traffic represented by its readiness contract.

For the current Work Items API, PostgreSQL is a critical request-time dependency. Readiness checks a bounded, read-only query against the schema contract the application actually needs.

Therefore PostgreSQL can fail while:

```text
/health → 200
/ready  → 503
```

The process is alive, but it is not ready to serve the dependent workload.

RabbitMQ is intentionally different in the current architecture. Accepted asynchronous work is first represented durably in PostgreSQL through the transactional outbox, so broker availability is not part of the API's request-time readiness check.

Readiness should represent the architecture's actual traffic-acceptance contract rather than blindly checking every dependency.

## Platform stability is not application verification

Sprint 01 demonstrated that ECS control-plane stability does not prove application correctness.

A platform can report a stable service while an external client still cannot use the application as intended.

The deployment path therefore added external verification after ECS convergence.

## Health is not version identity

A healthy response answers whether the endpoint works.

It does not answer whether the intended artifact was deployed.

Sprint 01 checked both:

```text
/health
/version
```

and deliberately exercised a wrong-version failure. A healthy old version is still a failed deployment when the deployment intended a different commit.

## Health exists at multiple layers

The AWS experiments exposed several layers:

- process/container health;
- ECS task and service state;
- load-balancer target health;
- application HTTP behavior;
- external reachability;
- exact deployed version.

A failure in one layer should not automatically be described as a failure in another.

This matters for diagnosis. "The service is unhealthy" is less useful than identifying which layer is failing.

## Verification needs bounds

A verifier that retries forever turns a failure into an indefinite wait.

Sprint 01 added bounded connection time, request time, retry count, and retry delay. Sprint 02 extended failure diagnosis around deployment transitions.

Bounded verification provides two useful properties:

1. transient startup behavior can settle;
2. persistent failure eventually produces a clear failed result.

## Diagnostics are part of failure handling

When verification fails, the system should retain enough context to distinguish causes such as:

- application startup exit;
- container health failure;
- ECS stabilization failure;
- HTTP failure;
- connection failure;
- timeout;
- version mismatch.

Sprint 02 deliberately exercised several of these layers and added bounded diagnostic collection.

## Graceful shutdown affects readiness

The current Work Items application marks itself not ready when graceful shutdown begins.

That represents an important lifecycle transition:

```text
serving
  ↓
stop accepting new traffic
  ↓
finish bounded in-flight work
  ↓
exit
```

Readiness is therefore not only a startup check. It participates in application lifecycle behavior.

## What to remember

- Liveness and readiness answer different questions.
- Readiness should reflect critical dependencies for the workload being accepted.
- Platform convergence is not external application verification.
- A healthy application can still be the wrong version.
- Health exists at several layers; diagnose the failing layer.
- Verification must be bounded.
- Useful diagnostics are part of deployment reliability.
- Readiness also matters during shutdown.

## Zero-to-Prod deep dives

- [Automated health verification](../sprint-01/automated-health-verification.md)
- [ECS Fargate deployment](../sprint-01/ecs-fargate-deploy.md)
- [Failed deployment diagnostics](../sprint-02/failed-deployment-diagnostics.md)
- [CloudWatch application logging](../sprint-02/ecs-cloudwatch-logging.md)
- [Local Work Items development](../guides/local-work-items.md)
