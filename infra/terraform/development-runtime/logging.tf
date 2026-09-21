resource "aws_cloudwatch_log_group" "demo_api" {
  name              = "/zero-to-prod/development/demo-api"
  retention_in_days = 7
}
