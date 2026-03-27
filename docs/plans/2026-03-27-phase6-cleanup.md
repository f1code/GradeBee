# Phase 6: Cleanup + Audio Storage + Backup

**Goal:** Remove dead Google Sheets/Docs dependencies, finalize Docker volume setup for DB + uploads, and implement automated SQLite backups to Scaleway Object Storage.

**Prerequisite:** Phases 1–5 complete. All handlers already use DB-backed repos; Sheets/Docs imports are dead code at this point.

---

## 1. Backend — Remove Google Sheets/Docs Deps

By Phase 6, earlier phases have already rewritten all handlers to use DB repos. The following files still import `google.golang.org/api/sheets/v4` or `google.golang.org/api/docs/v1` and need cleanup:

| File | Action |
|------|--------|
| `backend/google.go` | Remove `sheets/v4` and `docs/v1` imports. Remove `NewSheetsService` / `NewDocsService` funcs. Keep only the Drive v3 client constructor (used by `drive_import.go`). |
| `backend/notes.go` | Should already be rewritten in Phase 2 to use `NoteRepo`. If any residual `docs/v1` import remains, remove it. |
| `backend/report_generator.go` | Should already return HTML via DB in Phase 2. Remove any leftover `docs/v1` import and Google Docs creation logic. |
| `backend/setup.go` | **DELETE** — `/setup` endpoint removed in Phase 2. File imports `sheets/v4`; entire file is dead code. |
| `backend/drive_store.go` | **DELETE** — Drive-based storage replaced by disk + DB in Phase 2. |
| `backend/clerk_metadata.go` | **DELETE** — spreadsheet/folder IDs in Clerk metadata no longer used. |
| `backend/roster.go` | Remove any Sheets-based roster impl (replaced by `ClassRepo`/`StudentRepo` in Phase 2). Keep only the DB-backed impl. |
| `backend/deps.go` | Remove `GoogleServices`, `GoogleServicesForUser`, and any getters that return Sheets/Docs services. Keep `GetDriveService` (for Drive imports). |

### Files to keep unchanged
- `backend/drive_import.go` — uses `google.golang.org/api/drive/v3` to download picked files. No changes.
- `backend/auth.go` — provides Google OAuth token for Drive Picker. No changes.

## 2. `backend/go.mod`

Run `go mod tidy` after removing imports above. Expected result:
- **Keep:** `google.golang.org/api` (still needed for `drive/v3` sub-package)
- **Removed as indirect deps (by tidy):** Any transitive deps that were only pulled in by Sheets/Docs and aren't needed by Drive. The `cloud.google.com/go/*`, `google.golang.org/grpc`, `google.golang.org/genproto` packages may still be required transitively by the Drive client — `go mod tidy` will sort it out.
- **Keep:** `github.com/google/uuid` — likely used elsewhere (upload file naming).

Verify with `go build ./...` after tidy.

## 3. `.env.example`

### Remove
No Google-specific env vars currently exist in `.env.example` (confirmed — Clerk handles OAuth). Nothing to remove.

### Add
```
# SQLite database path (inside container)
DB_PATH=/data/gradebee.db

# Upload directory (inside container)
UPLOAD_DIR=/data/uploads

# Hours to keep processed audio files before cleanup (default 168 = 7 days)
UPLOAD_RETENTION_HOURS=168
```

Keep all existing vars (`CLERK_SECRET_KEY`, `OPENAI_API_KEY`, `ALLOWED_ORIGIN`, `LOG_LEVEL`, `DOMAIN`, `VPS_HOST`, Scaleway creds).

## 4. `docker-compose.yml`

### Changes to `backend` service
- Add named volume mount: `gradebee-data:/data` — holds both `gradebee.db` and `uploads/` subdirectory.
- Add environment pass-through for `DB_PATH`, `UPLOAD_DIR`, `UPLOAD_RETENTION_HOURS` (or rely on `env_file: .env` which already passes everything).

### Add volume
```yaml
volumes:
  caddy-data:
  caddy-config:
  gradebee-data:
```

No Google-specific config exists in `docker-compose.yml` currently, so nothing to remove.

### Result
The `/data` directory inside the container maps to a persistent Docker volume on the host. On the VPS this volume lives at `/var/lib/docker/volumes/gradebee_gradebee-data/_data/` by default, but the backup script accesses the DB directly via a bind-mount path (see §7 below).

**Decision:** Switch from named volume to bind mount so the backup script (running on host) can access the DB file directly:

```yaml
backend:
  volumes:
    - /opt/gradebee/data:/data
```

This maps host `/opt/gradebee/data/` → container `/data/`. The backup script reads from `/opt/gradebee/data/gradebee.db`.

## 5. `frontend/src/components/AudioUpload.tsx`

**No changes.** Drive Picker stays. Backend transparently downloads the picked file to local disk (handled in Phase 2). Frontend is unaffected.

## 6. `Makefile`

### Changes to `deploy` target
- Add `--include='scripts/***'` to rsync so `backup-db.sh` is deployed to the VPS.
- Ensure `/opt/gradebee/data/` directory exists on VPS (add `ssh` command: `mkdir -p $(VPS_DIR)/data`).

### New targets

```makefile
# Install backup cron + aws CLI config on VPS
setup-backups:
	ssh $(VPS_HOST) 'apt-get install -y sqlite3 awscli'
	scp scripts/backup-db.sh $(VPS_HOST):$(VPS_DIR)/scripts/backup-db.sh
	ssh $(VPS_HOST) 'chmod +x $(VPS_DIR)/scripts/backup-db.sh'
	ssh $(VPS_HOST) 'cat > /etc/cron.d/gradebee-backup <<EOF
0 */6 * * *  root  $(VPS_DIR)/scripts/backup-db.sh >> /var/log/gradebee-backup.log 2>&1
EOF'
	ssh $(VPS_HOST) 'aws configure set default.s3.endpoint_url https://s3.fr-par.scw.cloud'
	ssh $(VPS_HOST) 'aws configure set default.region fr-par'

# Run backup manually
backup:
	ssh $(VPS_HOST) '$(VPS_DIR)/scripts/backup-db.sh'

# List existing backups
backup-list:
	ssh $(VPS_HOST) 'aws s3 ls s3://gradebee-backups/db/'

# Restore from latest backup
backup-restore:
	ssh $(VPS_HOST) 'LATEST=$$(aws s3 ls s3://gradebee-backups/db/ | sort | tail -1 | awk "{print \$$4}") && \
		aws s3 cp s3://gradebee-backups/db/$$LATEST /tmp/restore.db && \
		docker compose -f $(VPS_DIR)/docker-compose.yml stop backend && \
		cp /tmp/restore.db $(VPS_DIR)/data/gradebee.db && \
		docker compose -f $(VPS_DIR)/docker-compose.yml start backend'
```

### Update `provision` target
Add call to `setup-backups` after existing provisioning, or integrate into `scripts/provision-vps.sh`.

## 7. `scripts/backup-db.sh`

**NEW file.** As specified in the master plan's Backup section.

### Logic
1. Set defaults: `DB_PATH=/opt/gradebee/data/gradebee.db`, `S3_BUCKET=s3://gradebee-backups`, `BACKUP_RETENTION_DAYS=30`
2. Generate timestamp (`YYYYMMDDTHHMMSSz`)
3. Run `sqlite3 "$DB_PATH" ".backup '/tmp/gradebee-${TIMESTAMP}.db'"` — safe online backup, doesn't lock WAL writers
4. Upload to `${BUCKET}/db/${TIMESTAMP}.db` via `aws s3 cp`
5. Remove local temp file
6. Prune: list objects in `${BUCKET}/db/`, sort chronologically, delete all but the newest 30 (retention count, not days — at 4 backups/day, 30 backups ≈ 7.5 days)
7. Exit 0 on success; `set -euo pipefail` ensures any failure aborts and is logged

### Error handling
- `set -euo pipefail` at top
- Cron redirects stdout+stderr to `/var/log/gradebee-backup.log`
- If `sqlite3` or `aws` fails, script exits non-zero and the temp file may remain (acceptable, `/tmp` is cleaned by OS)

## 8. Cron Setup

Installed by `make setup-backups` (see §6). Writes to `/etc/cron.d/gradebee-backup`:

```
0 */6 * * *  root  /opt/gradebee/scripts/backup-db.sh >> /var/log/gradebee-backup.log 2>&1
```

- Runs every 6 hours (00:00, 06:00, 12:00, 18:00 UTC)
- 30-backup retention → ~7.5 days of history
- Runs as root on host (outside Docker) — direct access to bind-mounted DB file

## 9. Terraform

Currently no `terraform/` directory exists (provisioning is done via `scripts/provision-vps.sh` using Scaleway CLI). Terraform resources are **new**.

### New directory: `terraform/`

#### `terraform/main.tf`
- Scaleway provider config (`fr-par` region)
- Backend config (local state or Scaleway S3 backend for state — recommend local for simplicity)

#### `terraform/storage.tf`
- **`scaleway_object_bucket.gradebee_backups`** — creates the `gradebee-backups` bucket in `fr-par` region. ACL private. No versioning needed (we manage retention ourselves). Lifecycle rule optional (belt-and-suspenders expiry at 30 days).

#### `terraform/iam.tf`
Three resources:

1. **`scaleway_iam_application.gradebee_backup`** — service account (IAM application) named `gradebee-backup`. This is the identity the VPS uses for S3 access.

2. **`scaleway_iam_policy.backup_s3_access`** — policy granting `ObjectStorageObjectAccess` permission (read, write, list, delete objects) scoped to the `gradebee-backups` bucket only. Rule:
   - Permission set: `ObjectStorageObjectAccess` (covers `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, `s3:ListBucket`)
   - Resource scope: `projects/<project_id>/buckets/gradebee-backups`
   - Attached to the IAM application above

3. **`scaleway_iam_api_key.backup_key`** — API key for the IAM application. The `access_key` and `secret_key` outputs are used to configure `aws` CLI on the VPS. These are written as sensitive outputs.

> **Note:** Scaleway doesn't support instance IAM roles the way AWS does. We use a scoped API key instead, configured in `~/.aws/credentials` on the VPS by `make setup-backups`.

#### `terraform/outputs.tf`
- `backup_s3_access_key` (sensitive) — for configuring `aws` CLI on VPS
- `backup_s3_secret_key` (sensitive) — same
- `backup_bucket_name` — bucket name for reference

### Integration with `make setup-backups`
After `terraform apply`, the operator copies the API key credentials to the VPS (via `make setup-backups` which writes `~/.aws/credentials`). Alternatively, store them in `.env` and pass during provisioning.

## 10. `make provision` Updates

Update `scripts/provision-vps.sh` (or add to `make setup-backups`) to:

1. Install `sqlite3` package (`apt-get install -y sqlite3`)
2. Install `awscli` (`apt-get install -y awscli`)
3. Configure aws CLI for Scaleway S3 endpoint:
   - `~/.aws/config`: `endpoint_url = https://s3.fr-par.scw.cloud`, `region = fr-par`
   - `~/.aws/credentials`: access key + secret key from Terraform output
4. Deploy `scripts/backup-db.sh` to `/opt/gradebee/scripts/`
5. Install cron job to `/etc/cron.d/gradebee-backup`
6. Verify: `sqlite3 --version`, `aws s3 ls s3://gradebee-backups/` (should return empty or existing backups)
7. Create data directory: `mkdir -p /opt/gradebee/data/uploads`

---

## 11. Logging — Scaleway Cockpit

Ship all logs to Scaleway Cockpit (managed Loki/Grafana) so we never need to SSH to read logs.

### What gets shipped
- **Docker container logs** (backend + caddy) — via Grafana Alloy collecting from Docker daemon
- **Backup cron output** — Alloy tails `/var/log/gradebee-backup.log`

### Setup

#### `scripts/install-alloy.sh` (NEW)
1. Add Grafana APT repo + install `alloy` package
2. Write config to `/etc/alloy/config.alloy`:
   - **Docker source:** `loki.source.docker` — auto-discovers running containers, attaches `container_name` label
   - **File source:** `local.file_match` for `/var/log/gradebee-backup.log`, label `job=gradebee-backup`
   - **Sink:** `loki.write` endpoint pointing to Cockpit push URL (`https://logs.cockpit.fr-par.scw.cloud/loki/api/v1/push`) with Cockpit token header
3. Enable + start `alloy` systemd service

#### Cockpit token
- Create a Cockpit token in Scaleway console (or via Terraform — `scaleway_cockpit_token` resource) with `logs:write` scope
- Token passed to Alloy config as the `X-Token` header value

#### Terraform additions (`terraform/cockpit.tf`, NEW)
- **`scaleway_cockpit.main`** — activate Cockpit for the project (idempotent)
- **`scaleway_cockpit_token.alloy`** — push token with `logs:write` scope, output as sensitive

#### Makefile
- `make setup-backups` → rename to **`make setup-infra`** (covers backups + logging)
- Add Alloy install step: `bash scripts/install-alloy.sh`

#### `make provision` updates
- Add Alloy installation to `scripts/provision-vps.sh`

### Querying
- Scaleway console → Cockpit → Grafana → Explore
- Filter by `container_name="backend"` or `job="gradebee-backup"`
- Free tier: 30M samples/month, 7-day retention — more than enough

---

## Open Questions

1. **Terraform state storage** — local file committed to repo, or remote S3 backend on Scaleway? Local is simpler for a single-operator project.
2. **Cockpit alerting** — configure a Grafana alert in Cockpit for backup failures (grep for non-zero exit or absence of recent backup log entry)? Or defer to later?
