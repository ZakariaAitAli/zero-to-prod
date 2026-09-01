## Objective

Define the architectural contract for the September 2026 Sprint 02 theme:

> **Recoverable, Observable, and Cost-Controlled Deployments**

For the selected controlled failure scenarios, Sprint 02 extends the development deployment path to recover temporary verification infrastructure from a fresh runner after hard interruption, diagnose ordinary failures and clean up normally, refuse unsafe rollback before ECS mutation, preserve durable successful-verification evidence, and return to a low-cost resting state.

This is a development-only learning sprint. It does not claim recovery from every possible failure, universal runtime compatibility, or production readiness. GitHub issues remain authoritative for issue-specific acceptance criteria. Issue #34 changes no AWS infrastructure and adds no implementation code.

## What Sprint 01 already proves

```text
tested commit -> immutable full-SHA ECR image -> GitHub Actions OIDC
-> development ECS deployment -> temporary external verification
-> ECS stability -> /health -> exact /version
-> desired count 0 -> destroy temporary verification infrastructure
```

Sprint 01 proves build-once artifact reuse, keyless CI authentication, ECS scale-up and scale-down, separate control-plane and external verification, bounded normal cleanup, serialized workflow mutations, and operator-triggered rollback to an existing image.

It does not prove recovery after a runner disappears with resources present, durable post-task diagnostics, evidence-backed rollback eligibility, or compatibility between an older image and current runtime configuration.

## Sprint 01 limitations selected for Sprint 02

| Limitation | Sprint 02 target |
| --- | --- |
| Terraform state is runner-local. | Store authoritative state outside the runner and prove fresh-runner access. |
| Interrupted cleanup has no tested recovery path. | Test operator-triggered recovery from a fresh runner. |
| Application logs are not durable. | Retain bounded logs long enough to diagnose selected failures. |
| Failed-deployment diagnostics are weak. | Correlate bounded ECS, container, log, and verifier evidence. |
| Successful verification has no durable machine-readable record. | Record evidence only after required verification succeeds. |
| Rollback eligibility depends on operator knowledge. | Require verified-deployment evidence before mutation. |
| Runtime compatibility is unknown. | Define and test a narrow Sprint 02 compatibility model. |
| CI is not change-aware. | Conservatively skip only demonstrably irrelevant validation. |

These limitations directly affect recoverability, diagnostics, rollback safety, or cost. Other improvements remain outside the sprint unless evidence shows they are necessary for this contract.

## Target end-to-end capability

> Given a tested commit in `zero-to-prod`, GitHub Actions can deploy the exact immutable image to the development ECS service; preserve recoverable infrastructure state; diagnose the selected deployment failures; verify external `/health` and exact `/version`; record successful verification as durable machine-readable evidence; permit operator-triggered rollback only to a previously verified candidate compatible under the tested Sprint 02 compatibility model; perform normal automatic cleanup; and support operator-triggered fresh-runner recovery after hard interruption so the environment can return to its low-cost baseline.

### Successful and ordinary-failure paths

Successful deployment requires ECS stability, `/health`, and exact `/version` before writing successful-verification evidence. It then scales ECS to `0`, destroys temporary verification infrastructure, and independently checks the baseline.

An ordinary failure stops within bounded time, preserves the evidence defined by the failure model, reports the failed layer, attempts normal automatic cleanup without letting diagnostics block cleanup, and checks final AWS state where practical.

### Hard-interruption recovery path

After runner termination, cleanup is not assumed. Authoritative state remains outside the runner. A later operator-triggered workflow reconnects from a fresh runner, inspects owned resources, performs recovery or destruction, and independently verifies the baseline. This proves the selected controlled scenario, not every possible state loss, outage, permission failure, or operator error.

### Rollback path

```text
operator target -> verified evidence for environment? -> immutable image exists?
-> compatible under tested Sprint 02 model?
-> no/unknown: refuse before ECS mutation
-> yes: deploy existing image -> ECS stability -> /health -> exact /version -> cleanup
```

Historical verification establishes eligibility, not current success. Post-rollback stabilization and external verification remain mandatory.

The resting state is ECS desired/running/pending counts `0`, temporary verification ALB/listener absent, and no unintended temporary deployment resources. Small durable resources may persist only for justified state, bounded logs, or evidence.

## Architecture delta

| Area | Sprint 02 delta | Boundary |
| --- | --- | --- |
| Terraform | Durable remote state and state-level mutation protection | Ownership remains limited to verification-lifecycle infrastructure. |
| Recovery | Operator-triggered fresh-runner procedure | No always-running remediation service. |
| Diagnostics | Bounded application logs plus correlated ECS/verifier output | No full observability platform. |
| Verification evidence | Small durable machine-readable records written after verification | Not a general release platform. |
| Rollback | Evidence-backed eligibility, then tested compatibility | Rollback remains operator-triggered. |
| CI | Conservative path-aware validation | Uncertain changes run validation. |

### Issues #40, #41, and #42

| Issue | Responsibility | Boundary |
| --- | --- | --- |
| #40 | Record environment, Git SHA/artifact identity, image URI/digest, task-definition identity, workflow/run identity, verification timestamp, and verification results. Keep the schema extensible for runtime evidence. | Does not know the final compatibility model. |
| #41 | Use verified evidence, environment, and image existence for rollback eligibility. | Prior verification does not establish current compatibility. |
| #42 | Experimentally determine the runtime/task properties in the rollback unit, define the Sprint 02 runtime-configuration identity, extend #40 metadata as required, and reject at least one incompatible rollback. | Claims apply only to the tested Sprint 02 model. |

The final Definition of Done requires runtime-configuration identity because #42 completes it; #40 is not responsible for predicting #42's experimental result.

## Failure model

| Response | Meaning |
| --- | --- |
| **Recover later** | The original runner is gone; an operator-triggered fresh runner uses durable state. |
| **Diagnose + clean up** | The running workflow collects bounded evidence and attempts normal automatic cleanup. |
| **Refuse before mutation** | Rollback evidence is insufficient or incompatible, so ECS is unchanged. |

Concurrent Terraform mutation is prevention: reject, wait, or safely serialize the competing writer while preserving authoritative state.

### Runner terminates after `terraform apply`

**Failure:** Resources exist, but the runner dies before destroy. **Surviving evidence:** authoritative state, backend identity, run identity, and AWS resource identity outside the runner. **Required behavior:** a later operator-triggered fresh runner reconnects, inspects owned resources, recovers or destroys them, and independently verifies AWS cleanup. **Outcome — recover later:** prove this controlled interruption without claiming recovery from every loss or corruption of durable state.

### Fresh runner has no local Terraform state

**Failure:** A new runner has no `.terraform` directory or local state. **Surviving evidence:** configured remote backend and authoritative state. **Required behavior:** `terraform init` reconnects without copied files; plan, inspection, or destroy uses the existing history rather than recreating resources blindly. **Outcome — recover later:** prove fresh execution can operate on the same authoritative state.

### Concurrent Terraform mutation

**Failure:** Two executions mutate the same state. Workflow concurrency alone is insufficient for every writer. **Surviving evidence:** one authoritative history and direct lock/conflict output. **Required behavior:** reject, wait, or safely serialize the competitor; two writers cannot independently report successful mutation; state remains usable. **Outcome — prevention:** prove the conflicting mutation cannot silently proceed.

### Application startup failure

**Failure:** The container exits before stable health. **Surviving evidence:** ECS task/stop state, container exit details, service events, task definition, image identity, and bounded stdout/stderr. **Required behavior:** bounded stabilization, no verified record, useful diagnostics, and normal automatic cleanup even if diagnostics are incomplete. **Outcome — diagnose + clean up:** distinguish startup failure from infrastructure or verifier failure.

### ECS service fails to stabilize

**Failure:** The service misses the stability deadline due to task, placement, target-health, or rollout problems. **Surviving evidence:** deployment state, events, counts, stopped-task/container details, task/image identity, and logs where available. **Required behavior:** explicit timeout, no verified record, bounded diagnostics, and normal automatic cleanup. **Outcome — diagnose + clean up:** attribute the layer as far as evidence permits.

### External `/health` verification fails

**Failure:** ECS is stable but an external client cannot obtain the required health result. **Surviving evidence:** request/result, endpoint and expected status, ECS/target context, logs, task definition, and image. **Required behavior:** fail within bounded retries, create no verified record, report health/connectivity evidence, and clean up normally. **Outcome — diagnose + clean up:** distinguish application reachability/health from ECS stability.

### Exact `/version` mismatch

**Failure:** `/health` succeeds but `/version` differs from the intended Git SHA. **Surviving evidence:** intended SHA, returned value, request/result, image digest, task definition, and run identity. **Required behavior:** fail, create no verified record, report the mismatch, and clean up normally. **Outcome — diagnose + clean up:** a healthy wrong artifact cannot become verified.

### ECR image exists but was never successfully verified

**Failure:** A valid SHA has an immutable ECR image but no successful-verification record for the intended environment. **Surviving evidence:** SHA, image lookup, environment, record lookup, rejection reason, and proof ECS was unchanged. **Required behavior:** build, publication, or deployment attempt remains insufficient; stop before task-definition registration or ECS deployment. **Outcome — refuse before mutation:** prove a published-but-unverified image is ineligible.

### Older image is incompatible with current runtime configuration

**Failure:** A verified older image conflicts with the runtime/task configuration proposed now under the tested Sprint 02 model. **Surviving evidence:** historical record, old and proposed runtime identities, tested rule, comparison, and rejection reason. **Required behavior:** #42 defines the narrow identity; identified incompatibility or an unestablished required fact stops rollback before ECS mutation. **Outcome — refuse before mutation:** reject at least one controlled incompatible case without claiming compatibility across untested secrets, schemas, services, IAM, or networking.

## Cost principles

| Guardrail | Rule |
| --- | --- |
| Existing AWS Budget | **$20/month** |
| Sprint 02 target | **<= $3** total AWS cost |
| Hard personal ceiling | **$5**; stop new experiments until unexpected spend is understood. |
| Compute | No new intentionally always-on compute. |
| Paid services | **No new recurring paid service without explicit justification.** |
| Temporary resources | Each has creation, normal cleanup, failure cleanup, applicable fresh-runner recovery, and independent final-state checks. |
| Durable resources | Purpose, lifecycle/retention, and expected cost are documented. |

Crossing the target may be justified; the ceiling is a stop-and-investigate boundary. Record actual cost and the most expensive resource or experiment at sprint completion.

## Definition of Done

- [ ] The development-only target capability and the three failure responses are demonstrated.
- [ ] Remote authoritative state works from a fresh runner with no local state.
- [ ] Conflicting Terraform mutation is prevented or safely serialized.
- [ ] The controlled post-apply interruption is recovered by an operator-triggered fresh-runner procedure.
- [ ] Startup, stabilization, `/health`, and `/version` failures preserve useful bounded evidence, create no verified record, and attempt normal cleanup.
- [ ] #40 records its assigned durable successful-verification fields only after verification.
- [ ] #41 rejects an ECR image lacking matching verified evidence before ECS mutation.
- [ ] #42 defines the tested runtime identity, extends metadata, and rejects at least one incompatible rollback before mutation.
- [ ] Eligible rollback remains operator-triggered, reuses an immutable image, and repeats stability, `/health`, and exact `/version`.
- [ ] Independent inspection confirms no unintended temporary resources in the final resting state.
- [ ] Change-aware CI validates relevant and uncertain changes and skips only demonstrably unrelated work.
- [ ] No excluded capability, intentionally always-on compute, or unjustified recurring paid service is introduced.
- [ ] Actual cost is reviewed against `<= $3`, the `$5` ceiling, and the existing `$20/month` AWS Budget.
- [ ] Each experiment records expectation, observation, evidence, cleanup/recovery, and lesson.
- [ ] Mistakes/incorrect assumptions, open knowledge gaps, next experiment, focused time by issue, approximate cost, and final environment state are recorded.

## Explicit non-goals

- production deployment or readiness;
- EKS/Kubernetes; GitOps/Argo CD;
- multi-account promotion or multi-region architecture;
- blue/green, canary, progressive, or zero-downtime delivery;
- service mesh;
- full Prometheus/Grafana, tracing, SIEM, or paid observability stack;
- automatic rollback or always-running orphan remediation;
- full Terraform migration of the retained environment;
- general release/configuration-management platforms;
- continuous synthetic monitoring or high-availability guarantees.

Terraform ownership remains limited to the verification lifecycle and minimum durable recovery state. Rollback and hard-interruption recovery remain explicit operator actions.

## September issue sequence

GitHub issues are authoritative for their acceptance criteria; this table records dependency ownership only.

| Order | Issue | Capability / dependency |
| ---: | --- | --- |
| 1 | #34 | Define this contract; governs #35-#44. |
| 2 | #35 | Durable remote Terraform state; enables #36/#37. |
| 3 | #36 | State locking and CI backend integration. |
| 4 | #37 | Fresh-runner recovery; requires #35/#36. |
| 5 | #38 | Bounded durable application logs; enables #39. |
| 6 | #39 | Correlated deployment diagnostics. |
| 7 | #40 | Extensible verified-deployment metadata; enables #41. |
| 8 | #41 | Evidence-backed rollback eligibility; separates eligibility from compatibility. |
| 9 | #42 | Tested runtime identity, metadata extension, and incompatible-rollback refusal. |
| 10 | #43 | Conservative change-aware CI. |
| 11 | #44 | Integrated demonstration, cost, cleanup, evidence, and reflection. |

The issue order remains #34 through #44.

## Assumptions and risks

### Assumptions

| Assumption | Validation |
| --- | --- |
| Sprint 01 is a valid foundation. | Integrated demonstration. |
| Runners are disposable; recovery state cannot be runner-only. | #35/#37. |
| Small S3-backed objects suit state and evidence volume. | #35/#40. |
| ECS plus bounded CloudWatch logs suffice for selected diagnostics. | #38/#39. |
| One development environment is sufficient for these learning claims. | #44. |
| Operator-triggered recovery and rollback are acceptable. | #37/#41/#42. |
| Justified persistent resources fit the cost guardrails. | #44 billing review. |

### Risks

| Risk | Required response |
| --- | --- |
| Backend bootstrap cycle | Keep bootstrap explicit and independent. |
| Sensitive Terraform state | Restrict, encrypt, version, and do not publish raw state. |
| Locking mistaken for workflow serialization | Test Terraform conflict directly; retain both controls. |
| Interruption leaves billable resources | Supervise and run documented recovery. |
| Logs or cost grow unexpectedly | Bound retention; pause at ceiling and investigate. |
| Diagnostics fail during failure | Bound and report them; never block cleanup. |
| Records written early or overwritten | Write after verification; protect storage appropriately. |
| Historical verification treated as guarantee | Repeat post-rollback verification. |
| Runtime identity incomplete | Limit claims to #42's tested properties and list exclusions. |
| Conservative compatibility rejects a valid target | Prefer refusal and record the limitation. |
| Change-aware CI skips required checks | Default uncertain changes to validation. |
| Scope expands | Apply non-goals and require direct evidence of necessity. |

When an experiment disproves an assumption, record the original assumption, observed evidence, correction, and resulting scope or architecture change.

## Expected evidence

| Capability / scenario | Minimum direct evidence | Issue |
| --- | --- | --- |
| Remote state | Protected backend and same state accessed from first and fresh runners | #35 |
| Concurrent mutation | Conflict/lock output and one usable authoritative state | #36 |
| Post-apply interruption | Resource identity, absent original workspace, fresh-runner recovery, independent cleanup checks | #37 |
| Startup failure | Stop/exit details, task/image, retained logs, cleanup | #38/#39 |
| Stabilization failure | Timeout, events/state/counts, task/log evidence, cleanup | #39 |
| `/health` failure | Request/result, ECS context, no record, cleanup | #39/#40 |
| `/version` mismatch | Intended/actual version, image/task identity, no record, cleanup | #39/#40 |
| Successful verification | Stability, `/health`, exact `/version`, then #40 record | #40 |
| Unverified image | Image present, record absent, rejection, ECS unchanged | #41 |
| Eligible rollback | Record/environment/image match plus repeated external verification | #41 |
| Runtime incompatibility | Old/current identities, tested rule, rejection, ECS unchanged | #42 |
| Change-aware CI | Controlled docs, app, Dockerfile, Terraform, workflow, and mixed cases | #43 |
| Resting baseline | ECS counts zero and independent absence of temporary resources | #37/#39/#44 |
| Cost | Actual spend, guardrail comparison, cost driver, paid-service justifications | #44 |

Evidence comes from the system responsible for the claim. Workflow success does not prove AWS cleanup; image existence does not prove successful verification. Each experiment preserves expectation, observation, direct evidence, cleanup/recovery, and lesson without publishing sensitive raw state.

## First experiment after Issue #34

Issue #35 asks one narrow handoff question:

> Can authoritative Terraform state for temporary verification infrastructure survive the runner that created it and be accessed from a separate fresh execution?

It establishes the smallest justified durable backend, bootstrap/access boundary, state protection, fresh-runner reconnection evidence, and final cleanup check. It does not prove concurrent mutation or real interrupted-cleanup recovery; those belong to #36 and #37. The GitHub Issue #35 body remains authoritative for detailed acceptance criteria.
