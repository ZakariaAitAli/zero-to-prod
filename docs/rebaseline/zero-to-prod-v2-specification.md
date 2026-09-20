# Zero-to-Prod Rebaseline Specification

**Version:** `v1.0-draft`
**Date:** 2026-09-20
**Status:** Design specification — no repository migration or Sprint 03 implementation has started.

This document defines what Zero-to-Prod is becoming after Sprint 01 and Sprint 02. It does **not** invalidate those sprints. Their strongest result was the engineering method they established: evidence-first reasoning, explicit failure models, fail-closed behavior, IAM discipline, honest limitations, and cost awareness.

The rebaseline exists because the project applied that method very deeply to too small an engineering surface. The audit's central conclusion captures the problem well: the repository has a **surface-area problem rather than a quality problem**.

---

## 1. Mission

> **Zero-to-Prod is an evidence-driven DevOps and Cloud Engineering laboratory for learning how to design, build, secure, deploy, observe, operate, compare, recover, and evolve real software systems across local, open-source, and multi-cloud environments—without treating any technology or provider as the default answer.**

Its purpose is to develop broad engineering judgment, not expertise limited to AWS, Kubernetes, Terraform, GitHub Actions, or any other particular technology.

Zero-to-Prod should train the ability to:

```text
understand a problem
        ↓
identify constraints
        ↓
identify viable architectures
        ↓
survey credible technologies
        ↓
implement representative alternatives
        ↓
break and measure them
        ↓
compare trade-offs
        ↓
make a defensible decision
        ↓
operate the chosen system
        ↓
recover, evolve, migrate and retire it
```

---

## 2. Scope

Zero-to-Prod covers the complete lifecycle of software systems:

```text
requirements
    ↓
architecture
    ↓
development
    ↓
testing
    ↓
security
    ↓
build
    ↓
software supply chain
    ↓
infrastructure
    ↓
deployment
    ↓
verification
    ↓
observability
    ↓
reliability
    ↓
scaling
    ↓
incident response
    ↓
recovery
    ↓
optimization
    ↓
cost management
    ↓
migration
    ↓
decommissioning
```

The project includes local infrastructure, self-hosted/open-source technology, public clouds, managed services, containers, VMs, serverless systems, Kubernetes, data systems, distributed systems, security, SRE, platform engineering, and related disciplines whenever they contribute meaningful learning.

---

## 3. Non-goals

Zero-to-Prod is **not** intended to become:

- an AWS-only project;
- a Kubernetes showcase;
- a Terraform tutorial;
- a collection of disconnected tools;
- a certification study repository;
- a giant YAML collection;
- a vendor tutorial copied into Git;
- a homelab where installing software is mistaken for understanding it;
- a fake “production-ready” system;
- a multi-cloud résumé exercise where identical workloads are deployed three times without a meaningful comparison;
- a project where documentation volume substitutes for implemented capability;
- a project where implementation alone is treated as mastery.

---

## 4. Core engineering principles

### P01 — Problem before technology

Work begins with:

> What engineering problem are we solving?

Not:

> Which technology should we use next?

---

### P02 — Concepts before providers

Portable engineering concepts should be understood independently of AWS, Azure, GCP, Kubernetes, or another implementation.

Example:

```text
workload identity
    ↓
federated short-lived credentials
    ↓
AWS IAM / Azure Entra / GCP Workload Identity
```

---

### P03 — Alternatives before decisions

Important choices require examination of credible alternatives.

A technology already used in the repository does not automatically become the preferred answer.

---

### P04 — Compare representative alternatives, not every product

Zero-to-Prod should test technologies that expose genuinely different:

- architectural models,
- operational characteristics,
- security models,
- reliability properties,
- scaling behavior,
- developer experience,
- cost structures.

It should not test ten nearly identical products merely to increase tool count.

---

### P05 — Evidence before claims

Behavioral claims require evidence from the system responsible for the claim.

Examples:

```text
application health
→ application/runtime evidence

deployment state
→ deployment platform

cloud permissions
→ actual provider authorization behavior

performance
→ measurements

recovery
→ completed recovery experiment
```

---

### P06 — Hands-on before assumed understanding

Reading is exposure.

Implementation establishes basic ability.

Breaking, measuring and diagnosing a system produces deeper learning.

---

### P07 — Failure is part of the architecture

Important systems must be examined under failure.

Happy-path operation is insufficient evidence.

---

### P08 — Measure before optimizing

No optimization should exist merely because optimization is possible.

The problem must first be measurable and meaningful.

This directly addresses one of the Sprint 02 lessons, where some CI/release mechanisms became disproportionately complex relative to their original constraint.

---

### P09 — Understand when _not_ to use a technology

Knowing Kubernetes does not mean choosing Kubernetes.

Knowing microservices does not imply preferring microservices.

Knowing Kafka does not imply needing Kafka.

Engineering competence includes recognizing when simplicity is the stronger design.

---

### P10 — Local-first where behavior is portable

The local machine is a first-class engineering laboratory.

The absence of a proper local development/lab path is currently one of the project's highest-leverage deficiencies.

---

### P11 — Real providers when provider semantics matter

Use AWS, Azure, GCP, or another platform when the provider itself is part of the lesson.

Examples:

```text
IAM evaluation
Organizations / management groups
VPC/VNet behavior
managed failover
provider quotas
cloud billing
managed monitoring semantics
```

---

### P12 — Multi-cloud without artificial duplication

AWS, Azure, and GCP are comparison targets, not mandatory copies of every architecture.

Another provider is introduced when it exposes a meaningful difference.

---

### P13 — Production-oriented, not production-theater

Zero-to-Prod may build production-oriented reference architectures.

It must not claim production readiness without evidence.

Every serious implementation should document:

```text
assumptions
security boundaries
failure modes
availability expectations
data durability
observability
operational requirements
recovery
scaling
cost
known limitations
unproven properties
```

---

### P14 — Prefer platform primitives and standards before bespoke mechanisms

Before creating a custom solution, investigate:

```text
existing platform primitives
industry standards
portable specifications
widely adopted protocols/formats
```

Custom mechanisms remain acceptable when the trade-off is explicit.

---

### P15 — Experimental scaffolding must have an exit decision

Anything created solely to enable an experiment must eventually be explicitly:

```text
kept
generalized
replaced
or removed
```

The current `RUNTIME_CONTRACT` experiment is the canonical example.

---

### P16 — Security is cross-cutting

Security is not one future sprint.

It includes:

```text
identity
authorization
secrets
supply chain
application security
network security
runtime security
detection
response
data protection
```

throughout the project.

---

### P17 — Observability is part of design

Logs, metrics, traces and operational signals should be determined by what operators need to understand.

They are not decoration added after deployment.

---

### P18 — Operations influence architecture

A design must consider:

```text
deployment
upgrade
debugging
backup
recovery
scaling
incident response
migration
retirement
```

from the beginning.

---

### P19 — Cost is a design constraint, not a learning constraint

The project remains cost-conscious because cloud expenses are personally funded.

When cost blocks learning:

> Move suitable learning locally rather than remove the learning objective.

---

### P20 — Decisions are contextual and revisitable

Major decisions should state:

```text
why this choice fits now
what alternatives were considered
what trade-offs were accepted
what assumptions were made
what would cause us to reconsider
```

---

## 5. Learning model

Technology names will no longer be marked simply as “learned.”

Zero-to-Prod uses:

| Level                     | Meaning                                                                                          |
| ------------------------- | ------------------------------------------------------------------------------------------------ |
| **L0 — Uncovered**        | No meaningful hands-on exposure                                                                  |
| **L1 — Exposed**          | Encountered the concept                                                                          |
| **L2 — Explain**          | Can explain the problem, purpose and mechanics                                                   |
| **L3 — Implement**        | Can build a working implementation                                                               |
| **L4 — Experiment**       | Has deliberately tested/broken it and understands important failure behavior                     |
| **L5 — Compare & Decide** | Can compare credible alternatives and defend an architectural choice                             |
| **L6 — Operate**          | Has sustained operational experience with upgrades, incidents, maintenance, failure and recovery |

### Target policy

> **Important domains should generally progress toward L5. Selected foundational systems should earn L6 through sustained operation.**

L6 must not be awarded because of a one-day chaos experiment.

---

## 6. Learning is concept-specific

A technology does not have one universal learning level.

For example:

```text
Terraform

remote state           L4
locking                L4
runner recovery        L4

modules                L1
brownfield import      L1
drift management       L1
refactoring            L0
testing                L0
multi-environment      L1
```

This prevents:

```text
Terraform ✓
```

from hiding substantial knowledge gaps.

The audit shows exactly this pattern: state mechanics are strongly exercised, while broader infrastructure modelling remains shallow.

---

## 7. Canonical knowledge taxonomy

Zero-to-Prod uses 22 top-level engineering domains:

```text
01 Software & Architecture
02 Systems & Linux
03 Networking
04 Runtime & Containers
05 Orchestration & Kubernetes
06 Source, Build & CI
07 Release & Delivery
08 Infrastructure as Code
09 Cloud Engineering
10 Data Systems
11 Messaging & Distributed Systems
12 Security
13 Software Supply Chain
14 Observability
15 SRE & Reliability
16 Performance & Resilience
17 Incident Response & Disaster Recovery
18 Platform Engineering & Developer Experience
19 Testing & Quality
20 FinOps
21 Governance & Decision Engineering
22 Migration, Lifecycle & Decommissioning
```

Technologies are implementations **inside** these domains.

Therefore:

```text
AWS
Azure
GCP
Kubernetes
Terraform
Kafka
PostgreSQL
GitHub Actions
```

are intentionally **not** top-level knowledge categories.

---

## 8. Capability evaluation

Every substantial capability should eventually be considered through these lenses:

```text
Problem
Architecture
Alternatives
Security
Reliability
Observability
Performance
Scalability
Operations
Developer experience
Cost
Portability
Lock-in
Failure modes
Lifecycle
Evidence
```

Not every experiment must exhaust all 16 dimensions, but they define the complete engineering model.

---

## 9. Technology-decision standard

A mature technology decision should answer:

```text
What problem are we solving?

What constraints exist?

What credible approaches exist?

How do those approaches work?

Which differences are architecturally meaningful?

What operational burden does each create?

How do they fail?

How do they scale?

How are they secured?

How are they observed?

What do they cost?

How portable are they?

What vendor/ecosystem dependencies exist?

How difficult are upgrades and migration?

What experiments did we perform?

What did those experiments actually demonstrate?

Which approach fits this system?

Why?

When would we choose differently?

What conditions would cause us to revisit the decision?
```

The decision is recorded through an ADR when appropriate.

---

## 10. Environment strategy

Every experiment receives one of three default classifications.

### LOCAL-FIRST

Use local environments where the important behavior is portable.

Examples:

```text
application lifecycle
PostgreSQL migrations
Redis
RabbitMQ
Kafka
Kubernetes fundamentals
GitOps
Prometheus
Grafana
OpenTelemetry
supply-chain tooling
load testing
chaos
```

### PROVIDER-SPECIFIC

Use a real provider where emulation would remove the lesson.

Examples:

```text
AWS IAM/SCP behavior
Azure Entra behavior
GCP Workload Identity
VPC/VNet-specific networking
managed-service failover
cloud billing
provider quotas
```

### HYBRID

Learn the general mechanism locally, then validate provider-specific behavior in a bounded cloud experiment.

Example:

```text
PostgreSQL locally
    ↓
migrations
backup
pooling
failure
    ↓
RDS / Cloud SQL / Azure PostgreSQL
    ↓
managed failover
PITR
provider-specific operations
```

The rebaseline audit independently reached essentially this local/provider split.

---

## 11. Environment hierarchy

The project may progressively support:

```text
E0 Developer environment
   fast single-service feedback

E1 Local integration lab
   application + real dependencies

E2 Local platform lab
   orchestration, observability, GitOps, security

E3 Persistent operational lab
   long-running environment for L6 learning

E4 Cloud sandbox
   provider-specific engineering

E5 Ephemeral cloud experiment
   expensive/temporary provider experiment
```

Heavy local infrastructure should be opt-in.

The normal workstation must remain usable.

---

## 12. Environment fidelity

Zero-to-Prod explicitly recognizes:

```text
MinIO != S3
LocalStack != AWS
k3d != EKS
containerized PostgreSQL != RDS Multi-AZ
```

Local environments prove portable behavior.

Real providers prove provider-specific behavior.

Neither should be used to overclaim the other.

---

## 13. Cloud cost contract

Before creating non-trivial cloud infrastructure, document:

```text
Why real cloud is required
Expected lifetime
Expected maximum cost
Cleanup mechanism
Interruption recovery
Resources that may continue billing
Final cost verification
```

Cloud should never remain running merely to make the portfolio appear “live.”

---

## 14. Reference-system strategy

Instead of creating many disconnected demos, Zero-to-Prod should eventually develop a realistic reference system containing roles such as:

```text
Web UI
   ↓
API
   ↓
Relational datastore
   │
   ├── Cache
   ├── Object storage
   └── Message delivery
            ↓
          Worker
```

The components are intentionally defined as **roles rather than products**.

For example:

```text
message delivery
```

might be tested using:

```text
RabbitMQ
SQS
Kafka
NATS
Service Bus
Pub/Sub
```

when different choices expose useful trade-offs.

This creates high learning density across architecture, networking, state, asynchronous processing, observability, security, performance, reliability and delivery.

---

## 15. Repository architecture

The long-term conceptual structure is:

```text
zero-to-prod/
│
├── apps/
├── systems/
├── platform/
├── deployments/
├── providers/
├── lab/
├── experiments/
├── evidence/
│
├── docs/
│   ├── concepts/
│   ├── architecture/
│   ├── adr/
│   ├── guides/
│   ├── runbooks/
│   ├── incidents/
│   ├── security/
│   ├── reference/
│   ├── learning/
│   └── sprints/
│
├── tools/
├── tests/
└── .github/
```

This is a **target architecture**, not an instruction to reorganize everything immediately.

The rebaseline audit independently recommended separating workloads, infrastructure, local labs, concepts, ADRs, experiments, runbooks, incidents and evidence.

---

## 16. Documentation model

Each type of knowledge has one purpose.

### Concept

> What should remain useful regardless of a particular implementation?

### Architecture

> How is this system structured?

### ADR

> Why was this decision made?

### Guide

> How is something implemented?

### Experiment

> What did we test and what happened?

### Evidence

> What proves the claim?

### Runbook

> How do we operate or recover it?

### Incident

> What unexpectedly failed and how did we respond?

### Reference

> What facts need quick lookup?

### Sprint

> What progress and learning occurred during this period?

Sprint documents become indexes rather than giant knowledge containers.

The existing experiment records remain valuable and should be preserved while reusable concepts are extracted rather than rewriting their history.

---

## 17. Historical preservation rule

Sprint 01 and Sprint 02 remain historical evidence.

They are **not retroactively rewritten** to conform to v2.

Knowledge may be extracted from them:

```text
historical experiment
        │
        ├── evergreen concept
        ├── ADR
        ├── runbook
        └── durable evidence
```

but the experiment record remains the factual source of what happened.

---

## 18. Evidence standard

Evidence should increasingly be:

```text
durable
sanitized
machine-readable where practical
independently understandable
linked to the experiment
stored outside ephemeral provider UI where possible
```

This improves on the current pattern in which some evidence relies on GitHub Actions runs or private cloud state that may eventually disappear.

---

## 19. Experiment standard

A substantial experiment should normally cover:

```text
Question
Why it matters
Current understanding
Options
Hypothesis
Architecture
Implementation
Expected failure model
Experiment
Evidence
Measurements
Unexpected behavior
Failure analysis
Security implications
Operational implications
Cost implications
Alternative comparison
What was learned
What remains unknown
Decision / ADR
Cleanup
```

This preserves the rigor of Sprint 02 without requiring every experiment to become thousands of lines long.

---

## 20. Capability unit

The basic learning unit becomes:

```text
Capability
│
├── Problem
├── Concepts
├── Alternatives
├── Architecture
├── Representative technologies
├── Local experiment
├── Provider experiment if justified
├── Failure tests
├── Measurements
├── Security
├── Operations
├── Cost
├── Comparison
├── Decision
├── Learning level
└── Remaining unknowns
```

This replaces:

```text
Installed technology X ✓
```

---

## 21. Roadmap

The current long-term dependency order is:

```text
0  Rebaseline & foundations
          ↓
1  Operable applications
          ↓
2  Real system backbone
          ↓
3  Observe & measure
          ↓
4  Reliability & operations
          ↓
5  Delivery & orchestration comparison
          ↓
6  Data & distributed systems depth
          ↓
7  Security & software supply chain depth
          ↓
8  Cloud/provider comparison
          ↓
9  VM, serverless & alternative compute
          ↓
10 Platform engineering
          ↓
11 Sustained operations / L6
          ↓
12 Architecture evolution, migration & retirement
```

This is a dependency model, **not a fixed calendar**.

Several stages can overlap once their prerequisites exist.

---

## 22. Cross-cutting tracks

The following evolve continuously:

```text
Security
Testing
Networking
Documentation
Evidence
Cost
Developer experience
Architecture decisions
Performance
```

There will not be a future period where, for example, security is “temporarily out of scope” simply because the dedicated security-depth stage has not arrived yet.

---

## 23. Sprint design rules

Future sprints must **not** be designed as:

```text
Sprint — Learn Kubernetes
Sprint — Learn Kafka
Sprint — Learn Azure
```

A sprint should instead answer one or more engineering questions through coherent systems.

A strong sprint should advance several related capability levels simultaneously.

Example:

```text
Goal:
Make a stateful service safely deployable and observable.

Advances:

application lifecycle       L2 → L4
database migrations         L0 → L4
readiness                   L1 → L4
metrics                     L0 → L3
backup/restore              L0 → L3
performance testing         L0 → L3
```

Technology choices follow from that goal.

---

## 24. Sprint learning-ROI test

Every proposed issue should answer two questions.

### Capability gain

> What can Zero-to-Prod do afterward that it could not do before?

### Learning gain

> What can the engineer explain, test, compare, diagnose or decide afterward?

Work with low capability gain and low learning gain should usually be rejected.

This is one of the most important safeguards against repeating Sprint 02's diminishing-return pattern.

---

## 25. L5 and L6 policy

The long-term target is:

```text
breadth
→ L5 across important engineering domains

selected depth
→ L6 in foundational technologies/systems
```

Probable eventual L6 candidates include:

```text
Linux
containers
one primary orchestrator
one relational datastore
observability
CI/CD
incident/SRE practice
one primary cloud provider
the persistent Zero-to-Prod platform itself
```

Which exact technologies earn those slots remains a decision to be earned through comparison.

---

## 26. Multi-cloud policy

AWS is currently the deepest cloud implementation because that is where existing Zero-to-Prod work exists.

This does **not** make AWS the architectural center of v2.

Azure, GCP and other providers are introduced around comparison questions such as:

```text
How does federated workload identity differ?

How do private-networking models differ?

How do managed container platforms differ?

How do managed relational databases differ?

What portability assumptions survive?

What changes operationally?

What changes economically?
```

The goal is **multi-cloud judgment**, not identical multi-cloud duplication.

---

## 27. Monorepo policy

Zero-to-Prod starts as one integrated repository.

Components should move into separate repositories only after demonstrating a real boundary, such as:

```text
independent lifecycle
independent consumers
different ownership
different security boundary
large independent CI workload
meaningful reuse outside Zero-to-Prod
```

Do not create distributed-repository complexity preemptively.

---

## 28. Definition of successful engineering learning

Zero-to-Prod succeeds when an unfamiliar system can be approached like this:

```text
I understand the requirement.

I can identify constraints.

I can recognize relevant architecture patterns.

I can identify realistic alternatives.

I know which uncertainties matter.

I can design an experiment to resolve them.

I can implement representative options.

I can measure their behavior.

I can reason about security, reliability and cost.

I understand important failure modes.

I can defend a decision.

I know when I would choose differently.

I can deploy it.

I can observe it.

I can diagnose it.

I can recover it.

I can evolve it.

I can eventually retire it.
```

That—not the number of tools in the repository—is the long-term success criterion.

---

## 29. Existing strengths that must survive the rebaseline

The rebaseline must **not destroy what already works well**.

Explicitly preserve:

```text
evidence-first engineering
failure models before implementation
fail-closed defaults
honest scope/limitation statements
OIDC instead of static credentials
least-privilege IAM discipline
full-SHA GitHub Actions pinning
immutable artifact delivery
build once / deploy tested artifact
external runtime verification
cleanup vs recovery distinction
cost guardrails
reflection after experiments
```

These are among the strongest attributes identified by the audit.

---

## 30. Immediate rebaseline constraints

Until the rebaseline moves into implementation:

```text
Do not rewrite Sprint 01.
Do not rewrite Sprint 02.
Do not start a Kubernetes project.
Do not create a giant new backlog.
Do not create Azure/GCP copies.
Do not restructure the entire repository at once.
Do not delete existing mechanisms merely because a reviewer dislikes them.
Do not call Sprint 03 "Kubernetes", "Observability" or another tool/domain name.
```

Every repository change after this point should be traceable to this specification.

---

## 31. Rebaseline acceptance criteria

I would consider the **design phase** of the rebaseline complete when we agree that:

- the mission is correct;
- provider and technology neutrality are explicit;
- the L0–L6 learning model is correct;
- the 22-domain taxonomy is sufficient;
- local/provider/hybrid rules are correct;
- documentation types are separated clearly;
- historical sprint evidence will be preserved;
- future work is problem-driven rather than tool-driven;
- comparison and ADR expectations are clear;
- the roadmap dependency ordering is reasonable;
- L5 breadth / selective L6 depth is the intended learning outcome;
- future Sprint 03 work can be evaluated against these rules.

The design criteria above are considered met for this rebaseline draft.

## Rebaseline design status

```text
Mission v2                       COMPLETE
Engineering principles           COMPLETE
Master audit baseline            COMPLETE
Capability map                   COMPLETE
Learning map                     COMPLETE
Knowledge taxonomy               COMPLETE
Target repository architecture   COMPLETE
Lab/environment strategy         COMPLETE
Long-term roadmap                COMPLETE
Roadmap gap matrix               COMPLETE
Rebaseline specification         COMPLETE — draft
```

The important transition is now:

```text
AUDIT
  ↓
RETHINK
  ↓
SPECIFY
  ↓
────────────── WE ARE HERE ──────────────
  ↓
IMPLEMENT THE REBASELINE
  ↓
DESIGN SPRINT 03
```
