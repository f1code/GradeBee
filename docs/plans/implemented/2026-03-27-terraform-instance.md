# Move VPS Instance Provisioning into Terraform

## Goal

Replace the `scripts/provision-vps.sh` and `scripts/teardown-vps.sh` shell scripts with a Terraform-managed Scaleway instance, so all infrastructure is provisioned/destroyed via `terraform apply` / `terraform destroy`.

## Proposed Changes

### 1. New file: `terraform/instance.tf`

Create the Scaleway instance resource:

```hcl
resource "scaleway_instance_ip" "public" {}

resource "scaleway_instance_server" "gradebee" {
  type  = "STARDUST1-S"
  image = "ubuntu_jammy"
  name  = "gradebee"

  ip_id     = scaleway_instance_ip.public.id
  user_data = { cloud-init = file("${path.module}/../scripts/cloud-init.yml") }

  tags = ["gradebee"]
}
```

### 2. Update `terraform/outputs.tf`

Add:

```hcl
output "vps_ip" {
  description = "Public IP of the GradeBee VPS"
  value       = scaleway_instance_ip.public.address
}
```

### 3. Delete scripts

- `scripts/provision-vps.sh`
- `scripts/teardown-vps.sh`

### 4. Update `Makefile`

- Remove the `provision` and `teardown` targets
- Remove them from `.PHONY`
- Optionally add a comment pointing to `cd terraform && terraform apply`

## Open Questions

- The current script allows overriding instance type/name/zone via env vars. Do we want Terraform variables for these, or are the hardcoded defaults fine?
- Should we add a `terraform/variables.tf` with defaults for `instance_type`, `instance_name`, `zone`?
- The `setup-infra` Makefile target SSHes into the VPS — it could use `terraform output -raw vps_ip` instead of `VPS_HOST`. Worth changing now or separate effort?
