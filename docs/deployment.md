# Deployment

GradeBee runs on a VPS as a Dokku application. The Go backend serves both the API and the
frontend SPA from a single Docker image. Dokku's built-in nginx proxy handles TLS (via
Let's Encrypt) and gzip compression.

## Architecture

```
Internet → :443 → Dokku nginx (TLS + gzip) → gradebee container :8080
                                                  └── /api/*  → Go handlers
                                                  └── /*      → embedded SPA (embed.FS)
```

SQLite database is stored at `/data/gradebee.db` inside a persistent volume bind-mounted
into the container by Dokku.

## Prerequisites

- VPS: bare Ubuntu/Debian (tested on Scaleway STARDUST1-S, Paris)
- Domain pointed at the VPS IP
- Ansible ≥ 2.14 installed locally
- Scaleway Cockpit token (for log shipping via Grafana Alloy)
- Scaleway Object Storage bucket (for database backups)
- GitHub PAT with `read:packages` + `write:packages` (for GHCR)

## Cloud Resource Setup (one-time, manual)

The following resources are created once via the Scaleway console or CLI and are not
managed by Ansible:

| Resource | Notes |
|---|---|
| VPS instance | Scaleway STARDUST1-S or equivalent |
| DNS A record | Points `gradebee.app` (or your domain) to the VPS IP |
| S3 backup bucket | e.g. `gradebee-backups` in `fr-par` |
| Scaleway IAM application + API key | Scoped to the backup bucket |
| Scaleway Cockpit token | `MetricsPublisher` + `LogsPublisher` roles |

## Server Provisioning (one-time)

The `ansible/provision.yml` playbook turns a bare VPS into a production-ready Dokku host.

### 1. Configure inventory

```bash
cp ansible/inventory.example ansible/inventory
# Edit ansible/inventory: replace the placeholder IP with your VPS IP
```

### 2. Store secrets

Create `secrets.yml` (keep out of git — add to `.gitignore`):

```yaml
deploy_ssh_pubkey: "ssh-ed25519 AAAA..."
ghcr_token: "ghp_xxx"
ghcr_user: "my-gh-user"
grafana_loki_url: "https://logs.cockpit.fr-par.scaleway.com/loki/api/v1/push"
cockpit_token: "xxx"
backup_s3_bucket: "s3://gradebee-backups"
backup_s3_endpoint: "https://s3.fr-par.scw.cloud"
backup_s3_region: "fr-par"
backup_s3_access_key: "xxx"
backup_s3_secret_key: "xxx"
```

Optionally encrypt with Ansible Vault:

```bash
ansible-vault encrypt secrets.yml
```

### 3. Run the playbook

```bash
ansible-playbook -i ansible/inventory ansible/provision.yml \
  -e "dokku_domain=gradebee.app" \
  -e @secrets.yml
```

Re-running is safe — all tasks are idempotent.

### What the playbook does

1. **System prep** — package upgrades, base dependencies, NTP
2. **Dokku install** — downloads and runs the official unattended installer (also installs Docker)
3. **Dokku app setup** — creates `gradebee` app, mounts `/data` volume, injects deploy SSH key,
   authenticates Docker with GHCR
4. **Grafana Alloy** — imports GPG key via `gpg --dearmor`, adds APT repo, installs, deploys
   config template (Scaleway Cockpit `X-Token` auth), enables service
5. **Backup cron** — deploys `backup-db.sh`, writes AWS CLI config for Scaleway S3, installs
   a 6-hourly cron job

### Backup retention

Backups run every 6 hours; the script keeps the 30 most recent snapshots (≈ 7.5 days rolling
window). For longer retention, configure an S3 lifecycle rule on the bucket directly.

### Migration note (existing Terraform/Compose host)

If applying this playbook to a host that previously used the Docker Compose layout, the
live database is at `/opt/gradebee/data/gradebee.db`. Before running the playbook, copy it:

```bash
ssh root@<VPS_IP> "cp /opt/gradebee/data/gradebee.db /data/gradebee.db"
```

The backup script targets `/data/gradebee.db` exclusively.

## Deployments (CI/CD)

Deployments are automated via GitHub Actions (see `.github/workflows/`):

| Workflow | Trigger | What it does |
|---|---|---|
| `deploy-production.yml` | Push to `main` | Build image → push to GHCR → `dokku git:from-image` |
| `review-app-deploy.yml` | PR opened/updated | Build image → deploy to `gradebee-pr-<N>` app |
| `review-app-teardown.yml` | PR closed | `dokku apps:destroy gradebee-pr-<N>` |

### Required repository secrets

| Secret | Description |
|---|---|
| `DEPLOY_HOST` | VPS IP address |
| `DEPLOY_SSH_KEY` | Private key matching the `deploy_ssh_pubkey` in the playbook |
| `GHCR_TOKEN` | GitHub PAT with `read:packages` + `write:packages` |

## Manual deploy / debugging

```bash
ssh root@<VPS_IP>
dokku logs gradebee -t          # tail app logs
dokku ps:report gradebee        # container status
dokku storage:list gradebee     # verify /data mount
```

To deploy a specific image manually:

```bash
dokku git:from-image gradebee ghcr.io/<owner>/gradebee:<tag>
```

## Application environment variables

Set via `dokku config:set gradebee KEY=VALUE`. Required variables:

| Variable | Required | Description |
|---|---|---|
| `CLERK_SECRET_KEY` | Yes | Clerk backend API key |
| `OPENAI_API_KEY` | Yes | OpenAI API key (Whisper + GPT) |
| `VITE_CLERK_PUBLISHABLE_KEY` | Yes | Clerk publishable key (baked into image at build time via `--build-arg`) |
| `ALLOWED_ORIGIN` | No | CORS origin (default `*`; set to `https://yourdomain` in prod) |
| `LOG_LEVEL` | No | `DEBUG`/`INFO`/`WARN`/`ERROR` (default `INFO`) |
| `LOG_FORMAT` | No | `json` for JSON logs, else text |

## Local development

For local development, use `docker compose` (see `docker-compose.yml`). The Compose file is
not used in production — it exists as a local convenience only.
