# Sprint 01 — Target Capability

## Objective

Build a complete development deployment path that allows a containerized service to be deployed from GitHub Actions to AWS without using permanent AWS credentials.

At the end of this sprint, a commit should be transformed into an immutable container image, deployed to an AWS development environment, automatically verified, and capable of being rolled back.

## Capability statement

Given a tested commit in the `zero-to-prod` repository, I can use GitHub Actions to:

1. Build a Docker image.
2. Tag the image with the Git commit SHA.
3. Authenticate to AWS using GitHub OIDC.
4. Push the image to Amazon ECR.
5. Deploy the image to Amazon ECS Fargate.
6. Wait for the ECS service to become stable.
7. Verify the deployed application through its health and version endpoints.
8. Roll back to a previously known-good version when necessary.

The complete process must run without building or deploying from my laptop.

## Demo application

The deployment target will be a small Go HTTP API.

It will expose:

* `GET /health` — confirms that the process is running.
* `GET /ready` — confirms that the application is ready to receive traffic.
* `GET /version` — returns the deployed application version or Git commit SHA.

The application will not require a database, message queue, cache, or external API.

## AWS development environment

| Component                  | Decision                                             |
| -------------------------- | ---------------------------------------------------- |
| AWS account                | Personal sandbox account                             |
| AWS region                 | `eu-west-3`                                          |
| Environment                | Development only                                     |
| Container registry         | Amazon ECR                                           |
| Runtime                    | Amazon ECS Fargate                                   |
| Traffic entry point        | Application Load Balancer                            |
| Authentication from GitHub | GitHub Actions OIDC                                  |
| Deployment artifact        | Docker image tagged with the full Git commit SHA     |
| Deployment verification    | HTTP checks against `/health` and `/version`         |
| Rollback unit              | Previous ECS task definition or known-good image SHA |

Suggested resource names:

* ECR repository: `zero-to-prod-demo-api`
* ECS cluster: `zero-to-prod-dev`
* ECS service: `demo-api`
* GitHub environment: `development`

## Deployment flow

```text
Git commit
    ↓
GitHub Actions
    ↓
Automated tests
    ↓
Docker image build
    ↓
GitHub OIDC authentication
    ↓
Amazon ECR
    ↓
ECS task definition revision
    ↓
ECS Fargate service deployment
    ↓
Service stability check
    ↓
/health and /version verification
```

## Security requirements

* No permanent AWS access key or secret key will be stored in GitHub.
* GitHub will receive temporary AWS credentials through OIDC.
* The AWS IAM trust policy will be restricted to this repository.
* Deployment permissions will be limited to the development environment.
* The workflow will not have access to any company or production AWS account.
* Sensitive values will not be committed to the repository.
* The application container should run as a non-root user where practical.

## Definition of done

Sprint 01 is complete when all the following conditions are satisfied:

* [x] The Go API has automated tests.
* [x] `/health`, `/ready`, and `/version` work locally.
* [x] The application runs successfully as a Docker container.
* [x] Pull requests run tests and validate the Docker build.
* [x] GitHub Actions authenticates to AWS through OIDC.
* [x] No permanent AWS credentials exist in GitHub secrets.
* [ ] Images are pushed to ECR using immutable Git commit SHA tags.
* [ ] GitHub Actions deploys the selected image to ECS Fargate.
* [ ] The workflow waits for the ECS service to stabilize.
* [ ] The workflow verifies `/health`.
* [ ] The workflow verifies that `/version` matches the deployed commit.
* [ ] Failed verification causes the workflow to fail.
* [ ] A previous known-good version can be redeployed.
* [ ] Concurrent deployments to the development environment are prevented.
* [ ] The deployment architecture and operating procedure are documented.
* [ ] AWS resources can be removed after the sprint.
* [ ] I can explain the complete deployment flow without following a tutorial.

## Deliberately excluded

The following items are outside Sprint 01:

* Production deployment
* Company AWS accounts
* Multiple AWS accounts
* Multiple AWS regions
* Kubernetes or Amazon EKS
* Argo CD or GitOps
* Backstage
* Terraform-based provisioning
* Reusable Terraform modules
* Databases
* Redis or other caching systems
* SQS or asynchronous workers
* Custom domains and Route 53
* TLS certificate management
* Blue/green or canary deployment
* Automatic rollback based on CloudWatch alarms
* Full observability platforms
* Distributed tracing
* Prometheus and Grafana
* Advanced vulnerability-management platforms
* Self-hosted GitHub Actions runners

These may become separate learning sprints after the basic deployment capability is demonstrated.

## Constraints

* The sprint has a maximum investment of 20 focused hours.
* Only one development environment will be used.
* New theory must be followed by practical implementation in the same session.
* No more than one or two unfamiliar concepts should be introduced during one practice session.
* AWS resources must be tagged and monitored for cost.
* Unnecessary resources must be stopped or destroyed after testing.

## Success demonstration

The final demonstration will:

1. Make a visible application change.
2. Push the change to GitHub.
3. Show the GitHub Actions workflow running.
4. Show the immutable image in ECR.
5. Show ECS deploying the new task definition.
6. Call `/health` successfully.
7. Call `/version` and confirm the expected commit SHA.
8. Deploy a broken version or cause verification to fail.
9. Restore the previous known-good version.
10. Explain the authentication, artifact, deployment, verification, and rollback flow.

## Current assumptions

* The AWS sandbox account is available.
* The required ECS and IAM permissions can be created.
* A small temporary AWS cost is acceptable within configured budget alerts.
* Manual infrastructure creation is acceptable for Sprint 01.
* Infrastructure as code will be addressed in a later Terraform-focused sprint.
