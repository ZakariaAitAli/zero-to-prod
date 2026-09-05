# Change-aware CI and infrastructure validation

## Objective

Issue #43 makes CI choose validation from the files that changed without weakening deployment safety.

The workflow now separates change detection, specialized validation, and the stable merge gate.

The intended branch-protection contract is:

    CI required

The previous `Test and build demo API` requirement was removed so application, Terraform, and workflow validation can run as independent conditional jobs.

The workflow itself still runs for every pull request to `main`.

Workflow-level `paths:` filters are deliberately avoided. Change detection happens inside the workflow, and the always-running aggregate `CI required` job evaluates whether every validation selected by the classifier succeeded.

The workflow gate is implemented in this branch. The repository setting requiring `CI required` must be added only after the new job has been observed successfully on GitHub.

## Design

The workflow classifies changed paths into four independent decisions:

    app
    terraform
    workflow
    deploy

The classifier is:

    scripts/classify-ci-changes.sh

Its regression tests are:

    scripts/test-ci-change-classifier.sh

Unknown paths use conservative validation rather than being assumed safe to skip.

### Path categories

| Changed path | Application | Terraform | Workflow / CI | Publish/deploy |
| --- | --- | --- | --- | --- |
| `docs/**` | no | no | no | no |
| `README.md` | no | no | no | no |
| `LICENSE` | no | no | no | no |
| `apps/demo-api/**` | yes | no | no | yes |
| `infra/terraform/**` | no | yes | no | no |
| `.github/workflows/**` | yes | yes | yes | no |
| `scripts/classify-ci-changes.sh` | yes | yes | yes | no |
| `scripts/test-ci-change-classifier.sh` | yes | yes | yes | no |
| `scripts/verify-ci-required.sh` | yes | yes | yes | no |
| `scripts/test-ci-required-gate.sh` | yes | yes | yes | no |
| `infra/aws/ecs/demo-api-task-definition.json` | yes | yes | no | yes |
| `scripts/verify-deployment.sh` | yes | yes | no | yes |
| unclassified path | yes | yes | yes | no |

Mixed changes take the union of the required validation categories.

Conservative validation does not automatically imply deployment.

### CI control-plane bootstrap guard

The classifier is itself part of the CI policy, so changes to CI-control files must not rely only on classifier output to decide whether CI-policy validation runs.

`Detect changes` therefore has an independent bootstrap guard for:

    .github/workflows/**
    scripts/classify-ci-changes.sh
    scripts/test-ci-change-classifier.sh
    scripts/verify-ci-required.sh
    scripts/test-ci-required-gate.sh

If any of these files change, the workflow forces:

    app=true
    terraform=true
    workflow=true

regardless of the classifier result.

This prevents a broken classifier change from incorrectly returning `workflow=false` and skipping validation of its own policy.

The bootstrap guard does not force:

    deploy=true

so CI-control changes still cannot trigger an AWS deployment by themselves.

## Required status-check behavior

The workflow now uses separate jobs:

    Detect changes
        |
        +-- Validate demo API
        +-- Validate Terraform
        +-- Validate CI workflows
        |
        v
    CI required

`Detect changes` exposes the classifier decisions as job outputs.

The three specialized validation jobs use job-level conditions and therefore run only when their category is required.

The stable aggregate job is:

    CI required

It uses:

    if: always()

so it still runs when one or more specialized jobs are intentionally skipped or fail.

The gate evaluates:

- whether change detection succeeded;
- whether every required validation succeeded;
- whether every non-required validation was actually skipped;
- whether classifier output is valid.

The gate fails closed if classifier output is missing or malformed.

The policy is implemented in:

    scripts/verify-ci-required.sh

and regression-tested by:

    scripts/test-ci-required-gate.sh

This provides one stable branch-protection contract while allowing specialized validation jobs to change independently.

At the time of this implementation update, the old `Test and build demo API` repository requirement has been removed. The new `CI required` repository requirement is intentionally pending until the new job has completed successfully on GitHub.

## Change-range semantics

Pull requests use a three-dot Git range:

    base...head

This compares from the merge base and represents the changes introduced by the pull request.

Pushes to `main` use:

    before..sha

This represents the commits included in that push.

The workflow checkout uses:

    fetch-depth: 0

because both sides of the Git diff must be available.

Manual `workflow_dispatch` executions do not have a meaningful changed-file range, so they deliberately use conservative full static validation.

## Application validation

When `app=true`, the existing validation remains:

    gofmt
    go vet
    go test
    Docker build

Local Issue #43 validation proved:

- Go formatting passed.
- `go vet ./...` passed.
- All automated tests passed.
- Docker build completed successfully.
- The runtime image still uses the non-root `app` user.

Local validation image:

    zero-to-prod-demo-api:issue-43-local-validation

Observed image size:

    6215095 bytes

Observed runtime user:

    app

## Terraform validation

When `terraform=true`, CI runs:

    terraform fmt -check -recursive infra/terraform

It validates both Terraform roots:

    infra/terraform/bootstrap/development-verification-state
    infra/terraform/development-verification

Each root is initialized using:

    terraform init -backend=false -input=false -lockfile=readonly

and then:

    terraform validate

`-backend=false` is intentional.

Static CI validation does not need to access the S3 remote-state backend, acquire AWS credentials, or mutate infrastructure.

Local validation succeeded for both Terraform roots using Terraform `1.15.9` and AWS provider `6.59.0`.

## GitHub Actions validation

Workflow and CI-control changes run `actionlint`.

Pinned version:

    v1.7.11

Command:

    go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.11

`v1.7.11` is compatible with the repository's Go `1.24.2` toolchain.

The classifier and classifier regression-test scripts also receive Bash syntax validation.

Local `actionlint` validation completed with no errors.

## Publish and deployment behavior

The `test-and-build` job exposes the classification result:

    deploy

Image export and artifact upload require:

    push to main
    AND
    deploy=true

`Publish immutable image to ECR` requires the same deployment decision.

`Deploy to development` still depends on successful image publication.

The existing deployment safety mechanisms remain unchanged, including:

- GitHub OIDC authentication;
- development environment protection;
- deployment concurrency;
- stale-deployment rejection;
- Terraform verification infrastructure;
- ECS stability checks;
- external deployment verification;
- cleanup.

A documentation-only push therefore becomes:

    docs-only push to main
        |
        v
    CI required succeeds
        |
        +-- app validation skipped
        +-- Terraform validation skipped
        +-- workflow validation skipped
        +-- no Go/Docker validation
        +-- no image artifact
        +-- no ECR publication
        +-- no OIDC AWS session
        +-- no development deployment

A deploy-relevant push remains:

    deploy-relevant push to main
        |
        v
    application validation
        |
        v
    Docker build
        |
        v
    image artifact
        |
        v
    immutable ECR publication
        |
        v
    development deployment

## Local failure experiments

The classifier regression test covers all six Issue #43 required scenarios plus an additional CI-control policy regression.

### Documentation-only

Input:

    docs/sprint-02/change-aware-ci.md

Observed:

    app=false
    terraform=false
    workflow=false
    deploy=false

Result: PASS.

### Go source change

Input:

    apps/demo-api/cmd/api/main.go

Observed:

    app=true
    terraform=false
    workflow=false
    deploy=true

Result: PASS.

### Dockerfile change

Input:

    apps/demo-api/Dockerfile

Observed:

    app=true
    terraform=false
    workflow=false
    deploy=true

Result: PASS.

### Terraform change

Input:

    infra/terraform/development-verification/main.tf

Observed:

    app=false
    terraform=true
    workflow=false
    deploy=false

Result: PASS.

### Workflow YAML change

Input:

    .github/workflows/demo-api-ci.yml

Observed:

    app=true
    terraform=true
    workflow=true
    deploy=false

Result: PASS.

### Mixed documentation + application change

Inputs:

    docs/sprint-02/change-aware-ci.md
    apps/demo-api/cmd/api/main.go

Observed:

    app=true
    terraform=false
    workflow=false
    deploy=true

Result: PASS.

The regression command:

    ./scripts/test-ci-change-classifier.sh

reports all six required scenarios plus the additional CI-control policy regression as passing.

## Live GitHub experiments

Local classifier tests prove the classification rules, but they do not prove actual GitHub required-status behavior.

Before closing Issue #43, test:

- docs-only PR;
- Go source change;
- Dockerfile change;
- Terraform change;
- workflow YAML change;
- mixed documentation + application change.

For the docs-only experiment, specifically verify that:

    CI required

appears and succeeds while all three specialized validation jobs are intentionally skipped.

After the first successful `CI required` run is observed, configure that exact check as the repository's required status check before merging the implementation.

These experiments should be performed after the change-aware workflow exists on `main`, allowing genuinely isolated PRs to test each category.

Issue #43 should not be closed until this evidence is recorded.

## Mistakes and knowledge gaps

### Workflow-level filtering can break required checks

A required workflow must not simply receive workflow-level `paths:` filtering when GitHub expects a status from that workflow.

If the workflow never starts, the required check may never appear.

The safe design keeps the workflow trigger unconditional and uses a stable aggregate gate that always runs. Specialized jobs may skip through job-level conditions, while `CI required` evaluates those results explicitly.

### The first mixed-change test harness relied on shell word splitting

The initial manual mixed-change experiment stored two paths in one scalar and expanded the variable assuming the shell would split it into separate arguments.

The interactive shell did not behave as assumed.

The classifier received one combined argument beginning with `docs/` and therefore classified it as documentation-only.

The classifier itself was correct.

The test was corrected by passing both paths as explicit arguments.

The regression script now uses Bash and explicit argument lists.

### Latest actionlint did not match the repository Go version

The first workflow-lint attempt used:

    actionlint v1.7.12

That release requires Go `>= 1.25`.

The repository currently pins Go `1.24.2`, causing local Go to automatically download a newer toolchain.

The implementation was therefore changed to:

    actionlint v1.7.11

which is compatible with the repository's Go version.

### PR and push diff ranges are different

The first implementation used the same two-commit diff approach for both pull requests and pushes.

This was corrected so:

    pull request -> base...head
    push         -> before..sha

The pull-request form uses the merge base, while the push form represents the exact pushed range.

## Cost impact

All implementation and local validation so far used:

- Git;
- Go;
- Docker;
- Terraform static validation;
- actionlint.

No AWS infrastructure was created or changed during Issue #43 implementation validation.

Expected AWS cost is negligible.

## Remaining evidence before closure

- [x] Path categories designed before workflow editing.
- [x] Stable aggregate `CI required` gate implemented.
- [x] Required-gate policy extracted into an executable script.
- [x] Required-gate regression tests pass locally.
- [x] Gate fails closed on missing or malformed classifier state.
- [x] Application checks conditional on application relevance.
- [x] Terraform formatting validation implemented.
- [x] Terraform configuration validation implemented.
- [x] Workflow linting implemented.
- [x] Deployment publishing gated by explicit deploy relevance.
- [x] Six required classifier scenarios plus the CI-control policy regression pass locally.
- [x] Classifier regression tests implemented as executable tests.
- [x] Terraform validation passes locally.
- [x] Workflow linting passes locally.
- [x] Application formatting, vet, tests, and Docker build pass locally.
- [ ] Live `CI required` job succeeds on the refactored implementation PR.
- [ ] Repository rules updated to require `CI required`.
- [ ] Live docs-only PR proves `CI required` succeeds with specialized jobs skipped.
- [ ] Live Go-source PR behavior recorded.
- [ ] Live Dockerfile PR behavior recorded.
- [ ] Live Terraform PR behavior recorded.
- [ ] Live workflow-YAML PR behavior recorded.
- [ ] Live mixed docs + application PR behavior recorded.
- [ ] Final implementation PR evidence recorded.
- [ ] Actual focused time recorded.

## Next experiment

Run the refactored workflow on PR #48 and verify that:

    Detect changes
    Validate demo API
    Validate Terraform
    Validate CI workflows
    CI required

behave according to the classifier.

After `CI required` succeeds on GitHub, configure that exact job as the repository's required status check.

Do not merge PR #48 until the new required gate has been observed and configured.

After the implementation exists on `main`, create controlled PR experiments for each path category and record the actual GitHub Actions behavior before closing Issue #43.
