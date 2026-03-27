# S3-compatible object storage bucket for SQLite backups.
resource "scaleway_object_bucket" "gradebee_backups" {
  name   = "gradebee-backups"
  region = "fr-par"
  acl    = "private"

  lifecycle_rule {
    enabled = true

    expiration {
      days = 30
    }
  }

  tags = {
    project = "gradebee"
    purpose = "db-backups"
  }
}
