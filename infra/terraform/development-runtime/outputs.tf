output "ecs_cluster_arn" {
  description = "ARN of the development ECS cluster."
  value       = aws_ecs_cluster.development.arn
}

output "ecs_service_arn" {
  description = "ARN of the demo API ECS service."
  value       = aws_ecs_service.demo_api.arn
}

output "demo_api_target_group_arn" {
  description = "ARN of the long-lived demo API target group."
  value       = aws_lb_target_group.demo_api.arn
}

output "verification_alb_security_group_id" {
  description = "Security group ID used by the temporary verification ALB."
  value       = aws_security_group.verification_alb.id
}

output "demo_api_security_group_id" {
  description = "Security group ID used by demo API Fargate tasks."
  value       = aws_security_group.demo_api.id
}

output "ecs_task_execution_role_arn" {
  description = "ARN of the demo API ECS task execution role."
  value       = aws_iam_role.ecs_task_execution.arn
}

output "demo_api_log_group_name" {
  description = "CloudWatch Logs group used by the demo API."
  value       = aws_cloudwatch_log_group.demo_api.name
}
