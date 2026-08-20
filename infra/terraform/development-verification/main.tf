data "aws_lb_target_group" "demo_api" {
  name = "zero-to-prod-dev-demo-api"
}

data "aws_security_group" "alb" {
  name   = "zero-to-prod-dev-alb"
  vpc_id = "vpc-0de74a3a8146ba655"
}

data "aws_subnet" "alb_a" {
  id = "subnet-0f331eaaf21305f08"
}

data "aws_subnet" "alb_b" {
  id = "subnet-0dbb5fe68184ddba0"
}

resource "aws_lb" "verification" {
  name               = "zero-to-prod-dev-alb"
  internal           = false
  load_balancer_type = "application"

  security_groups = [
    data.aws_security_group.alb.id,
  ]

  subnets = [
    data.aws_subnet.alb_a.id,
    data.aws_subnet.alb_b.id,
  ]
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.verification.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = data.aws_lb_target_group.demo_api.arn
  }
}
