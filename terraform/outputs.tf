output "backup_s3_access_key" {
  description = "Access key for gradebee-backup IAM application (configure in aws CLI on VPS)"
  value       = scaleway_iam_api_key.backup_key.access_key
  sensitive   = true
}

output "backup_s3_secret_key" {
  description = "Secret key for gradebee-backup IAM application"
  value       = scaleway_iam_api_key.backup_key.secret_key
  sensitive   = true
}

output "backup_bucket_name" {
  description = "Name of the S3 bucket for backups"
  value       = scaleway_object_bucket.gradebee_backups.name
}

output "cockpit_logs_push_url" {
  description = "Cockpit Loki push URL for Alloy"
  value       = "https://logs.cockpit.fr-par.scw.cloud/loki/api/v1/push"
}

output "cockpit_token" {
  description = "Cockpit push token for Alloy"
  value       = scaleway_cockpit_token.alloy.secret_key
  sensitive   = true
}

output "vps_ip" {
  description = "Public IP of the GradeBee VPS"
  value       = scaleway_instance_ip.public.address
}
