data "aws_vpc" "default" {
  default = true
}

data "aws_subnet" "runtime_a" {
  vpc_id            = data.aws_vpc.default.id
  availability_zone = "eu-west-3a"
  default_for_az    = true
}

data "aws_subnet" "runtime_b" {
  vpc_id            = data.aws_vpc.default.id
  availability_zone = "eu-west-3b"
  default_for_az    = true
}

locals {
  runtime_subnet_ids = [
    data.aws_subnet.runtime_a.id,
    data.aws_subnet.runtime_b.id,
  ]
}
