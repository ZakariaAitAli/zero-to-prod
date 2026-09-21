# Sprint 02 — Own Development Runtime Infrastructure with Terraform

## Purpose

Issue #88 establishes Terraform ownership of the long-lived AWS runtime used by the development demo API.

Before this issue, the deployment workflow could safely deploy and verify application revisions, but several resources required by that workflow existed only as manually created AWS infrastructure.

Those resources included:

    ECS cluster
    ECS service
    target group
    ALB security group
    task security group
    security-group rules
    ECS task execution role
    execution-role inline policies
    CloudWatch log group

The deployment path therefore depended on infrastructure that could be inspected in AWS but could not be reconstructed or audited from Terraform alone.

The target capability was:

    adopt the existing long-lived development runtime into Terraform
    without recreating working resources
    and without weakening deployment, rollback, or verification behavior

This work applies only to the development sandbox.

It does not establish production readiness.

---

## Ownership boundary

A dedicated Terraform root now owns the project-specific long-lived development runtime:

    infra/terraform/development-runtime

Its authoritative remote-state key is:

    development-runtime/terraform.tfstate

The state uses the existing versioned Terraform-state bucket:

    zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate

Region:

    eu-west-3

The runtime state owns:

    ECS cluster
    ECS service structural configuration
    target group
    ALB security group
    task security group
    security-group rules
    ECS task execution role
    execution-role ECR pull policy
    execution-role CloudWatch Logs policy
    CloudWatch log group

The following remain external dependencies:

    default VPC
    default public subnets
    default Internet Gateway
    main route table
    ECR repository
    GitHub Actions OIDC provider
    GitHub Actions IAM role
    ECS service-linked role
    deployment-record storage
    normal ECS task-definition revisions

The temporary verification ALB and listener remain owned by:

    infra/terraform/development-verification

This keeps long-lived runtime ownership separate from temporary verification infrastructure.

---

## ECS service ownership contract

Terraform owns the service's structural configuration:

    cluster
    Fargate launch configuration
    network configuration
    security groups
    subnets
    public-IP assignment
    target-group attachment
    container name and port
    health-check grace period
    deployment configuration
    service tags
    tag propagation

The deployment workflow continues to own:

    task_definition
    desired_count

The Terraform resource therefore explicitly ignores changes to those two attributes.

This prevents Terraform from reverting a task-definition revision registered by CI or interfering with temporary scale-up and scale-down during deployment verification.

Normal task-definition revision registration remains outside Terraform.

---

## Networking boundary

The sandbox continues to use the low-cost networking model:

    default VPC
    public default subnets
    assign_public_ip = true
    Internet Gateway
    no NAT Gateway

Terraform discovers the default VPC and the required default subnets through data sources.

The default networking resources are not imported into the runtime state.

---

## Adoption strategy

The existing AWS resources were adopted rather than recreated.

Exact import blocks were created for 13 long-lived resources.

Before any state mutation, an import-aware Terraform plan showed:

    13 to import
    0 to add
    0 to change
    0 to destroy

The reviewed plan was then applied.

Result:

    13 imported
    0 added
    0 changed
    0 destroyed

No existing runtime resource was replaced or recreated to establish Terraform ownership.

After adoption, a fresh plan reported:

    No changes. Your infrastructure matches the configuration.

This established the zero-change ownership baseline required before any deliberate runtime behavior change.

---

## Target-group readiness change

Application endpoint semantics are:

    /health
        process liveness

    /ready
        lifecycle-aware readiness

    /version
        exact build Git SHA

The ECS container health check remains on:

    /health

After Terraform adoption reached a stable zero-change plan, the load-balancer target-group health check was deliberately changed from:

    /health

to:

    /ready

The reviewed Terraform plan showed:

    0 to add
    1 to change
    0 to destroy

The target group was updated in place.

After the apply:

    HealthCheckPath = /ready
    LoadBalancerArns = []

The ECS service remained:

    desired = 0
    running = 0
    pending = 0

A fresh runtime Terraform plan again reported no changes.

---

## Verification-stack wiring

The temporary verification Terraform stack no longer discovers the long-lived target group and ALB security group by AWS name.

Instead, it reads explicit outputs from the runtime Terraform state.

Runtime outputs used by the verification stack:

    verification_alb_security_group_id
    demo_api_target_group_arn

The dependency is now:

    development-runtime state
        ↓
    development-verification remote-state data source
        ↓
    temporary ALB and listener

A read-only verification Terraform plan resolved the expected existing runtime identifiers and proposed only:

    aws_lb.verification
    aws_lb_listener.http

Plan result:

    2 to add
    0 to change
    0 to destroy

The verification stack therefore continues to own only temporary infrastructure.

---

## Runtime-state IAM boundary

The GitHub Actions role requires read access to the long-lived runtime state so the verification stack can consume its outputs.

The role was granted only:

    s3:ListBucket

for the exact runtime-state prefix, and:

    s3:GetObject

for:

    development-runtime/terraform.tfstate

Policy simulation confirmed:

    s3:GetObject     allowed
    s3:PutObject     implicitDeny
    s3:DeleteObject  implicitDeny

AWS Access Analyzer reported no findings.

The deployment workflow therefore cannot silently overwrite or delete the long-lived runtime Terraform state.

---

## CI validation

The existing Terraform validation job was extended to include:

    infra/terraform/development-runtime

CI now validates these Terraform roots:

    infra/terraform/bootstrap/development-verification-state
    infra/terraform/bootstrap/deployment-records
    infra/terraform/development-runtime
    infra/terraform/development-verification

Pull request #89 passed all required CI checks before merge.

The merge commit was:

    92238484a8f896820045b3960ab3c6550462252c

The post-merge CI run completed successfully without deploying because the runtime ownership changes did not classify as application deployment changes.

After that run, the service remained at desired count zero and no temporary ALB existed.

---

## Deployment experiment

A separate non-behavioral application change was used to exercise the existing main-only deployment path without weakening its deployment policy.

The experiment was merged through pull request #90.

Experiment merge SHA:

    2663a9d82dc4cba887472c6c6f6240c332253ab3

GitHub Actions run:

    35662470265

The workflow successfully:

    built the demo API image
    published the immutable image to ECR
    read the development runtime remote state
    planned temporary verification infrastructure
    created the verification ALB and listener
    registered ECS task definition revision 29
    scaled the ECS service to 1
    waited for service stability
    verified the deployment externally
    wrote the verified deployment record
    scaled the ECS service back to 0
    destroyed the temporary ALB and listener

The runtime configuration digest remained:

    sha256:58ba6680fbf90644e5325f3cb432db33898d06b3f4115df908cb7b49c9579029

This confirms the Terraform ownership work did not alter or bypass the existing runtime configuration identity mechanism.

---

## External verification evidence

The deployment workflow verified the application through the temporary ALB.

Health result:

    observed: healthy
    result: passed

Readiness result:

    observed: ready
    result: passed

Expected version:

    2663a9d82dc4cba887472c6c6f6240c332253ab3

Observed version:

    2663a9d82dc4cba887472c6c6f6240c332253ab3

Version verification:

    passed

Registered and deployed task definition:

    arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:29

Verified image URI:

    333534066371.dkr.ecr.eu-west-3.amazonaws.com/zero-to-prod-demo-api:2663a9d82dc4cba887472c6c6f6240c332253ab3

Verified image digest:

    sha256:2e78e7d1495069198b4b715838b36e2af7de2feaa08bb43f4901cb706b92026c

---

## Verified deployment record

The successful experiment produced:

    s3://zero-to-prod-333534066371-eu-west-3-deployment-records/development/2663a9d82dc4cba887472c6c6f6240c332253ab3.json

The retrieved record contains:

    schema_version = 2
    verification_status = verified
    environment = development
    git_sha = 2663a9d82dc4cba887472c6c6f6240c332253ab3
    image_digest = sha256:2e78e7d1495069198b4b715838b36e2af7de2feaa08bb43f4901cb706b92026c
    runtime_config_digest = sha256:58ba6680fbf90644e5325f3cb432db33898d06b3f4115df908cb7b49c9579029
    task_definition_arn = arn:aws:ecs:eu-west-3:333534066371:task-definition/zero-to-prod-demo-api:29
    workflow_run_id = 35662470265
    health.status = healthy
    version.matches = true

The stored image digest was independently compared with ECR and matched.

The complete record identity assertion returned:

    true

The S3 object is versioned.

Version ID observed for this experiment:

    NMUtwhEm45FjTP.tCQ6r5leRpGYIiygg

---

## Cleanup evidence

After verification, the workflow scaled the service back to zero.

Observed final ECS state:

    desired = 0
    running = 0
    pending = 0

The long-lived ECS service retained task-definition revision:

    zero-to-prod-demo-api:29

The temporary verification infrastructure was destroyed successfully:

    2 resources destroyed

The target group remained long-lived and Terraform-owned:

    HealthCheckPath = /ready
    LoadBalancerArns = []

The verification Terraform state was empty after cleanup.

A fresh development-runtime Terraform plan reported:

    No changes. Your infrastructure matches the configuration.

This confirms the deployment workflow can mutate its intentionally owned service attributes without causing Terraform drift.

---

## Cost posture

The final development posture preserves the low-cost sandbox model:

    ECS desired count = 0
    running Fargate tasks = 0
    temporary ALB = destroyed
    temporary listener = destroyed
    NAT Gateway = none
    always-on load balancer = none

The only new persistent component is the small S3 runtime state object in the existing Terraform-state bucket.

---

## Result

Issue #88 established a reproducible long-lived Terraform ownership boundary for the development runtime while preserving the existing deployment safety model.

The resulting ownership model is:

    Terraform
        long-lived runtime structure

    deployment workflow
        task-definition revisions
        temporary desired-count changes

    verification Terraform
        temporary ALB and listener

    AWS default networking
        external dependency

The adoption did not recreate existing runtime resources.

The later `/ready` target-group change was performed only after stable Terraform ownership had been demonstrated.

The successful deployment experiment proved that the existing workflow still:

    verifies /health
    verifies /ready
    verifies exact /version
    preserves runtime_config_digest
    writes an immutable verified deployment record
    returns ECS to desired count 0
    destroys temporary verification infrastructure

Historical Sprint 02 evidence was not rewritten.
