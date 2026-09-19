locals {
  bucket_name = "zero-to-prod-333534066371-eu-west-3-deployment-records"
}

resource "aws_s3_bucket" "deployment_records" {
  bucket        = local.bucket_name
  force_destroy = false

  tags = {
    Project     = "zero-to-prod"
    Purpose     = "verified-deployment-records"
    Environment = "development"
  }
}

resource "aws_s3_bucket_public_access_block" "deployment_records" {
  bucket = aws_s3_bucket.deployment_records.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_versioning" "deployment_records" {
  bucket = aws_s3_bucket.deployment_records.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "deployment_records" {
  bucket = aws_s3_bucket.deployment_records.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_policy" "deployment_records" {
  bucket = aws_s3_bucket.deployment_records.id

  policy = jsonencode({
    Version = "2012-10-17"

    Statement = [
      {
        Sid       = "RequireConditionalVerifiedRecordWrites"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:PutObject"
        Resource  = "${aws_s3_bucket.deployment_records.arn}/development/*"

        Condition = {
          Null = {
            "s3:if-none-match" = "true"
          }
        }
      }
    ]
  })
}
