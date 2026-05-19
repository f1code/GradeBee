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
- Terraform ≥ 1.0 + Scaleway provider configured (`SCW_ACCESS_KEY`, `SCW_SECRET_KEY`, `SCW_DEFAULT_PROJECT_ID`)
- Ansible ≥ 2.14 installed locally
- GitHub PAT with `read:packages` + `write:packages` (for GHCR)

## Cloud Resource Setup (one-time, Terraform)

The S3 backup bucket, IAM service account, and Cockpit token are managed by Terraform
in the `terraform/` directory. Run this once before provisioning the server:

```bash
make infra-up
```

After apply, read the outputs needed for `ansible/secrets.yml`:

```bash
cd terraform
terraform output -raw cockpit_token
terraform output -raw cockpit_logs_push_url
terraform output -raw backup_s3_access_key
terraform output -raw backup_s3_secret_key
terraform output    backup_bucket_name   # for backup_s3_bucket: s3://<name>
terraform output    vps_ip               # for your DNS A record
```

The following resources are **not** managed by Terraform and must be set up manually:

| Resource | Notes |
|---|---|
| VPS instance | Scaleway STARDUST1-S or equivalent |
| DNS A record | Points `gradebee.app` (or your domain) to the VPS IP |

## Server Provisioning (one-time)

The `ansible/provision.yml` playbook turns a bare VPS into a production-ready Dokku host.

### 1. Configure inventory

```bash
cp ansible/inventory.example ansible/inventory
# Edit ansible/inventory: replace the placeholder IP with your VPS IP
```

### 2. Store secrets

Create `ansible/secrets.yml` (gitignored — plain text is fine):

```yaml
# Infrastructure secrets
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

# Application secrets (set as Dokku config vars on the server)
clerk_secret_key: "sk_live_xxx"
openai_api_key: "sk-xxx"
```

### 3. Run the playbook

```bash
make infra-provision
```

Or run Terraform + Ansible together for a full first-time setup:

```bash
make infra
```

The Dokku domain defaults to the value in `ansible/vars.yml`. Override for a different domain:

```bash
make infra-provision DOKKU_DOMAIN=other.app
```

Re-running is safe — all tasks are idempotent.

Non-secret config (log level, paths, retention) lives in `ansible/vars.yml` and is loaded
automatically. Override individual values by adding them to `secrets.yml` or passing
`-e key=value` on the command line.

### What the playbook does

1. **System prep** — package upgrades, base dependencies, NTP
2. **Dokku install** — downloads and runs the official unattended installer (also installs Docker)
3. **Dokku app setup** — creates `gradebee` app, mounts `/data` volume, injects deploy SSH key,
   authenticates Docker with GHCR, sets all app environment variables via `dokku config:set`
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

There are two distinct sets of variables:

### Backend runtime (set by Ansible via `dokku config:set`, sourced from `secrets.yml` / `vars.yml`)

| Variable | Secret? | Description |
|---|---|---|
| `CLERK_SECRET_KEY` | Yes (`secrets.yml`) | Clerk backend API key |
| `OPENAI_API_KEY` | Yes (`secrets.yml`) | OpenAI API key (Whisper + GPT) |
| `DB_PATH` | No (`vars.yml`) | SQLite path (default `/data/gradebee.db`) |
| `UPLOADS_DIR` | No (`vars.yml`) | Audio upload directory (default `/data/uploads`) |
| `UPLOAD_RETENTION_HOURS` | No (`vars.yml`) | Hours to keep processed audio (default 168 = 7 days) |
| `ALLOWED_ORIGIN` | No (`vars.yml`) | CORS origin (default `*`; in prod the SPA is same-origin so CORS is unused) |
| `LOG_LEVEL` | No (`vars.yml`) | `DEBUG`/`INFO`/`WARN`/`ERROR` (default `INFO`) |
| `LOG_FORMAT` | No (`vars.yml`) | `json` for JSON logs, else text |

To change a value after initial provisioning, update `secrets.yml` or `vars.yml` and re-run
the playbook, or set it directly: `dokku config:set gradebee KEY=VALUE`.

### Frontend build-time (passed as `--build-arg` to `docker build`)

These are baked into the JS bundle at image build time. CI passes them from GitHub repository secrets.

| Variable | Required | Description |
|---|---|---|
| `VITE_CLERK_PUBLISHABLE_KEY` | Yes | Clerk publishable key |
| `VITE_API_URL` | No | API base URL (default `/api`, same origin) |
| `VITE_SENTRY_DSN` | No | Sentry DSN (omit to disable Sentry) |
| `VITE_APP_VERSION` | No | Release tag for Sentry (CI passes `${{ github.sha }}`) |

## Troubleshooting

- **Migrations fail on deploy** — check `dokku logs gradebee --num 200` for the predeploy output. The predeploy hook is `/gradebee --migrate-only` (see `app.json`); a non-zero exit aborts the deploy.
- **Frontend shows blank page** — usually a missing build arg (`VITE_CLERK_PUBLISHABLE_KEY` not set at build time). The bundle throws on load; inspect the browser console.
- **502 from nginx** — the binary panicked. Check `dokku logs gradebee` for the stack trace. Common cause: missing `CLERK_SECRET_KEY` runtime var.
- **gzip not applied** — verify Dokku's nginx config gzips `application/javascript`, `text/css`, `application/json`. Override via `dokku nginx:set gradebee` or `nginx.conf.sigil` if needed.

## Local development

For local development, use `docker compose` (see `docker-compose.yml`). The Compose file is
not used in production — it exists as a local convenience only.

For day-to-day work, run backend and frontend separately:

```bash
# Backend
cd backend && go run ./cmd/server

# Frontend (in another shell) — set VITE_API_URL=http://localhost:8080/api in frontend/.env
npm run --prefix frontend dev
```
