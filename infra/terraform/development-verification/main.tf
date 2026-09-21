data "terraform_remote_state" "development_runtime" {
  backend = "s3"

  config = {
    bucket  = "zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate"
    key     = "development-runtime/terraform.tfstate"
    region  = "eu-west-3"
    encrypt = true
  }
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
    data.terraform_remote_state.development_runtime.outputs.verification_alb_security_group_id,
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
    type = "forward"

    target_group_arn = data.terraform_remote_state.development_runtime.outputs.demo_api_target_group_arn
  }
}
