# Terraform Backend Retirement Evidence

Date: 2026-09-26

## Scope

This evidence records the verification performed before retiring the
pre-rebaseline AWS Terraform state backend for Zero-to-Prod.

AWS account:

- Account ID: `333534066371`
- Region: `eu-west-3`
- CLI profile used for verification: `sandbox`

Backend bucket:

`zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate`

No raw Terraform state files are preserved in Git.

## Current Remote State

Two current Terraform state objects existed in the backend.

### development-runtime

State key:

`development-runtime/terraform.tfstate`

Final inspected state:

- Terraform state version: `4`
- Terraform version: `1.15.9`
- Serial: `4`
- Lineage: `47da4fba-a93b-e39a-bc4e-d7227f4102d1`
- Managed/data resources recorded: `0`

### development-verification

State key:

`development-verification/terraform.tfstate`

Final inspected state:

- Terraform state version: `4`
- Terraform version: `1.15.9`
- Serial: `47`
- Lineage: `71e55630-6ebf-c0b8-9bb1-50500e8816c4`
- Managed/data resources recorded: `0`

Both authoritative current states were empty before backend retirement.

## Last Non-Empty Runtime State

The last inspected non-empty `development-runtime` state contained these
managed resources:

- `aws_ecs_cluster.development`
- `aws_lb_target_group.demo_api`
- `aws_security_group.demo_api`

Identifiers:

- ECS cluster:
  `arn:aws:ecs:eu-west-3:333534066371:cluster/zero-to-prod-dev`
- Target group:
  `arn:aws:elasticloadbalancing:eu-west-3:333534066371:targetgroup/zero-to-prod-dev-demo-api/e13da237e78ebb05`
- Security group:
  `sg-066f3abf671c5677b`

The state also contained data sources for the ECS task definition, runtime
subnets, and default VPC. These were lookups rather than Terraform-managed
resources and were not treated as retirement targets.

## Last Non-Empty Verification State

The last inspected non-empty `development-verification` state contained:

- `aws_lb.verification`
- `aws_lb_listener.http`

Identifiers:

- Verification ALB:
  `arn:aws:elasticloadbalancing:eu-west-3:333534066371:loadbalancer/app/zero-to-prod-dev-alb/22d8340462e618a4`
- HTTP listener:
  `arn:aws:elasticloadbalancing:eu-west-3:333534066371:listener/app/zero-to-prod-dev-alb/22d8340462e618a4/58c34f162933552c`

The state also contained subnet data sources and a
`terraform_remote_state.development_runtime` data source. These were inputs,
not resources owned by the verification configuration.

## Direct AWS Absence Verification

After confirming both current Terraform states contained zero resources, the
last known managed resource identifiers were queried directly against AWS.

Results:

- ECS cluster: absent
- Runtime target group: absent
- Runtime security group: absent
- Verification ALB: absent
- Verification HTTP listener: absent

This independently confirmed that the empty Terraform states corresponded to
the absence of the resources previously managed by those states.

## Backend Bootstrap State

The backend bucket itself remained managed by the local bootstrap state under:

`infra/terraform/bootstrap/development-verification-state`

Bootstrap state metadata before retirement:

- Terraform state version: `4`
- Terraform version: `1.15.9`
- Serial: `5`
- Lineage: `258af765-d330-9ee4-238c-448ca5889d4b`

Managed resources:

- `aws_s3_bucket.terraform_state`
- `aws_s3_bucket_public_access_block.terraform_state`
- `aws_s3_bucket_server_side_encryption_configuration.terraform_state`
- `aws_s3_bucket_versioning.terraform_state`

The S3 bucket was configured with:

`force_destroy = false`

This intentionally prevented Terraform from deleting a non-empty state bucket.

A final `terraform init`, `terraform validate`, and refresh-enabled
`terraform plan` were run against the bootstrap configuration.

Result:

`No changes. Your infrastructure matches the configuration.`

This confirmed that the bootstrap configuration, local Terraform state, and
actual AWS backend infrastructure agreed immediately before retirement.

## Retirement Decision

The backend became eligible for retirement only after all of the following
were established:

1. The pre-rebaseline AWS runtime and verification delivery paths had been
   retired.
2. The GitHub Actions AWS/OIDC consumer had been removed.
3. The `zero-to-prod-github-actions` IAM role and its permissions had been
   retired.
4. Both current remote Terraform states contained zero resources.
5. The resources from the last non-empty states were independently confirmed
   absent from AWS.
6. The bootstrap state remained intact and matched the real backend
   infrastructure with no drift.

Historical S3 state versions and Terraform lock-object versions are therefore
retirement artifacts rather than active infrastructure state.

Raw state files are deliberately not committed as repository evidence.

## Retirement Result

The versioned backend was inventoried immediately before retirement:

- Object versions: `128`
- Delete markers: `77`
- Total historical entries: `205`
- Logical keys: `4`

The four logical keys were:

- `development-runtime/terraform.tfstate`
- `development-runtime/terraform.tfstate.tflock`
- `development-verification/terraform.tfstate`
- `development-verification/terraform.tfstate.tflock`

All `205` object versions and delete markers were explicitly purged.

Post-purge verification returned:

- Object versions: `0`
- Delete markers: `0`
- Current objects: `0`

A Terraform destroy plan was then generated from the bootstrap state.

The reviewed plan contained exactly four deletions:

- `aws_s3_bucket.terraform_state`
- `aws_s3_bucket_public_access_block.terraform_state`
- `aws_s3_bucket_server_side_encryption_configuration.terraform_state`
- `aws_s3_bucket_versioning.terraform_state`

Plan summary:

`0 to add, 0 to change, 4 to destroy`

The reviewed saved plan was applied successfully.

Result:

`Resources: 0 added, 0 changed, 4 destroyed.`

The final local bootstrap state retained the same lineage and contained zero
resources:

- Serial: `10`
- Lineage: `258af765-d330-9ee4-238c-448ca5889d4b`
- Resource count: `0`

Direct AWS verification after the Terraform destroy confirmed that the backend
bucket no longer exists.

A subsequent account-level bucket listing found no remaining S3 buckets whose
names contain `zero-to-prod`.

The pre-rebaseline Terraform backend is therefore retired.
