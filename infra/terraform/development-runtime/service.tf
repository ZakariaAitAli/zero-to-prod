data "aws_ecs_task_definition" "demo_api" {
  task_definition = "zero-to-prod-demo-api"
}

resource "aws_ecs_service" "demo_api" {
  name    = "demo-api"
  cluster = aws_ecs_cluster.development.arn

  task_definition = data.aws_ecs_task_definition.demo_api.arn
  desired_count   = 0

  launch_type      = "FARGATE"
  platform_version = "LATEST"

  scheduling_strategy           = "REPLICA"
  availability_zone_rebalancing = "ENABLED"

  deployment_maximum_percent         = 200
  deployment_minimum_healthy_percent = 100

  deployment_circuit_breaker {
    enable   = false
    rollback = false
  }

  deployment_controller {
    type = "ECS"
  }

  health_check_grace_period_seconds = 30

  network_configuration {
    subnets          = local.runtime_subnet_ids
    security_groups  = [aws_security_group.demo_api.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.demo_api.arn
    container_name   = "demo-api"
    container_port   = 8080
  }

  enable_ecs_managed_tags = true
  enable_execute_command  = false
  propagate_tags          = "SERVICE"

  tags = {
    Project     = "zero-to-prod"
    Environment = "development"
  }

  lifecycle {
    ignore_changes = [
      task_definition,
      desired_count,
    ]
  }
}
