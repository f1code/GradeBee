# S3-compatible object storage bucket for SQLite backups.
resource "scaleway_object_bucket" "gradebee_backups" {
  name   = "gradebee-backups"
  region = "fr-par"

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

resource "scaleway_object_bucket_acl" "gradebee_backups" {
  bucket = scaleway_object_bucket.gradebee_backups.name
  region = "fr-par"
  acl    = "private"
}
