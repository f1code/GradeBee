output "backup_s3_access_key" {
  description = "Access key for gradebee-backup IAM application"
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

