# Identity and access

Identity and access control answer two different questions:

```text
Authentication
Who are you?

Authorization
What are you allowed to do?
```

Zero-to-Prod made that distinction concrete while building the GitHub-to-AWS delivery path.

## Workload identity instead of stored cloud credentials

The Sprint 01 delivery workflow used GitHub Actions OIDC to assume an AWS IAM role through AWS STS.

The important flow was:

```text
GitHub Actions job
      ↓
OIDC identity token
      ↓
AWS IAM trust policy
      ↓
AWS STS temporary credentials
      ↓
AWS API calls allowed by IAM permissions
```

The workflow did not require a permanent AWS access key and secret key stored in GitHub.

This separates the identity relationship from the permissions granted after authentication.

## Trust is not permission

An IAM role has two relevant sides in this model.

The **trust policy** decides which external identity may assume the role. In the GitHub OIDC path, conditions such as the token audience and subject constrain which GitHub context is trusted.

The role's **permission policies** decide what the successfully authenticated session may do.

A workflow can therefore authenticate successfully and still receive an authorization failure when it attempts an ECR, ECS, IAM, Terraform-related, or other AWS operation.

Zero-to-Prod deliberately encountered both kinds of failure.

## Context can change identity

Sprint 01 exposed an important OIDC detail when a GitHub Environment was added to deployment jobs.

The OIDC subject changed from the branch-oriented identity expected by the existing trust policy to an environment-oriented identity. Role assumption then failed until the trust policy explicitly allowed the intended environment subject.

The lesson is broader than GitHub and AWS:

> Identity is determined by the actual execution context and claims, not by what an operator assumes the context will produce.

## Least privilege is derived from behavior

Least privilege is not simply "use a small policy."

A useful process is:

1. identify the operation the workload must perform;
2. identify the APIs involved in that operation;
3. scope resources where the service supports resource-level permissions;
4. avoid unrelated administrative actions;
5. exercise both successful and denied behavior;
6. refine the policy from observed requirements.

For example, pushing an image to ECR requires several API operations. Successful registry authentication alone does not authorize the complete push.

Zero-to-Prod also kept responsibilities separate where practical: deployment identity, ECS task execution identity, PostgreSQL migration identity, PostgreSQL application identity, RabbitMQ publisher identity, and RabbitMQ worker identity do not all need the same permissions.

## Runtime identity boundaries

The current Work Items lab applies the same principle locally.

PostgreSQL separates:

- a bootstrap administrator;
- a migration identity that owns schema changes;
- an application identity with only the runtime table and sequence privileges the API needs.

RabbitMQ similarly separates publisher and worker responsibilities in the asynchronous-processing lab.

The general rule is:

> Give a component the authority required by its responsibility, rather than giving every component administrative authority because it is convenient.

## Authentication is only the beginning

A secure workload path has several boundaries:

```text
identity
  ↓
trust
  ↓
temporary session
  ↓
permissions
  ↓
resource boundary
  ↓
application/runtime responsibility
```

A failure at each layer means something different. Treating all of them as "IAM problems" makes diagnosis and design harder.

## What to remember

- Authentication and authorization are separate.
- OIDC removes the need for stored long-lived cloud credentials; it does not remove the need for authorization design.
- Trust policies constrain who may obtain a role session.
- Permission policies constrain what that session may do.
- Execution context can change identity claims.
- Least privilege is easier to reason about when responsibilities are separated.
- Successful authentication does not prove successful access to a service.

## Zero-to-Prod deep dives

- [GitHub-to-AWS OIDC](../sprint-01/github-aws-oidc.md)
- [Immutable ECR publication](../sprint-01/ecr-immutable-push.md)
- [ECS Fargate deployment](../sprint-01/ecs-fargate-deploy.md)
- [Sprint 01 security decisions](../sprint-01/security-decisions.md)
- [Local PostgreSQL roles and migrations](../guides/local-postgresql.md)
- [Reliable asynchronous processing](../sprint-03/reliable-async-processing.md)
