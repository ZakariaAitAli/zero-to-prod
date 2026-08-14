Using the same structure and level of detail as your Issue #6 documentation.

# Sprint 01 — Push Immutable Images to ECR

## Purpose

Issue #6 established secure authentication between GitHub Actions and the AWS sandbox using GitHub OIDC and AWS STS.

At the end of Issue #6:

```text
GitHub → AWS authentication      ✅
ECR authorization               ❌
ECS authorization               ❌
```

Issue #7 adds the minimum authorization required for GitHub Actions to publish the Docker image produced by CI to the existing private Amazon ECR repository.

Repository:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api
```

AWS account:

```text
333534066371
```

Region:

```text
eu-west-3
```

The image is tagged with the exact Git commit SHA instead of a mutable tag such as:

```text
latest
```

The intended image reference is:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:<GITHUB_SHA>
```

The ECR repository uses immutable tags.

This means that once a Git commit SHA is associated with an image manifest, that tag cannot later be moved to a different image.

The capability established by Issue #7 is:

```text
GitHub main commit
        ↓
tests + Docker build
        ↓
exact built image transferred between jobs
        ↓
GitHub OIDC
        ↓
AWS STS temporary credentials
        ↓
Docker authenticates to ECR
        ↓
least-privilege ECR authorization
        ↓
SHA-tagged image pushed
        ↓
immutable image stored in ECR
```

No permanent AWS credentials are stored in GitHub.

No ECS deployment permissions are introduced in this issue.

---

## Existing ECR repository

Before changing IAM or GitHub Actions, the existing repository was inspected:

```bash
aws ecr describe-repositories \
  --repository-names zero-to-prod-demo-api \
  --region eu-west-3 \
  --profile sandbox
```

Verified configuration:

```text
Repository ARN:
arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api

Repository URI:
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api

Tag mutability:
IMMUTABLE

Encryption:
AES256

Scan on push:
false
```

The repository already existed before Issue #7.

GitHub Actions therefore does not need permission to create or administer repositories.

Because the repository uses ECR-managed AES256 encryption rather than a customer-managed KMS key, the GitHub Actions role does not require KMS permissions.

---

## Authentication vs authorization

Authentication and authorization remain separate concerns.

Issue #6 established authentication:

```text
GitHub Actions
      ↓
GitHub OIDC token
      ↓
AWS STS
      ↓
zero-to-prod-github-actions
      ↓
temporary AWS credentials
```

Issue #7 adds authorization:

```text
temporary AWS credentials
      ↓
IAM permission policy
      ↓
allowed ECR API operations
```

The IAM trust policy answers:

```text
Who may become this role?
```

The ECR permission policy answers:

```text
What may this role do after authentication?
```

The complete relationship is:

```text
GitHub workload identity
        ↓
IAM trust policy
        ↓
may assume zero-to-prod-github-actions
        ↓
temporary credentials
        ↓
IAM permission policy
        ↓
may push to one ECR repository
```

Successful OIDC authentication alone does not imply permission to use ECR.

---

## ECR push flow

A Docker push to Amazon ECR is not represented by one IAM action such as:

```text
ecr:PushImage
```

There is no such single action.

A push consists of several ECR API operations.

First, Docker authenticates to the registry.

The AWS CLI command:

```bash
aws ecr get-login-password
```

uses:

```text
ecr:GetAuthorizationToken
```

The resulting temporary ECR password is passed to Docker:

```text
temporary AWS credentials
        ↓
ecr:GetAuthorizationToken
        ↓
ECR registry password
        ↓
docker login
```

Once Docker is authenticated, the image push uses repository-level operations.

Conceptually:

```text
Docker image
     ↓
BatchCheckLayerAvailability
     ↓
does layer already exist?
   ┌───────┴───────┐
   │               │
  yes              no
   │               ↓
   │      InitiateLayerUpload
   │               ↓
   │        UploadLayerPart
   │               ↓
   │      CompleteLayerUpload
   │
   └───────────┐
               ↓
            PutImage
               ↓
image manifest + SHA tag stored
```

---

## IAM permissions

The GitHub Actions role is:

```text
zero-to-prod-github-actions
```

Before Issue #7 it had no attached or inline permission policies.

This was verified using:

```bash
aws iam list-attached-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

Result:

```json
{
  "AttachedPolicies": []
}
```

Inline policies:

```bash
aws iam list-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

Result:

```json
{
  "PolicyNames": []
}
```

Issue #7 adds an inline permission policy named:

```text
zero-to-prod-ecr-push
```

The policy source is stored in:

```text
infra/aws/iam/github-actions-ecr-push-policy.json
```

The policy contains:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AuthenticateToECR",
      "Effect": "Allow",
      "Action": "ecr:GetAuthorizationToken",
      "Resource": "*"
    },
    {
      "Sid": "PushImageToDemoApiRepository",
      "Effect": "Allow",
      "Action": [
        "ecr:BatchCheckLayerAvailability",
        "ecr:BatchGetImage",
        "ecr:CompleteLayerUpload",
        "ecr:InitiateLayerUpload",
        "ecr:PutImage",
        "ecr:UploadLayerPart"
      ],
      "Resource": "arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api"
    }
  ]
}
```

---

## Repository-scoped permissions

The following ECR actions are restricted to the exact target repository:

```text
ecr:BatchCheckLayerAvailability
ecr:BatchGetImage
ecr:CompleteLayerUpload
ecr:InitiateLayerUpload
ecr:PutImage
ecr:UploadLayerPart
```

Resource:

```text
arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api
```

The GitHub role therefore does not receive push authorization for:

```text
arn:aws:ecr:eu-west-3:333534066371:repository/*
```

and does not receive:

```text
Resource: "*"
```

for repository operations.

The one exception is:

```text
ecr:GetAuthorizationToken
```

This action does not support repository-level resource scoping.

It therefore requires:

```json
"Resource": "*"
```

This does not allow GitHub Actions to push to every ECR repository.

It only allows the role to obtain an ECR registry authentication token.

The actual repository operations are independently authorized by the second IAM statement.

---

## Permissions deliberately not granted

Issue #7 does not grant:

```text
ecr:CreateRepository
ecr:DeleteRepository
ecr:BatchDeleteImage
ecr:SetRepositoryPolicy
ecr:DeleteRepositoryPolicy
ecr:PutLifecyclePolicy
ecr:PutImageTagMutability
ecr:StartImageScan
```

The GitHub Actions role therefore cannot:

```text
create ECR repositories
delete ECR repositories
delete images
change repository policies
change tag mutability
change lifecycle configuration
```

No ECS permissions are added.

---

## IAM policy validation

The policy was first validated locally using:

```bash
jq . infra/aws/iam/github-actions-ecr-push-policy.json
```

AWS IAM Access Analyzer was then used:

```bash
aws accessanalyzer validate-policy \
  --policy-document file://infra/aws/iam/github-actions-ecr-push-policy.json \
  --policy-type IDENTITY_POLICY \
  --region eu-west-3 \
  --profile sandbox
```

Result:

```json
{
  "findings": []
}
```

The policy was then attached to the role:

```bash
aws iam put-role-policy \
  --role-name zero-to-prod-github-actions \
  --policy-name zero-to-prod-ecr-push \
  --policy-document file://infra/aws/iam/github-actions-ecr-push-policy.json \
  --profile sandbox
```

The stored AWS-side policy was read back with:

```bash
aws iam get-role-policy \
  --role-name zero-to-prod-github-actions \
  --policy-name zero-to-prod-ecr-push \
  --profile sandbox
```

This verified that the intended policy was actually stored in IAM.

---

## GitHub Actions implementation

The CI workflow is:

```text
.github/workflows/demo-api-ci.yml
```

The existing AWS job from Issue #6 remains dependent on:

```yaml
needs: test-and-build
```

It also remains restricted to:

```yaml
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
```

OIDC permission remains scoped only to this AWS job:

```yaml
permissions:
  id-token: write
```

A pull request therefore cannot execute the AWS/ECR job.

---

## ECR registry authentication

After OIDC authentication and STS role assumption, Docker logs in to ECR using:

```bash
aws ecr get-login-password --region eu-west-3 \
  | docker login \
      --username AWS \
      --password-stdin 333534066371.dkr.ecr.eu-west-3.amazonaws.com
```

This uses the temporary AWS credentials established by:

```text
aws-actions/configure-aws-credentials
```

No static ECR password is stored in GitHub.

No permanent:

```text
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
```

is stored in GitHub.

---

## Build-once artifact handoff

The original CI already built the Docker image using:

```text
zero-to-prod-demo-api:${GITHUB_SHA}
```

However, the build job and AWS job execute on separate GitHub-hosted runners.

A Docker image stored in the first runner's local Docker daemon does not automatically exist on the second runner.

The initial architecture was therefore:

```text
test-and-build runner
      ↓
Docker image exists locally
      ↓
runner ends
      ↓
image disappears

AWS runner
      ↓
fresh Docker daemon
      ↓
image unavailable
```

Rather than rebuilding the image, Issue #7 transfers the exact image produced by CI.

The build job performs:

```text
docker build
     ↓
docker save
     ↓
demo-api-image.tar
     ↓
upload GitHub Actions artifact
```

The AWS job performs:

```text
download artifact
     ↓
docker load
     ↓
same Docker image restored
```

This means the image pushed to ECR is the exact image artifact built by the CI job rather than a second independently rebuilt image.

---

## Main-only artifact transfer

The image is exported only on a push to `main`:

```yaml
- name: Export Docker image
  if: github.event_name == 'push' && github.ref == 'refs/heads/main'
```

This avoids artifact storage and transfer during normal pull-request validation.

The image is saved with:

```bash
docker save \
  --output demo-api-image.tar \
  "${image}"
```

The artifact is uploaded with one-day retention.

Conceptually:

```text
pull_request
   ↓
build + test
   ↓
no image export
no artifact upload
no AWS access
```

versus:

```text
push → main
   ↓
build + test
   ↓
docker save
   ↓
artifact upload
   ↓
AWS job
   ↓
ECR push
```

---

## Artifact retention

The Docker image artifact is retained for:

```text
1 day
```

The artifact exists only to transfer the image between two jobs in the same workflow execution.

Long-term artifact storage is unnecessary because ECR becomes the durable image registry after a successful push.

This keeps GitHub Actions artifact storage usage low.

---

## SHA-based image tagging

The image tag is based on:

```text
GITHUB_SHA
```

For a successful `main` push:

```text
zero-to-prod-demo-api:${GITHUB_SHA}
```

is retagged as:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:${GITHUB_SHA}
```

No:

```text
latest
```

tag is used.

No mutable deployment alias is introduced in Issue #7.

This produces a direct relationship between source code and container image:

```text
Git commit SHA
      ↓
Docker image tag
      ↓
ECR image digest
```

---

## Pull request SHA behavior

During a `pull_request` workflow, GitHub may set:

```text
GITHUB_SHA
```

to the synthetic PR merge commit rather than the feature branch head commit.

This was observed during the PR run.

The PR head commit and the image built during the PR had different SHAs because CI was testing GitHub's temporary merge result.

This does not affect ECR publishing because ECR publishing runs only on:

```text
push → main
```

The final image therefore uses the actual commit present on `main`.

---

## Successful verification

PR #19 was merged:

```text
Push immutable images to ECR
```

The resulting `main` workflow run was:

```text
31796560692
```

Overall result:

```text
success
```

The build job completed:

```text
Build Docker image             ✅
Export Docker image            ✅
Upload Docker image artifact   ✅
```

The AWS job completed:

```text
Download Docker image artifact ✅
Load Docker image              ✅
Tag image for Amazon ECR       ✅
Configure AWS credentials      ✅
Verify AWS identity            ✅
Log in to Amazon ECR           ✅
Push image to Amazon ECR       ✅
```

This proved the complete workflow:

```text
GitHub main commit
      ↓
CI tests
      ↓
Docker build
      ↓
exact artifact transfer
      ↓
GitHub OIDC
      ↓
AWS STS
      ↓
temporary credentials
      ↓
ECR registry login
      ↓
repository-scoped push
      ↓
ECR image
```

---

## ECR-side verification

GitHub reporting a successful push is not enough by itself.

The image was independently verified from the ECR side using:

```bash
aws ecr describe-images \
  --repository-name zero-to-prod-demo-api \
  --region eu-west-3 \
  --profile sandbox \
  --query 'imageDetails[].{Tags:imageTags,Digest:imageDigest,PushedAt:imagePushedAt,Size:imageSizeInBytes}' \
  --output json
```

Result:

```json
[
  {
    "Tags": [
      "114b45f54aea153f89602e974975e28328bb0bc4"
    ],
    "Digest": "sha256:1927a012a6697f28d36fb4ff539b3289101f4ff2e52287bc69b15016ab59603c",
    "PushedAt": "2026-08-14T12:32:59.717000+01:00",
    "Size": 6213227
  }
]
```

The tag:

```text
114b45f54aea153f89602e974975e28328bb0bc4
```

was verified as the GitHub merge commit for PR #19.

This establishes provenance:

```text
GitHub merge commit
114b45f54aea153f89602e974975e28328bb0bc4
        ↓
ECR image tag
114b45f54aea153f89602e974975e28328bb0bc4
        ↓
ECR image digest
sha256:1927a012a6697f28d36fb4ff539b3289101f4ff2e52287bc69b15016ab59603c
```

---

## Failure experiments

### Failure experiment 1 — unauthorized repository

IAM least privilege was tested before the real push.

The role policy was simulated for:

```text
ecr:PutImage
```

against both:

```text
arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api
```

and:

```text
arn:aws:ecr:eu-west-3:333534066371:repository/not-allowed
```

Command:

```bash
aws iam simulate-principal-policy \
  --policy-source-arn arn:aws:iam::333534066371:role/zero-to-prod-github-actions \
  --action-names ecr:PutImage \
  --resource-arns \
    arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api \
    arn:aws:ecr:eu-west-3:333534066371:repository/not-allowed \
  --profile sandbox
```

Result:

```text
zero-to-prod-demo-api → allowed
not-allowed           → implicitDeny
```

The unauthorized repository received:

```text
implicitDeny
```

This proves the repository resource boundary.

Authentication and authorization remained separate:

```text
GitHub may become IAM role      ✅

IAM role may PutImage to target ✅

IAM role may PutImage elsewhere ❌
```

---

### Failure experiment 2 — duplicate image

After the successful push, the existing image manifest was retrieved and an attempt was made to put the same manifest using the same existing tag.

ECR returned:

```text
ImageAlreadyExistsException
```

The error reported that the same digest and tag already existed.

This demonstrates duplicate-image detection.

It did not yet prove tag immutability because both the tag and manifest were unchanged.

---

### Failure experiment 3 — immutable tag overwrite

To test actual tag immutability, a semantically equivalent manifest was serialized differently.

Existing ECR manifest digest:

```text
sha256:1927a012a6697f28d36fb4ff539b3289101f4ff2e52287bc69b15016ab59603c
```

Altered candidate manifest digest:

```text
sha256:24aa90de12f87f51e9d58c3b32b5f016722369ad339932a5f0d68fd7ec116b69
```

The digests were different.

The altered manifest was then submitted using the existing SHA tag:

```text
114b45f54aea153f89602e974975e28328bb0bc4
```

ECR rejected the write:

```text
ImageTagAlreadyExistsException
```

Error:

```text
The image tag '114b45f54aea153f89602e974975e28328bb0bc4'
already exists in the 'zero-to-prod-demo-api' repository and cannot
be overwritten because the tag is immutable.
```

This proves:

```text
existing SHA tag
      ↓
currently points to digest A
      ↓
attempt to point to digest B
      ↓
ECR rejects write
```

---

## Post-failure verification

After the failed overwrite, the existing image was queried again:

```bash
aws ecr describe-images \
  --repository-name zero-to-prod-demo-api \
  --image-ids imageTag="$TAG" \
  --region eu-west-3 \
  --profile sandbox \
  --query 'imageDetails[0].{Tags:imageTags,Digest:imageDigest,PushedAt:imagePushedAt}' \
  --output json
```

Result:

```json
{
  "Tags": [
    "114b45f54aea153f89602e974975e28328bb0bc4"
  ],
  "Digest": "sha256:1927a012a6697f28d36fb4ff539b3289101f4ff2e52287bc69b15016ab59603c",
  "PushedAt": "2026-08-14T12:32:59.717000+01:00"
}
```

The digest remained unchanged.

Therefore:

```text
failed overwrite
      ↓
no mutation of existing tag
      ↓
original tag → digest mapping preserved ✅
```

---

## Security decisions

### OIDC remains the authentication mechanism

GitHub Actions does not use permanent AWS credentials.

Authentication remains:

```text
GitHub OIDC
      ↓
AWS STS
      ↓
temporary credentials
```

### Authorization is repository-scoped

Push operations are restricted to:

```text
arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api
```

### `GetAuthorizationToken` is separated

Registry authentication requires:

```text
ecr:GetAuthorizationToken
```

with:

```text
Resource: "*"
```

Repository operations remain separately scoped.

### No ECS permissions

Issue #7 stops at ECR.

The GitHub Actions role cannot deploy the image to ECS yet.

### No ECR administrative permissions

The role cannot:

```text
create repositories
delete repositories
delete images
change tag mutability
change lifecycle configuration
change repository policies
```

### SHA-only publishing

Images are published using the complete Git commit SHA.

No:

```text
latest
```

tag is used.

### Immutable tags

The ECR repository is configured with:

```text
IMMUTABLE
```

A real overwrite attempt was rejected.

### PRs cannot access AWS

The AWS/ECR job remains restricted to:

```text
push → main
```

A PR run showed:

```text
Test and build demo API        ✅ success
Verify GitHub to AWS OIDC      ⏭ skipped
```

The export and artifact-upload steps were also skipped during PR validation.

### GitHub Actions pinned by SHA

Security-sensitive third-party actions are pinned to exact commit SHAs rather than only version tags.

---

## Cost considerations

No continuously running AWS resources were created during Issue #7.

Existing resource:

```text
Amazon ECR repository
```

Additional usage consists primarily of:

```text
ECR image storage
GitHub Actions execution
short-lived GitHub artifact storage
```

The first ECR image is approximately:

```text
6.2 MB compressed in ECR
```

The Docker image reported by the CI Docker daemon before registry compression was approximately:

```text
14 MB
```

The transfer artifact is retained for only:

```text
1 day
```

This avoids unnecessary long-term GitHub artifact storage.

No:

```text
ECS task
load balancer
database
NAT gateway
EC2 instance
```

was created for Issue #7.

---

## Verification

### Verify repository configuration

```bash
aws ecr describe-repositories \
  --repository-names zero-to-prod-demo-api \
  --region eu-west-3 \
  --profile sandbox
```

Expected:

```text
imageTagMutability = IMMUTABLE
```

### Validate IAM policy JSON

```bash
jq . infra/aws/iam/github-actions-ecr-push-policy.json
```

### Validate policy with Access Analyzer

```bash
aws accessanalyzer validate-policy \
  --policy-document file://infra/aws/iam/github-actions-ecr-push-policy.json \
  --policy-type IDENTITY_POLICY \
  --region eu-west-3 \
  --profile sandbox
```

Expected:

```json
{
  "findings": []
}
```

### Inspect stored role policy

```bash
aws iam get-role-policy \
  --role-name zero-to-prod-github-actions \
  --policy-name zero-to-prod-ecr-push \
  --profile sandbox
```

### Validate workflow

```bash
actionlint .github/workflows/demo-api-ci.yml
```

### Check repository image

```bash
aws ecr describe-images \
  --repository-name zero-to-prod-demo-api \
  --region eu-west-3 \
  --profile sandbox
```

### Check exact SHA tag

```bash
aws ecr describe-images \
  --repository-name zero-to-prod-demo-api \
  --image-ids imageTag=114b45f54aea153f89602e974975e28328bb0bc4 \
  --region eu-west-3 \
  --profile sandbox
```

---

## Cleanup procedure

The ECR repository is required by later Sprint 01 deployment work and should not be deleted yet.

The IAM ECR push policy is also required while GitHub Actions continues publishing images.

If ECR publishing is retired, remove the inline role policy:

```bash
aws iam delete-role-policy \
  --role-name zero-to-prod-github-actions \
  --policy-name zero-to-prod-ecr-push \
  --profile sandbox
```

Verify:

```bash
aws iam list-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

If the repository itself is eventually retired, inspect its images first:

```bash
aws ecr describe-images \
  --repository-name zero-to-prod-demo-api \
  --region eu-west-3 \
  --profile sandbox
```

Delete images only as part of an intentional repository cleanup.

Do not remove the GitHub OIDC provider or IAM role merely because ECR publishing is removed if later Sprint issues still use them.

---

## Troubleshooting and lessons learned

### A successful OIDC login does not mean ECR push permission

Issue #6 proved:

```text
GitHub → AWS authentication
```

Issue #7 separately added:

```text
AWS role → ECR authorization
```

These should be tested independently.

### `GetAuthorizationToken` cannot be repository-scoped

The ECR registry token permission requires:

```text
Resource: "*"
```

This does not imply wildcard repository push access.

Actual push operations remain repository-scoped.

### A Docker push is multiple API operations

There is no:

```text
ecr:PushImage
```

permission.

Docker/ECR performs layer checks, uploads, completion, and manifest creation through several APIs.

### Separate GitHub jobs do not share Docker daemon state

The Docker image built in one GitHub-hosted job is not automatically present in another job.

The exact built image therefore had to be transferred explicitly.

### Build once, promote the exact artifact

Instead of rebuilding in the AWS job:

```text
build
  ↓
docker save
  ↓
artifact
  ↓
docker load
  ↓
push
```

was used.

This reduces the risk of testing one image and publishing a separately rebuilt one.

### Pull-request `GITHUB_SHA` is different

During PR workflows, `GITHUB_SHA` may identify GitHub's synthetic merge commit.

Publishing is restricted to `main`, so production image identity uses the actual commit present on `main`.

### `ImageAlreadyExistsException` is not the immutability test

Re-submitting the exact same manifest with the exact same tag resulted in:

```text
ImageAlreadyExistsException
```

This means the identical image already exists.

It does not prove that the tag cannot point to a different manifest.

### `ImageTagAlreadyExistsException` proves immutability

Submitting a different manifest digest under the existing SHA tag returned:

```text
ImageTagAlreadyExistsException
```

This was the actual proof that immutable tags prevent tag reassignment.

### Verify state after a failed write

After the immutability failure, ECR was queried again.

The original digest was unchanged.

A failed write should not merely be assumed harmless; the resulting state should be verified.

### GitHub CLI PR edit issue

Attempting to update the PR body with:

```text
gh pr edit
```

returned a GraphQL error related to deprecated GitHub Projects classic fields.

The PR body was verified afterward and had not changed.

This reinforced the rule:

```text
command attempted
      ↓
verify resulting state
      ↓
do not assume success or failure effects
```

### Validate before changing cloud state

The workflow used several validation layers before the first real push:

```text
jq
      ↓
IAM Access Analyzer
      ↓
actionlint
      ↓
git diff --check
      ↓
PR CI
      ↓
IAM policy simulation
      ↓
main push
      ↓
ECR-side verification
```

This reduced the amount of debugging required against live AWS state.

---

## Reflection

Issue #7 changed the mental model from:

```text
"CI needs permission to push a Docker image"
```

to:

```text
GitHub authenticates to AWS
        ↓
AWS authorizes registry authentication
        ↓
AWS authorizes repository operations
        ↓
Docker pushes several layers and a manifest
        ↓
ECR assigns an immutable Git SHA tag to a digest
```

The most important lesson was that authentication, authorization, artifact identity, and immutability are separate controls.

A secure pipeline requires all four.

### Authentication

```text
Who is the workload?
```

Answer:

```text
GitHub Actions on main
```

### Authorization

```text
What may the workload do?
```

Answer:

```text
push only to zero-to-prod-demo-api
```

### Artifact identity

```text
Which source revision produced this image?
```

Answer:

```text
full Git commit SHA
```

### Immutability

```text
Can this source identity later point to different bytes?
```

Answer:

```text
No — ECR rejects tag reassignment.
```

---

## Mistakes and corrections

### Assumed the PR branch push would trigger the workflow

The workflow was configured for:

```text
push → main
pull_request → main
```

A direct push to the feature branch did not trigger CI.

The behavior was verified from GitHub rather than assumed.

### Initially tested the same manifest for immutability

The first `put-image` experiment reused both:

```text
same tag
same manifest
```

ECR returned:

```text
ImageAlreadyExistsException
```

That tested duplicate detection rather than tag immutability.

The experiment was corrected by creating a manifest with a different digest and attempting to assign it to the existing tag.

That correctly produced:

```text
ImageTagAlreadyExistsException
```

### Needed explicit artifact transfer between jobs

Initially, the build image existed only in the build runner.

Inspecting the workflow showed there was no:

```text
docker save/load
upload-artifact
download-artifact
```

mechanism.

The design was adjusted to explicitly transfer the exact tested image.

### PR description became stale during implementation

The original PR body said:

```text
no image push added yet
```

after image push support had already been implemented.

The PR description was updated before merge so the review artifact accurately described the final scope.

---

## Knowledge gaps identified

Topics worth exploring further include:

```text
OCI image manifests and indexes
Docker image digest calculation
ECR layer deduplication
multi-platform images
BuildKit / docker buildx
image signing
SBOM generation
provenance attestations
ECR vulnerability scanning
ECR lifecycle policies
cross-account ECR access
```

These were not required to prove Issue #7 and were deliberately kept outside the implementation scope.

---

## Focused work performed

Issue #7 included:

```text
ECR repository inspection
IAM permission mapping
repository-level resource scoping
Access Analyzer validation
IAM policy deployment
IAM stored-state verification
IAM policy simulation
GitHub Actions artifact architecture
Docker save/load workflow
SHA tagging
OIDC/ECR integration
PR security-boundary verification
successful main push
ECR-side verification
duplicate-image failure test
immutable-tag overwrite failure test
post-failure state verification
documentation
```

The work emphasized evidence-driven changes rather than applying the full pipeline at once.

---

## Result

Issue #7 established a production-style image publishing path:

```text
Git commit on main
      ↓
GitHub Actions
      ↓
tests
      ↓
Docker build
      ↓
exact image artifact
      ↓
GitHub OIDC
      ↓
AWS STS
      ↓
temporary credentials
      ↓
ECR registry authentication
      ↓
least-privilege repository authorization
      ↓
SHA-tagged image
      ↓
immutable ECR repository
```

Verified image:

```text
Repository:
zero-to-prod-demo-api

Tag:
114b45f54aea153f89602e974975e28328bb0bc4

Digest:
sha256:1927a012a6697f28d36fb4ff539b3289101f4ff2e52287bc69b15016ab59603c
```

Security properties now proven:

```text
No permanent GitHub AWS credentials       ✅

GitHub OIDC authentication                ✅

main-only AWS job                         ✅

least-privilege ECR repository access     ✅

unauthorized repository denied            ✅

build once / push same image               ✅

full Git SHA image tags                    ✅

ECR tag immutability                      ✅

real immutable overwrite rejected         ✅

ECR image independently verified          ✅

ECS permissions                           ❌ not added yet
```

---

## Next experiment

The next capability should consume the immutable ECR image rather than rebuild it.

The next experiment should answer:

```text
How does a deployment system select and run the exact image that CI already published?
```

The desired direction is:

```text
Git commit
    ↓
immutable ECR image
    ↓
deployment definition
    ↓
runtime platform
```

The next issue should continue preserving the separation between:

```text
image publication
```

and:

```text
workload deployment
```

so ECR permissions and ECS permissions remain independently understandable and testable.
