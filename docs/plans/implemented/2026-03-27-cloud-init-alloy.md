# Merge Alloy Install + Setup-Infra into Cloud-Init

## Goal

Eliminate the separate `scripts/install-alloy.sh` script and the `make setup-infra` SSH steps by baking everything into cloud-init. The VPS should be fully provisioned and configured after `make provision` with no manual SSH follow-up.

## Current Problem

After `make provision`, you still need to:
1. `make setup-infra` — SSHes in to install packages, copy backup script, set up cron, configure AWS CLI, install Alloy

This is fragile (requires `VPS_HOST` to be set, SSH connectivity) and could all happen at boot.

## Proposed Changes

### 1. Create `terraform/alloy-config.alloy.tftpl`

Template file for the Alloy config, with `${cockpit_token}` placeholder:

```
// --- Docker container logs ---
discovery.docker "containers" { ... }
loki.source.docker "docker_logs" { ... }
// --- Backup cron log ---
local.file_match "backup_log" { ... }
loki.source.file "backup" { ... }
// --- Ship to Scaleway Cockpit ---
loki.write "cockpit" {
  endpoint {
    url = "https://logs.cockpit.fr-par.scw.cloud/loki/api/v1/push"
    headers = { "X-Token" = "${cockpit_token}" }
  }
}
```

### 2. Create `terraform/cloud-init.yml.tftpl`

Replace the static `cloud-init.yml` with a template that includes everything:

- **packages**: existing ones + `apt-transport-https`, `software-properties-common`
- **write_files**:
  - `/etc/alloy/config.alloy` (from `templatefile("alloy-config.alloy.tftpl", ...)`)
  - `/opt/gradebee/scripts/backup-db.sh` (inline the backup script)
  - `/etc/cron.d/gradebee-backup` (the cron entry)
  - `/etc/apt/sources.list.d/grafana.list` (Grafana repo)
  - `/etc/apt/keyrings/grafana.gpg` — tricky, needs to be fetched at runtime
- **runcmd**:
  - Existing Docker install steps
  - Add Grafana GPG key + install `alloy`
  - `usermod -aG docker alloy`
  - `systemctl enable alloy && systemctl start alloy`
  - Configure AWS CLI: `aws configure set default.s3.endpoint_url ...` and `default.region`

### 3. Update `terraform/instance.tf`

Use `templatefile()` instead of `file()` for cloud-init, passing in `cockpit_token`:

```hcl
user_data = {
  cloud-init = templatefile("${path.module}/cloud-init.yml.tftpl", {
    cockpit_token      = scaleway_cockpit_token.alloy.secret_key
    backup_s3_key      = scaleway_iam_api_key.backup_key.access_key
    backup_s3_secret   = scaleway_iam_api_key.backup_key.secret_key
  })
}
```

### 4. Delete `scripts/install-alloy.sh` and `scripts/backup-db.sh`

Both are now inlined in cloud-init. (Or keep `backup-db.sh` as source-of-truth and use `file()` to inline it.)

### 5. Update `Makefile`

- Remove `setup-infra` target entirely (or keep as no-op with a message)
- Remove from `.PHONY`

### 6. Delete `terraform/cloud-init.yml`

Replaced by `cloud-init.yml.tftpl`.

## Open Questions

- Should we keep `backup-db.sh` as a standalone file (read via `file()` in the template) for easier editing, or inline it?
- The `setup-infra` target also configures AWS credentials on the VPS — we'd need to pass the IAM key/secret into cloud-init too. This means the credentials are in the cloud-init userdata (visible in Scaleway console). Acceptable?
- Should `make deploy` still rsync `scripts/` or can we stop that since scripts are baked into cloud-init?
