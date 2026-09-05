# Change-aware CI and infrastructure validation

## Objective

Issue #43 makes CI choose validation from the files that changed without weakening the required `main` status check or deployment safety.

The existing required check remains:

    Test and build demo API

The workflow itself still runs for every pull request to `main`.

Change detection happens inside that required job rather than through workflow-level `paths:` filters. Documentation-only pull requests therefore still receive a successful required check instead of leaving the required check absent.

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
| `infra/aws/ecs/demo-api-task-definition.json` | yes | yes | no | yes |
| `scripts/verify-deployment.sh` | yes | yes | no | yes |
| unclassified path | yes | yes | yes | no |

Mixed changes take the union of the required validation categories.

Conservative validation does not automatically imply deployment.

## Required status-check behavior

The required job remains:

    jobs:
      test-and-build:
        name: Test and build demo API

The job itself is not path-filtered.

For an unrelated change, the job still:

1. checks out the repository;
2. detects and classifies changed paths;
3. records the classification;
4. explicitly reports that application validation was skipped;
5. succeeds if no relevant validation failed.

Therefore a documentation-only PR still reports:

    Test and build demo API

This avoids the failure mode already observed where filtering out the whole workflow caused the required status check never to appear.

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
    required check succeeds
        |
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

The classifier regression test covers all six required scenarios.

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

reports all six scenarios as passing.

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

    Test and build demo API

appears and succeeds even though Go and Docker validation are skipped.

These experiments should be performed after the change-aware workflow exists on `main`, allowing genuinely isolated PRs to test each category.

Issue #43 should not be closed until this evidence is recorded.

## Mistakes and knowledge gaps

### Workflow-level filtering can break required checks

A required workflow must not simply receive workflow-level `paths:` filtering when GitHub expects a status from that workflow.

If the workflow never starts, the required check may never appear.

The safe design keeps the required job present and performs path-aware decisions inside it.

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
- [x] Required job name preserved.
- [x] Application checks conditional on application relevance.
- [x] Terraform formatting validation implemented.
- [x] Terraform configuration validation implemented.
- [x] Workflow linting implemented.
- [x] Deployment publishing gated by explicit deploy relevance.
- [x] Six classifier scenarios pass locally.
- [x] Classifier regression tests implemented as executable tests.
- [x] Terraform validation passes locally.
- [x] Workflow linting passes locally.
- [x] Application formatting, vet, tests, and Docker build pass locally.
- [ ] Live docs-only PR proves required check still appears.
- [ ] Live Go-source PR behavior recorded.
- [ ] Live Dockerfile PR behavior recorded.
- [ ] Live Terraform PR behavior recorded.
- [ ] Live workflow-YAML PR behavior recorded.
- [ ] Live mixed docs + application PR behavior recorded.
- [ ] Final implementation PR evidence recorded.
- [ ] Actual focused time recorded.

## Next experiment

Commit and review the change-aware CI implementation without automatically closing Issue #43.

After the implementation exists on `main`, create controlled PR experiments for each path category and verify actual GitHub Actions behavior before closing Issue #43.
