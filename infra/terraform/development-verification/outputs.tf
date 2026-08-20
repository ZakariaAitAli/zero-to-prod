output "verification_base_url" {
  description = "Public HTTP base URL for the temporary development verification ALB"
  value       = format("http://%s", aws_lb.verification.dns_name)
}
