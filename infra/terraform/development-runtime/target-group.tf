resource "aws_lb_target_group" "demo_api" {
  name             = "zero-to-prod-dev-demo-api"
  port             = 8080
  protocol         = "HTTP"
  protocol_version = "HTTP1"
  target_type      = "ip"
  vpc_id           = data.aws_vpc.default.id

  deregistration_delay = 300

  health_check {
    enabled             = true
    protocol            = "HTTP"
    port                = "traffic-port"
    path                = "/ready"
    matcher             = "200"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 5
    unhealthy_threshold = 2
  }

  tags = {
    Project     = "zero-to-prod"
    Environment = "development"
  }
}
