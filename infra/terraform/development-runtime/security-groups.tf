resource "aws_security_group" "verification_alb" {
  name        = "zero-to-prod-dev-alb"
  description = "ALB security group for zero-to-prod development"
  vpc_id      = data.aws_vpc.default.id

  tags = {
    Name        = "zero-to-prod-dev-alb"
    Project     = "zero-to-prod"
    Environment = "development"
  }
}

resource "aws_security_group" "demo_api" {
  name        = "zero-to-prod-dev-demo-api"
  description = "Fargate task security group for zero-to-prod demo API"
  vpc_id      = data.aws_vpc.default.id

  tags = {
    Name        = "zero-to-prod-dev-demo-api"
    Project     = "zero-to-prod"
    Environment = "development"
  }
}
