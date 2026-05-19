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
- `envsubst` installed locally (part of `gettext`; on macOS: `brew install gettext`)
- GitHub PAT with `read:packages` + `write:packages` (for GHCR)

## Cloud Resource Setup (one-time, Terraform)

The S3 backup bucket, IAM service account, and Cockpit token are managed by Terraform
in the `terraform/` directory. Run this once before provisioning the server:

```bash
make infra-up
```

After apply, read the outputs needed for `.env.infra`:

```bash
cd terraform
terraform output -raw cockpit_token
terraform output -raw cockpit_logs_push_url
terraform output -raw backup_s3_access_key
terraform output -raw backup_s3_secret_key
terraform output    backup_bucket_name   # for BACKUP_S3_BUCKET: s3://<name>
terraform output    vps_ip               # for your DNS A record
```

The following resources are **not** managed by Terraform and must be set up manually:

| Resource | Notes |
|---|---|
| VPS instance | Scaleway STARDUST1-S or equivalent |
| DNS A record | Points `gradebee.app` (or your domain) to the VPS IP |

## Server Provisioning

Provisioning is split into two scripts with different lifecycles:

| Script | Make target | When to run |
|---|---|---|
| `scripts/provision-server.sh` | `make infra-server` | Once per VPS: apt, Dokku, Alloy, GHCR login, AWS CLI for backups |
| `scripts/provision-app.sh` | `make infra-app` | Once per environment: create app, set config vars, deploy image, TLS, backup cron |
| _(both in order)_ | `make infra-provision` | Convenience wrapper: runs server + app in order (first-time setup) |

### 1. Configure `.env.infra`

```bash
cp .env.infra.example .env.infra
# Edit .env.infra: fill in SSH_HOST, secrets, and config values
```

`.env.infra` is gitignored. `.env.infra.example` is committed with empty values and
comments documenting each variable.

### 2. Run the scripts

```bash
make infra-provision   # runs server + app in one pass
```

Or run Terraform + provisioning together for a full first-time setup:

```bash
make infra
```

For targeted re-runs:

```bash
make infra-server      # re-run server setup (e.g. update Alloy config)
make infra-app         # re-deploy app, update config vars, or redeploy image
```

Re-running is safe — all steps are idempotent.

### What each script does

**`scripts/provision-server.sh`** (server-level, run once per VPS):

1. **System prep** — package upgrades, base dependencies (`curl`, `git`, `ca-certificates`,
   `gnupg`, `sqlite3`, `awscli`), NTP
2. **Dokku install** — downloads and runs the official unattended installer (also installs Docker)
3. **Grafana Alloy** — imports GPG key via `gpg --dearmor`, adds APT repo, installs, deploys
   config template (Scaleway Cockpit `X-Token` auth), enables service
4. **AWS CLI config** — writes `/root/.aws/config` + `/root/.aws/credentials` (shared by all per-app backup scripts)
5. **Let's Encrypt plugin** — installs `dokku-letsencrypt`, enables auto-renewal cron
6. **GHCR credentials** — writes `config.json` directly to `/home/dokku/.docker/` (fixes the
   Ansible "become unprivileged user" ACL issue)

**`scripts/provision-app.sh`** (app-level, run once per environment):

1. **Dokku app** — creates `$APP_NAME` app, mounts `/data/$APP_NAME` volume
2. **Deploy SSH key** — appends `DEPLOY_SSH_PUBKEY` to dokku's `authorized_keys`
3. **Config vars** — sets all app environment variables via `dokku config:set`
4. **Initial deploy** — runs `dokku git:from-image` to pull the image from GHCR and start the app
5. **TLS** — sets Let's Encrypt contact email, runs `dokku letsencrypt:enable`
6. **Backup cron** — deploys `backup-db.sh` to `/opt/$APP_NAME/scripts/`, installs a 6-hourly cron

> **Prerequisite for deploy and TLS:** push the Docker image to GHCR and ensure the domain
> is resolving to the server before running `make infra-app` or `make infra-provision`.

### Provisioning additional environments

To add a staging environment on the same host, create a per-env secrets file and run the app
script with it:

```bash
cp .env.infra.example .env.staging
# Edit .env.staging: set APP_NAME=gradebee-staging, GHCR_IMAGE=...:staging, etc.
ENV_FILE=.env.staging ./scripts/provision-app.sh
```

Or via Make:

```bash
ENV_FILE=.env.staging make infra-app
```

Each environment gets its own Dokku app, data directory (`/data/gradebee-staging`), scripts
directory, and cron job. Backups go to a separate S3 key prefix (`gradebee-staging/db/`) in
the shared bucket.

### Backup retention

Backups run every 6 hours; the script keeps the 30 most recent snapshots (≈ 7.5 days rolling
window). Each app's backups are stored under `$APP_NAME/db/` in the S3 bucket to avoid
collisions between environments. For longer retention, configure an S3 lifecycle rule on the
bucket directly.

### Migration note (existing Terraform/Compose host)

If applying this to a host that previously used the Docker Compose layout, the
live database is at `/opt/gradebee/data/gradebee.db`. Before running the scripts, copy it:

```bash
ssh root@<VPS_IP> "mkdir -p /data/gradebee && cp /opt/gradebee/data/gradebee.db /data/gradebee/gradebee.db"
```

The backup script targets `/data/$APP_NAME/$APP_NAME.db` exclusively (via the host-side
data dir; the container sees it as `/data/$APP_NAME.db` through the bind mount).

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
| `DEPLOY_SSH_KEY` | Private key matching `DEPLOY_SSH_PUBKEY` in `.env.infra` |
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

### Backend runtime (set by `provision-app.sh` via `dokku config:set`, sourced from `.env.infra`)

| Variable | Secret? | Description |
|---|---|---|
| `CLERK_SECRET_KEY` | Yes | Clerk backend API key |
| `OPENAI_API_KEY` | Yes | OpenAI API key (Whisper + GPT) |
| `DB_PATH` | No | SQLite path (default `/data/gradebee.db`) |
| `UPLOADS_DIR` | No | Audio upload directory (default `/data/uploads`) |
| `UPLOAD_RETENTION_HOURS` | No | Hours to keep processed audio (default 168 = 7 days) |
| `ALLOWED_ORIGIN` | No | CORS origin (default `*`; in prod the SPA is same-origin so CORS is unused) |
| `LOG_LEVEL` | No | `DEBUG`/`INFO`/`WARN`/`ERROR` (default `INFO`) |
| `LOG_FORMAT` | No | `json` for JSON logs, else text |

To change a value after initial provisioning, update `.env.infra` and re-run
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
