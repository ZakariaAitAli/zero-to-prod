terraform {
  required_version = "= 1.15.9"

  backend "s3" {
    bucket  = "zero-to-prod-333534066371-eu-west-3-dev-verification-tfstate"
    key     = "development-verification/terraform.tfstate"
    region  = "eu-west-3"
    encrypt = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "= 6.59.0"
    }
  }
}
