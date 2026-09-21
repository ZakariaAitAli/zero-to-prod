resource "aws_vpc_security_group_ingress_rule" "verification_alb_http" {
  security_group_id = aws_security_group.verification_alb.id

  cidr_ipv4   = "0.0.0.0/0"
  from_port   = 80
  ip_protocol = "tcp"
  to_port     = 80

  description = "Allow public HTTP to development ALB"
}

resource "aws_vpc_security_group_egress_rule" "verification_alb_to_demo_api" {
  security_group_id = aws_security_group.verification_alb.id

  referenced_security_group_id = aws_security_group.demo_api.id
  from_port                    = 8080
  ip_protocol                  = "tcp"
  to_port                      = 8080

  description = "Allow ALB traffic only to demo API tasks"
}

resource "aws_vpc_security_group_ingress_rule" "demo_api_from_verification_alb" {
  security_group_id = aws_security_group.demo_api.id

  referenced_security_group_id = aws_security_group.verification_alb.id
  from_port                    = 8080
  ip_protocol                  = "tcp"
  to_port                      = 8080

  description = "Allow demo API traffic only from ALB"
}

resource "aws_vpc_security_group_egress_rule" "demo_api_all" {
  security_group_id = aws_security_group.demo_api.id

  cidr_ipv4   = "0.0.0.0/0"
  ip_protocol = "-1"
}
