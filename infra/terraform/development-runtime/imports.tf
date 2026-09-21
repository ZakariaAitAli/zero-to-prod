import {
  to = aws_ecs_cluster.development
  id = "zero-to-prod-dev"
}

import {
  to = aws_security_group.verification_alb
  id = "sg-074c56242e9680fb8"
}

import {
  to = aws_security_group.demo_api
  id = "sg-066f3abf671c5677b"
}

import {
  to = aws_vpc_security_group_ingress_rule.verification_alb_http
  id = "sgr-007acc1175781bfd5"
}

import {
  to = aws_vpc_security_group_egress_rule.verification_alb_to_demo_api
  id = "sgr-091d827c199a02060"
}

import {
  to = aws_vpc_security_group_ingress_rule.demo_api_from_verification_alb
  id = "sgr-033b7d09d4f78d3c4"
}

import {
  to = aws_vpc_security_group_egress_rule.demo_api_all
  id = "sgr-01a72eb49539a1d4f"
}

import {
  to = aws_lb_target_group.demo_api
  id = "arn:aws:elasticloadbalancing:eu-west-3:333534066371:targetgroup/zero-to-prod-dev-demo-api/e13da237e78ebb05"
}

import {
  to = aws_iam_role.ecs_task_execution
  id = "zero-to-prod-ecs-task-execution"
}

import {
  to = aws_iam_role_policy.ecs_task_execution_ecr_pull
  id = "zero-to-prod-ecs-task-execution:zero-to-prod-ecr-pull"
}

import {
  to = aws_iam_role_policy.ecs_task_execution_cloudwatch_logs
  id = "zero-to-prod-ecs-task-execution:zero-to-prod-cloudwatch-logs"
}

import {
  to = aws_cloudwatch_log_group.demo_api
  id = "/zero-to-prod/development/demo-api"
}

import {
  to = aws_ecs_service.demo_api
  id = "zero-to-prod-dev/demo-api"
}
