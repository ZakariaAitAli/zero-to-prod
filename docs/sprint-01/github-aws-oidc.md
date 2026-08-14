# Sprint 01 — GitHub-to-AWS OIDC

## Purpose

GitHub Actions needs access to the AWS sandbox in order to push container images and deploy workloads during Sprint 01.

Permanent AWS access keys are deliberately not stored in GitHub.

Instead, GitHub Actions authenticates to AWS using OpenID Connect (OIDC). GitHub provides a short-lived identity token, AWS validates that identity, and AWS STS returns temporary AWS credentials when the configured trust conditions are satisfied.

The authentication path is:

```text
GitHub Actions job
        ↓
job is allowed to request an OIDC token
        ↓
GitHub OIDC token
        ↓
AWS STS
        ↓
IAM role trust policy validation
        ↓
temporary AWS credentials
```

This establishes machine-to-machine authentication between GitHub Actions and the AWS sandbox without maintaining long-lived credentials.

## Authentication flow

The GitHub Actions job receives:

```yaml
permissions:
  id-token: write
```

This permission does not grant access to AWS resources.

It allows the job to request a short-lived OIDC identity token from GitHub.

The workflow then uses:

```text
aws-actions/configure-aws-credentials
```

to exchange that GitHub identity token with AWS STS.

AWS evaluates the trust policy attached to the target IAM role.

The trust policy verifies that:

* the token is intended for AWS STS;
* the token represents the expected GitHub repository;
* the workflow identity comes from the allowed branch.

When those conditions are satisfied, AWS STS creates temporary credentials for the IAM role.

The resulting flow is:

```text
GitHub Actions
      ↓
id-token: write
      ↓
GitHub issues OIDC token
      ↓
AWS STS: AssumeRoleWithWebIdentity
      ↓
IAM trust policy
      ↓
audience + repository + branch validated
      ↓
temporary AWS credentials
```

Receiving temporary credentials only proves authentication.

The IAM role still requires permission policies before it can access services such as Amazon ECR or ECS.

At the end of Issue #6:

```text
GitHub → AWS authentication      ✅

ECR authorization               ❌ not added yet
ECS authorization               ❌ not added yet
```

Authorization will be introduced separately so that authentication and permissions can be tested independently.

## AWS OIDC provider

The GitHub Actions identity provider is registered in the sandbox AWS account.

AWS account:

```text
333534066371
```

Provider URL:

```text
https://token.actions.githubusercontent.com
```

Audience:

```text
sts.amazonaws.com
```

Provider ARN:

```text
arn:aws:iam::333534066371:oidc-provider/token.actions.githubusercontent.com
```

The provider is tagged with:

```text
Project=zero-to-prod
Environment=sandbox
```

The OIDC provider tells AWS that identities issued by GitHub Actions may be evaluated by IAM trust policies.

Creating the provider alone does not give any GitHub repository access to AWS resources.

## IAM role

The GitHub Actions IAM role is:

```text
zero-to-prod-github-actions
```

ARN:

```text
arn:aws:iam::333534066371:role/zero-to-prod-github-actions
```

The role is assumed through:

```text
sts:AssumeRoleWithWebIdentity
```

At the end of Issue #6, the role intentionally has no attached or inline AWS permission policies.

Verification:

```bash
aws iam list-attached-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

Expected result:

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

Expected result:

```json
{
  "PolicyNames": []
}
```

This allowed authentication to be verified separately from ECR authorization.

## Trust policy

The role trust policy is stored in:

```text
infra/aws/iam/github-actions-trust-policy.json
```

The important trust conditions are:

```json
{
  "StringEquals": {
    "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
    "token.actions.githubusercontent.com:sub": "repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main"
  }
}
```

The trust policy answers:

> Who is allowed to become this IAM role?

It does not define what the role can do afterward.

### Audience condition

The audience condition is:

```text
token.actions.githubusercontent.com:aud = sts.amazonaws.com
```

This verifies that the GitHub token was created for use with AWS STS.

Conceptually:

```text
aud
 ↓
Who is this token intended for?
 ↓
AWS STS
```

### Subject condition

The subject condition is:

```text
repo:ZakariaAitAli/zero-to-prod:ref:refs/heads/main
```

This restricts role assumption to the expected repository and `main` branch.

Conceptually:

```text
sub
 ↓
Which GitHub workload identity is this?
 ↓
ZakariaAitAli/zero-to-prod
main branch
```

Using an exact `StringEquals` condition prevents unrelated repositories or branches from assuming the role.

## GitHub Actions configuration

The existing CI workflow is:

```text
.github/workflows/demo-api-ci.yml
```

AWS authentication is performed in a dedicated job:

```text
verify-aws-oidc
```

The job depends on the existing test and build job:

```yaml
needs: test-and-build
```

AWS authentication is therefore not attempted when CI fails.

The job runs only for a push to `main`:

```yaml
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
```

OIDC token permission is scoped only to this job:

```yaml
permissions:
  id-token: write
```

The test and Docker build job does not receive OIDC token permissions.

AWS credentials are configured using the official AWS GitHub Action.

The action is pinned to an immutable commit SHA:

```yaml
uses: aws-actions/configure-aws-credentials@e6de054238d6b7531b4efff3b6587d9aade6a06c # v6.2.3
```

The workflow assumes:

```text
arn:aws:iam::333534066371:role/zero-to-prod-github-actions
```

in:

```text
eu-west-3
```

After authentication, the workflow verifies the resulting identity with:

```bash
aws sts get-caller-identity
```

## Authentication vs authorization

One of the main lessons from this issue was the difference between IAM trust and IAM permissions.

A role trust policy answers:

```text
Who may become this role?
```

A permission policy answers:

```text
What may this role do?
```

The relationship is:

```text
GitHub identity
      ↓
trust policy
      ↓
may assume IAM role
      ↓
temporary credentials
      ↓
permission policy
      ↓
allowed AWS API calls
```

This means a workflow may successfully authenticate to AWS while still being unable to use ECR, ECS, S3, or other AWS services.

This separation makes it possible to test authentication independently before introducing broader AWS permissions.

## Successful verification

PR #17 was merged into `main`.

The resulting `main` workflow successfully authenticated to AWS using OIDC.

The credentials action reported:

```text
Assuming role with OIDC

Authenticated as assumedRoleId
AROAU3KBN63B5DZD3NMLM:GitHubActions
```

The workflow then ran:

```bash
aws sts get-caller-identity
```

Result:

```json
{
  "UserId": "AROAU3KBN63B5DZD3NMLM:GitHubActions",
  "Account": "333534066371",
  "Arn": "arn:aws:sts::333534066371:assumed-role/zero-to-prod-github-actions/GitHubActions"
}
```

This proves:

```text
GitHub Actions
      ↓
GitHub OIDC
      ↓
AWS STS
      ↓
zero-to-prod-github-actions
      ↓
temporary AWS credentials
```

No permanent AWS access key or secret access key was required.

## Failure experiment

A deliberate failure experiment was performed to verify that the IAM trust policy actually rejects an unexpected GitHub identity.

Normally, the AWS job runs only on:

```yaml
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
```

For the experiment, the workflow condition was temporarily changed to:

```yaml
if: github.event_name == 'pull_request'
```

The AWS trust policy was not changed.

This allowed a pull-request workflow to attempt to assume the role.

Expected behavior:

```text
pull-request identity
       ↓
does not match main-branch subject
       ↓
AWS STS rejects role assumption
```

Actual result:

```text
Assuming role with OIDC

Could not assume role with OIDC:
Not authorized to perform sts:AssumeRoleWithWebIdentity
```

The credentials action retried the failed role assumption 12 times before giving up.

The experiment therefore demonstrated:

```text
pull_request identity → rejected ❌
main branch identity  → accepted ✅
```

The temporary workflow change was then reverted and the `main`-only condition restored.

## Security decisions

### No permanent AWS credentials

No permanent:

```text
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
```

is stored in GitHub for this workflow.

AWS credentials are obtained dynamically through STS.

### Repository and branch restriction

The IAM trust relationship is restricted to:

```text
Repository: ZakariaAitAli/zero-to-prod
Branch: main
```

A pull-request identity was deliberately tested and rejected.

### Least privilege for GitHub permissions

OIDC permission is not granted at workflow scope.

Only the AWS authentication job receives:

```yaml
permissions:
  id-token: write
```

### Authentication before authorization

The IAM role initially has no ECR or ECS permission policy.

This keeps the authentication problem separate from AWS resource authorization.

### Immutable GitHub Action reference

The AWS credentials GitHub Action is pinned to an exact commit SHA rather than only a mutable version reference.

This reduces supply-chain risk by ensuring the workflow executes the intended action revision.

## Cost considerations

The IAM role and GitHub OIDC provider do not create continuously running AWS workload resources.

No ECS tasks, load balancers, databases, or other runtime infrastructure were created during this issue.

AWS-side runtime cost for the OIDC authentication foundation is therefore effectively:

```text
$0
```

The resources should remain because they are required by later Sprint 01 issues.

## Verification

### Verify AWS account

```bash
aws sts get-caller-identity \
  --profile sandbox
```

Expected account:

```text
333534066371
```

### Verify OIDC provider

```bash
aws iam list-open-id-connect-providers \
  --profile sandbox
```

Expected provider:

```text
arn:aws:iam::333534066371:oidc-provider/token.actions.githubusercontent.com
```

### Inspect OIDC provider

```bash
aws iam get-open-id-connect-provider \
  --open-id-connect-provider-arn \
    arn:aws:iam::333534066371:oidc-provider/token.actions.githubusercontent.com \
  --profile sandbox
```

### Inspect IAM role

```bash
aws iam get-role \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

### Verify attached permission policies

```bash
aws iam list-attached-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

### Verify inline permission policies

```bash
aws iam list-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

### Validate trust-policy JSON locally

```bash
jq . infra/aws/iam/github-actions-trust-policy.json
```

### Validate GitHub Actions workflow

```bash
actionlint .github/workflows/demo-api-ci.yml
```

## Cleanup procedure

The OIDC provider and IAM role should remain while Sprint 01 uses GitHub-to-AWS authentication.

If the Zero-to-Prod AWS integration is retired, clean up the IAM role before deleting the OIDC provider.

### Inspect role policies

First verify what policies are attached:

```bash
aws iam list-attached-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox

aws iam list-role-policies \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

Later Sprint 01 issues may add policies to this role. Those policies must be detached or deleted before the role can be removed.

### Delete the IAM role

When no permission policies remain:

```bash
aws iam delete-role \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

### Delete the GitHub OIDC provider

```bash
aws iam delete-open-id-connect-provider \
  --open-id-connect-provider-arn \
    arn:aws:iam::333534066371:oidc-provider/token.actions.githubusercontent.com \
  --profile sandbox
```

### Verify cleanup

```bash
aws iam list-open-id-connect-providers \
  --profile sandbox
```

and:

```bash
aws iam get-role \
  --role-name zero-to-prod-github-actions \
  --profile sandbox
```

After deletion, `get-role` should return a `NoSuchEntity` error.

## Troubleshooting and lessons learned

### `id-token: write` does not mean AWS access

The permission:

```yaml
id-token: write
```

only allows GitHub Actions to request an OIDC identity token.

AWS still independently evaluates the IAM role trust policy.

### Workflow conditions and IAM trust policies are separate controls

The GitHub workflow contains:

```text
Should this AWS authentication job run?
```

The AWS trust policy contains:

```text
If authentication is attempted, should AWS trust this identity?
```

The failure experiment proved that bypassing the first control does not bypass the second.

### Successful authentication does not imply service permissions

`aws sts get-caller-identity` can succeed even when the role has no ECR permission.

Authentication and authorization should therefore be tested separately.

### Validate workflows locally

`actionlint` was installed and used to detect GitHub Actions-specific configuration problems before pushing changes.

```bash
actionlint .github/workflows/demo-api-ci.yml
```

### Failed role assumptions may retry

The AWS credentials action retried the deliberate failed role assumption 12 times.

A trust-policy failure therefore took significantly longer than a single failed STS request.

## Result

Issue #6 established secure workload identity between GitHub Actions and the AWS sandbox.

The verified state is:

```text
GitHub commit
      ↓
GitHub Actions
      ↓
tests + Docker build
      ↓
GitHub OIDC
      ↓
AWS STS
      ↓
temporary credentials
      ↓
zero-to-prod-github-actions
```

The next capability is to give this role the minimum authorization required to push immutable commit-SHA images to the existing Amazon ECR repository.
