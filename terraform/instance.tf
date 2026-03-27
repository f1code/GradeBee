# VPS instance with a dedicated public IP.
resource "scaleway_instance_ip" "public" {}

resource "scaleway_instance_server" "gradebee" {
  type  = "STARDUST1-S"
  image = "ubuntu_jammy"
  name  = "gradebee"

  ip_id = scaleway_instance_ip.public.id

  user_data = {
    cloud-init = templatefile("${path.module}/cloud-init.yml.tftpl", {
      cockpit_token    = scaleway_cockpit_token.alloy.secret_key
      backup_s3_key    = scaleway_iam_api_key.backup_key.access_key
      backup_s3_secret = scaleway_iam_api_key.backup_key.secret_key
      backup_script    = file("${path.module}/backup-db.sh")
      alloy_config = templatefile("${path.module}/alloy-config.alloy.tftpl", {
        cockpit_token = scaleway_cockpit_token.alloy.secret_key
      })
    })
  }

  tags = ["gradebee"]
}
