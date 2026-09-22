# Zero-to-Prod v2 — Engineering Work Contract

## Purpose

This document operationalizes:

    docs/rebaseline/zero-to-prod-v2-specification.md

It defines the minimum conventions for:

- documentation;
- experiments;
- evidence;
- learning-level changes;
- Sprint 03 candidate selection.

The specification remains authoritative.

The goal is rigor without documentation bloat.

---

## Documentation contract

Use each document type for one primary purpose.

| Type | Purpose |
| --- | --- |
| Concept | Portable engineering knowledge independent of one implementation |
| Architecture | System structure, boundaries, dependencies, ownership, and flows |
| ADR | Why a meaningful architectural or operational decision was made |
| Guide | How to implement or perform a repeatable workflow |
| Experiment | What was deliberately tested and what happened |
| Evidence | What proves a behavioral claim |
| Runbook | How to operate, recover, or maintain a system |
| Incident | What failed unexpectedly and what changed afterward |
| Reference | Stable facts or conventions for quick lookup |
| Sprint | Index of coherent capability and learning progress |

Create an ADR when:

- credible alternatives exist;
- the decision has meaningful consequences;
- preserving the reasoning will help future work.

A useful ADR normally records:

- problem;
- constraints;
- alternatives;
- evidence;
- trade-offs;
- decision;
- consequences;
- reconsideration conditions.

Do not create an ADR merely because a technology was used.

### Historical preservation

Sprint 01 and Sprint 02 remain historical evidence.

Do not:

- rewrite them into v2 terminology;
- move them only to fit the target repository layout;
- revise historical claims because later implementation changed.

Reusable concepts, guides, ADRs, runbooks, and evidence may be extracted while the historical source remains intact.

### Proportionality

Small changes normally need only:

- the change;
- appropriate checks;
- concise PR evidence.

Medium capability changes may additionally require:

- a guide or architecture update;
- implementation evidence;
- relevant failure testing.

Substantial experiments use the convention below.

Documentation volume is not evidence of engineering quality.

---

## Substantial experiment convention

Use the relevant parts of this structure.

### Problem

Record:

- question being answered;
- why it matters;
- current understanding;
- remaining assumptions;
- important constraints.

### Alternatives

Identify credible approaches.

Do not add alternatives merely to increase tool count.

### Hypothesis

State expected behavior before testing.

### Environment

Classify the experiment as exactly one of:

    LOCAL-FIRST
    PROVIDER-SPECIFIC
    HYBRID

Explain why.

Use real cloud infrastructure only when provider semantics are part of the learning objective.

### Architecture

Record the participating components, dependencies, and important boundaries.

### Failure model

Identify:

- how the system can fail;
- which failures matter;
- which failures will be deliberately exercised.

### Implementation

Build only what is required to answer the engineering question.

### Experiment

Record:

- what was exercised;
- what was expected;
- what was observed.

### Evidence

Prefer evidence that is:

- durable;
- sanitized;
- reproducible;
- machine-readable where practical;
- understandable outside an ephemeral provider UI.

Behavioral claims should use evidence from the system responsible for the claim.

Examples:

    application health
    -> runtime evidence

    deployment state
    -> deployment-platform evidence

    authorization
    -> actual authorization behavior

    performance
    -> measurements

    recovery
    -> completed recovery experiment

### Measurements

Measure only what helps answer the question.

Possible examples:

- latency;
- throughput;
- recovery time;
- resource consumption;
- cost.

### Analysis

Capture relevant:

- unexpected behavior;
- failure analysis;
- security implications;
- operational implications;
- cost implications.

### Comparison

When evaluating alternatives, record meaningful observed differences.

L5 requires credible comparison and a defensible decision.

Implementation of one option alone does not establish L5.

### Learning

State what can now be:

- explained;
- implemented;
- tested;
- diagnosed;
- operated;
- compared;
- decided.

### Remaining unknowns

Explicitly state what the experiment does not prove.

### Decision

Create or link an ADR when the evidence produces a significant architectural decision.

### Cleanup

Experimental scaffolding must eventually be explicitly:

    kept
    generalized
    replaced
    or removed

Independently verify cleanup when cost or operational state matters.

---

## Learning-level changes

The current baseline is:

    docs/learning/current-capability-baseline.md

Before increasing a capability level, record:

- previous level;
- new level;
- supporting evidence;
- why that evidence satisfies the level;
- remaining unknowns.

Evidence expectations:

| Change | Requirement |
| --- | --- |
| L2 -> L3 | Working implementation |
| L3 -> L4 | Deliberate experimentation or failure testing |
| L4 -> L5 | Credible alternatives compared and a defensible decision made |
| L5 -> L6 | Sustained operational experience |

A short experiment cannot establish L6.

A technology does not receive one universal learning level.

Levels remain capability-specific.

---

## Sprint 03 entry criteria

Sprint 03 must begin with an engineering problem, not a technology name.

Before selecting a candidate, answer the following.

### Engineering problem

What problem are we solving?

### Capability gain

What can Zero-to-Prod do afterward that it could not do before?

### Learning gain

What can the engineer explain, test, compare, diagnose, operate, or decide afterward?

### Current baseline

Which capabilities in:

    docs/learning/current-capability-baseline.md

does this candidate build on?

### Expected progression

For each important capability record:

    current level -> expected level

Also state what evidence would be required to earn the expected level.

The expected level is a hypothesis, not an automatic promotion.

### Prerequisites

What must already work before the candidate is useful?

### Environment classification

Choose:

    LOCAL-FIRST
    PROVIDER-SPECIFIC
    HYBRID

If cloud resources are required, explain why local execution cannot answer the question.

### Alternatives

What credible implementation approaches should be considered?

Important architectural choices must not assume a technology by default.

### Expected evidence

What observable evidence will demonstrate success or failure?

### Failure questions

Which important failure modes should be deliberately exercised?

### Security

What new:

- trust boundaries;
- permissions;
- secrets;
- data exposure;
- network exposure;
- supply-chain concerns

are introduced?

### Operations

What new:

- deployment;
- upgrade;
- debugging;
- maintenance;
- recovery

responsibilities are introduced?

### Cost

Consider:

- local resource usage;
- cloud cost;
- idle cost;
- failure cost;
- cleanup.

### Decision requirement

Would the work produce a meaningful architectural decision?

If yes, determine whether an ADR is required.

### Learning ROI

Reject or redesign work with:

    low capability gain
    +
    low learning gain

Adding another technology name is not sufficient learning value.

---

## Repository evolution

The v2 repository architecture is a target, not an immediate migration.

Create directories when actual content requires them.

Issue #92 establishes real content under:

    docs/guides/
    docs/learning/
    docs/reference/

Do not pre-create the complete future directory structure.

Historical Sprint 01 and Sprint 02 content remains in place.

---

## Working sequence

Future substantial work should normally follow:

    engineering problem
            |
            v
    capability + learning gain
            |
            v
    environment classification
            |
            v
    credible alternatives
            |
            v
    experiment
            |
            v
    failure testing
            |
            v
    evidence
            |
            v
    comparison when required
            |
            v
    decision
            |
            v
    capability update
            |
            v
    cleanup / operations

This keeps Zero-to-Prod problem-driven, evidence-driven, cost-aware, and proportional.
