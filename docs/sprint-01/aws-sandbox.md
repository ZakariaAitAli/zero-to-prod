# Sprint 01 — AWS Sandbox Foundation

## Purpose

Sprint 01 uses a dedicated AWS sandbox account for all workload resources.

The original AWS account is used as the AWS Organizations management account. Application and infrastructure resources for Zero-to-Prod are created only in the sandbox member account.

## AWS organization

```text
AWS Organization
│
├── Management account
│   ├── Name: ingenieur.devops
│   └── Account ID: 277009666960
│
└── Member account
    ├── Name: sandbox
    └── Account ID: 333534066371
```

The management account is reserved for organization-level responsibilities such as:

* AWS Organizations
* IAM Identity Center
* Consolidated billing
* Organization administration

Zero-to-Prod workload resources are created in the `sandbox` member account.

## Region

Sprint 01 uses a single AWS region:

```text
eu-west-3
Europe (Paris)
```

All regional resources created during Sprint 01 must explicitly target `eu-west-3`.

## Human access

Human access is managed using AWS IAM Identity Center.

Identity:

```text
User: zakaria
Group: Admins
Permission set: AdministratorAccess
Target AWS account: sandbox
```

Access path:

```text
zakaria
   ↓
Admins
   ↓
AdministratorAccess
   ↓
sandbox
333534066371
```

The management account is not used for normal Zero-to-Prod engineering work.

## CLI authentication

The AWS CLI uses IAM Identity Center rather than permanent IAM access keys.

Profile:

```text
sandbox
```

Authentication:

```bash
aws sso login --profile sandbox
```

Identity verification:

```bash
aws sts get-caller-identity \
  --profile sandbox
```

Expected account:

```text
333534066371
```

Expected identity type:

```text
assumed-role/AWSReservedSSO_AdministratorAccess_.../zakaria
```

This means the CLI is using temporary credentials obtained through IAM Identity Center.

## Account safety guard

Before creating AWS resources, the active account is explicitly verified.

```bash
export AWS_PROFILE=sandbox
export AWS_REGION=eu-west-3
export EXPECTED_ACCOUNT_ID=333534066371

ACCOUNT_ID=$(
  aws sts get-caller-identity \
    --profile "$AWS_PROFILE" \
    --query Account \
    --output text
)

if [ "$ACCOUNT_ID" != "$EXPECTED_ACCOUNT_ID" ]; then
  echo "ERROR: refusing to operate in unexpected AWS account"
  echo "Expected: $EXPECTED_ACCOUNT_ID"
  echo "Current:  $ACCOUNT_ID"
  exit 1
fi
```

This guard exists to prevent accidental resource creation in the management account or another AWS account.

## Cost guardrail

A monthly AWS Budget is configured before creating workload infrastructure.

```text
Budget name: zero-to-prod-monthly
Budget amount: 20 USD
Period: Monthly
Budget type: Cost
```

Actual-spend notifications:

```text
50%  → 10 USD
80%  → 16 USD
100% → 20 USD
```

Notifications are delivered by email.

The budget is an alerting mechanism and not a hard spending limit.

## Amazon ECR

Repository:

```text
zero-to-prod-demo-api
```

Configuration:

```text
AWS account: 333534066371
Region: eu-west-3
Tag mutability: IMMUTABLE
Encryption: AES256
Scan on push: false
```

Repository URI:

```text
333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api
```

Repository ARN:

```text
arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api
```

The repository uses immutable image tags.

Future CI/CD workflows will use Git commit SHAs as image tags:

```text
zero-to-prod-demo-api:<git-commit-sha>
```

Example:

```text
zero-to-prod-demo-api:755432c9cb4ad0d109a6cbfda0437f71ace8baab
```

This provides a direct relationship between:

```text
source commit
     ↓
container image
     ↓
deployment version
```

## Resource tags

The ECR repository is tagged with:

```text
Project=zero-to-prod
Environment=sandbox
Owner=ZakariaAitAli
```

Verify tags:

```bash
ECR_ARN="arn:aws:ecr:eu-west-3:333534066371:repository/zero-to-prod-demo-api"

aws ecr list-tags-for-resource \
  --profile sandbox \
  --region eu-west-3 \
  --resource-arn "$ECR_ARN"
```

## Verification

### Verify AWS identity

```bash
aws sts get-caller-identity \
  --profile sandbox
```

### Verify budget

```bash
aws budgets describe-budget \
  --profile sandbox \
  --account-id 333534066371 \
  --budget-name zero-to-prod-monthly
```

### Verify budget notifications

```bash
aws budgets describe-notifications-for-budget \
  --profile sandbox \
  --account-id 333534066371 \
  --budget-name zero-to-prod-monthly
```

### Verify ECR

```bash
aws ecr describe-repositories \
  --profile sandbox \
  --region eu-west-3 \
  --repository-names zero-to-prod-demo-api
```

## Cleanup procedure

### Inspect ECR images

Before deleting the repository:

```bash
aws ecr list-images \
  --profile sandbox \
  --region eu-west-3 \
  --repository-name zero-to-prod-demo-api
```

### Delete ECR repository

```bash
aws ecr delete-repository \
  --profile sandbox \
  --region eu-west-3 \
  --repository-name zero-to-prod-demo-api \
  --force
```

`--force` also deletes images stored in the repository.

### Verify ECR deletion

```bash
aws ecr describe-repositories \
  --profile sandbox \
  --region eu-west-3 \
  --repository-names zero-to-prod-demo-api
```

After successful cleanup, a `RepositoryNotFoundException` is expected.

### Budget cleanup

The budget should remain active while the sandbox account is in use.

When the sandbox project is retired:

```bash
aws budgets delete-budget \
  --profile sandbox \
  --account-id 333534066371 \
  --budget-name zero-to-prod-monthly
```

## Operational rule

Every future Sprint 01 AWS resource must have:

1. An explicitly selected AWS account.
2. An explicitly selected AWS region.
3. Appropriate resource tags.
4. A cost consideration.
5. A documented cleanup procedure.

A resource is not considered fully provisioned until its deletion path is understood.

## Scope note

This issue was originally expected to focus primarily on AWS Budgets and Amazon ECR.

The actual task expanded because the AWS environment initially consisted only of a root account and the development machine had no configured AWS identity.

Before creating workload resources, the AWS foundation was therefore established correctly:

* Root access was secured.
* AWS Organizations was enabled.
* IAM Identity Center was enabled in `eu-west-3`.
* A dedicated `sandbox` member account was created.
* A human Identity Center user was created.
* An `Admins` group was created.
* An `AdministratorAccess` permission set was created.
* The group was assigned to the sandbox account.
* AWS CLI SSO authentication was configured.
* Temporary STS credentials were verified.
* An explicit account guard was tested.
* AWS Budget notifications were configured.
* The ECR repository was created.

This additional work establishes the security, account-isolation, identity, and cost foundation used by the remaining Sprint 01 tasks.
