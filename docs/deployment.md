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

The S3 backup bucket and IAM service account are managed by Terraform
in the `terraform/` directory. Run this once before provisioning the server:

```bash
make infra-up
```

After apply, read the outputs needed for `ansible/secrets.yml`:

```bash
cd terraform
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

## Server Provisioning

Provisioning is split into two playbooks with different lifecycles:

| Playbook | Make target | When to run |
|---|---|---|
| `ansible/provision-server.yml` | `make infra-server` | Once per VPS: apt, Dokku, GHCR login, AWS CLI for backups |
| `ansible/provision-app.yml` | `make infra-app` | Once per environment: create app, set config vars, deploy image, TLS, backup cron |
| `ansible/provision.yml` | `make infra-provision` | Convenience wrapper: runs server + app in order (first-time setup) |

### 1. Configure inventory

```bash
cp ansible/inventory.example ansible/inventory
# Edit ansible/inventory: replace the placeholder IP with your VPS IP
```

### 2. Store secrets

Create `ansible/secrets.yml` (gitignored — plain text is fine):

```yaml
# Server-level secrets (provision-server.yml)
ghcr_token: "ghp_xxx"
ghcr_user: "my-gh-user"
backup_s3_bucket: "s3://gradebee-backups"
backup_s3_endpoint: "https://s3.fr-par.scw.cloud"
backup_s3_region: "fr-par"
backup_s3_access_key: "xxx"
backup_s3_secret_key: "xxx"

# App-level secrets (provision-app.yml)
deploy_ssh_pubkey: "ssh-ed25519 AAAA..."
clerk_secret_key: "sk_live_xxx"
openai_api_key: "sk-xxx"
letsencrypt_email: "you@example.com"
# Sentry cron monitor URL for the backup job (optional; leave empty to disable).
# Format: https://<ingest-host>/api/<project-id>/cron/gradebee-backup/<public-key>/
# Construct from your Sentry project DSN. Set per-environment in the app secrets file.
sentry_crons_url: ""
```

### 3. Run the playbook

```bash
make infra-provision   # runs server + app in one pass
```

Or run Terraform + Ansible together for a full first-time setup:

```bash
make infra
```

For targeted re-runs:

```bash
make infra-server      # re-run server setup (e.g. update AWS CLI config)
make infra-app         # re-deploy app, update config vars, or redeploy image
```

The Dokku domain defaults to the value in `ansible/vars.yml`. Override for a different domain:

```bash
make infra-provision DOKKU_DOMAIN=other.app
```

Re-running is safe — all tasks are idempotent.

Non-secret config (log level, paths, retention) lives in `ansible/vars.yml` and is loaded
automatically. Override individual values by adding them to `secrets.yml` or passing
`-e key=value` on the command line.

### What each playbook does

**`provision-server.yml`** (server-level, run once per VPS):

1. **System prep** — package upgrades, base dependencies, NTP
2. **Dokku install** — downloads and runs the official unattended installer (also installs Docker)
3. **AWS CLI config** — writes `/root/.aws/config` + `/root/.aws/credentials` (shared by all per-app backup scripts)
4. **Let's Encrypt plugin** — installs `dokku-letsencrypt`, enables auto-renewal cron
5. **GHCR login** — `docker login ghcr.io` (host-scoped; all apps benefit)

**`provision-app.yml`** (app-level, run once per environment):

1. **Dokku app** — creates `{{ app_name }}` app, mounts `/data/{{ app_name }}` volume
2. **Deploy SSH key** — injects `deploy_ssh_pubkey` into dokku's `authorized_keys`
3. **Config vars** — sets all app environment variables via `dokku config:set`
4. **Initial deploy** — runs `dokku git:from-image` to pull the image from GHCR and start the app
5. **TLS** — sets Let's Encrypt contact email, runs `dokku letsencrypt:enable`
6. **Backup cron** — deploys `backup-db.sh` to `/opt/{{ app_name }}/scripts/`, installs a 6-hourly cron named `{{ app_name }}-db-backup`. If `sentry_crons_url` is set in `secrets.yml`, the script sends check-ins to the Sentry Cron Monitor so you get alerted on missed or failed backups.

> **Prerequisite for deploy and TLS:** push the Docker image to GHCR and ensure the domain
> is resolving to the server before running `make infra-app` or `make infra-provision`.

### Provisioning additional environments

To add a staging environment on the same host, create a per-env secrets file and run the app
playbook with overrides:

```bash
ansible-playbook -i ansible/inventory ansible/provision-app.yml \
  -e @ansible/secrets.staging.yml \
  -e app_name=gradebee-staging \
  -e ghcr_image=ghcr.io/nicogaller/gradebee:staging
```

Or via Make for the common case:

```bash
make infra-app app_name=gradebee-staging ghcr_image=ghcr.io/nicogaller/gradebee:staging
```

Each environment gets its own Dokku app, data directory (`/data/gradebee-staging`), scripts
directory, and cron job. Backups go to a separate S3 key prefix (`gradebee-staging/db/`) in
the shared bucket.

### Backup retention

Backups run every 6 hours; the script keeps the 30 most recent snapshots (≈ 7.5 days rolling
window). Each app's backups are stored under `{{ app_name }}/db/` in the S3 bucket to avoid
collisions between environments. For longer retention, configure an S3 lifecycle rule on the
bucket directly.

### Migration note (existing Terraform/Compose host)

If applying this playbook to a host that previously used the Docker Compose layout, the
live database is at `/opt/gradebee/data/gradebee.db`. Before running the playbook, copy it:

```bash
ssh root@<VPS_IP> "mkdir -p /data/gradebee && cp /opt/gradebee/data/gradebee.db /data/gradebee/gradebee.db"
```

The backup script targets `/data/{{ app_name }}/{{ app_name }}.db` exclusively (via the host-side
data dir; the container sees it as `/data/{{ app_name }}.db` through the bind mount).

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
| `CLERK_SECRET_KEY` | Clerk backend secret key (injected into review apps via `dokku config:set`) |
| `OPENAI_API_KEY` | OpenAI API key (used when `LLM_PROVIDER=openai`) |
| `MISTRAL_API_KEY` | Mistral API key (used when `LLM_PROVIDER=mistral`; required for default config) |
| `VITE_CLERK_PUBLISHABLE_KEY` | Clerk publishable key (passed as Docker build-arg) |
| `VITE_SENTRY_DSN` | Sentry DSN (optional; passed as Docker build-arg) |

> **Note:** GHCR authentication uses `secrets.GITHUB_TOKEN` (auto-provided by GitHub Actions) — no PAT is needed.

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
| `LLM_PROVIDER` | No (`vars.yml`) | `"openai"` or `"mistral"` (default `"mistral"`) |
| `OPENAI_API_KEY` | Yes (`secrets.yml`) | OpenAI API key (used when `LLM_PROVIDER=openai`) |
| `MISTRAL_API_KEY` | Yes (`secrets.yml`) | Mistral API key (used when `LLM_PROVIDER=mistral`) |
| `DB_PATH` | No (`vars.yml`) | SQLite path (default `/data/gradebee.db`) |
| `UPLOADS_DIR` | No (`vars.yml`) | Audio upload directory (default `/data/uploads`) |
| `UPLOAD_RETENTION_HOURS` | No (`vars.yml`) | Hours to keep processed audio (default 168 = 7 days) |
| `ALLOWED_ORIGIN` | No (`vars.yml`) | CORS origin (default `*`; in prod the SPA is same-origin so CORS is unused) |
| `LOG_LEVEL` | No (`vars.yml`) | `DEBUG`/`INFO`/`WARN`/`ERROR` (default `INFO`) |
| `LOG_FORMAT` | No (`vars.yml`) | `json` for JSON logs, else text |
| `SENTRY_DSN` | No | Sentry DSN; baked into Docker image via `VITE_SENTRY_DSN` build-arg |
| `SENTRY_RELEASE` | No | Release tag; baked in via `VITE_APP_VERSION` build-arg (git SHA in CI) |
| `SENTRY_ENVIRONMENT` | No | Environment tag in Sentry (e.g. `production`); set via `dokku config:set` |

To change a value after initial provisioning, update `secrets.yml` or `vars.yml` and re-run
`make infra-app`, or set it directly: `dokku config:set gradebee KEY=VALUE`.

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
pnpm -F frontend dev
```
