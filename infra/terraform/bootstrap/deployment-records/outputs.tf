output "bucket_name" {
  value = aws_s3_bucket.deployment_records.bucket
}

output "bucket_arn" {
  value = aws_s3_bucket.deployment_records.arn
}
