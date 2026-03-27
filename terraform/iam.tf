# Service account (IAM application) for backup S3 access from the VPS.
resource "scaleway_iam_application" "gradebee_backup" {
  name        = "gradebee-backup"
  description = "Service account for GradeBee SQLite backup uploads to S3"
}

# Policy granting object-level S3 access scoped to the backup bucket.
resource "scaleway_iam_policy" "backup_s3_access" {
  name           = "gradebee-backup-s3"
  description    = "Allow read/write/list/delete on gradebee-backups bucket"
  application_id = scaleway_iam_application.gradebee_backup.id

  rule {
    permission_set_names = ["ObjectStorageBucketsRead", "ObjectStorageObjectsRead", "ObjectStorageObjectsWrite", "ObjectStorageObjectsDelete"]
    project_ids          = [data.scaleway_account_project.current.id]
  }
}

# Look up the current project so we can scope the policy.
data "scaleway_account_project" "current" {}

# API key for the IAM application — used to configure aws CLI on the VPS.
resource "scaleway_iam_api_key" "backup_key" {
  application_id = scaleway_iam_application.gradebee_backup.id
  description    = "API key for gradebee-backup S3 access"
  expires_at     = timeadd(timestamp(), "8760h") # 1 year

  lifecycle {
    ignore_changes = [expires_at]
  }
}
