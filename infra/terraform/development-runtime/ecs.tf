resource "aws_ecs_cluster" "development" {
  name = "zero-to-prod-dev"

  setting {
    name  = "containerInsights"
    value = "disabled"
  }

  tags = {
    Project     = "zero-to-prod"
    Environment = "development"
  }
}
