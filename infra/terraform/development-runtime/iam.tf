resource "aws_iam_role" "ecs_task_execution" {
  name = "zero-to-prod-ecs-task-execution"

  assume_role_policy = file("${path.module}/../../aws/iam/ecs-task-execution-trust-policy.json")

  tags = {
    Project     = "zero-to-prod"
    Environment = "development"
  }
}

resource "aws_iam_role_policy" "ecs_task_execution_ecr_pull" {
  name = "zero-to-prod-ecr-pull"
  role = aws_iam_role.ecs_task_execution.id

  policy = file("${path.module}/../../aws/iam/ecs-task-execution-ecr-pull-policy.json")
}

resource "aws_iam_role_policy" "ecs_task_execution_cloudwatch_logs" {
  name = "zero-to-prod-cloudwatch-logs"
  role = aws_iam_role.ecs_task_execution.id

  policy = file("${path.module}/../../aws/iam/ecs-task-execution-cloudwatch-logs-policy.json")
}
