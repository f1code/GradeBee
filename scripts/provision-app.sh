#!/usr/bin/env bash
# scripts/provision-app.sh — per-app / per-environment Dokku setup.
#
# Usage (from repo root):
#   ./scripts/provision-app.sh
#
# For a staging environment:
#   ENV_FILE=.env.staging ./scripts/provision-app.sh
#
# Or via Make:
#   make infra-app
#
# Reads configuration from .env.infra (see .env.infra.example).
# Safe to re-run — all steps are idempotent.
#
# Prerequisite: provision-server.sh must have run at least once on this host.
#
# Required variables (from .env.infra):
#   SSH_HOST              - VPS IP or hostname
#   SSH_USER              - SSH user (default: root)
#   APP_NAME              - Dokku application name (e.g. gradebee)
#   GHCR_IMAGE            - Full Docker image reference (e.g. ghcr.io/nicogaller/gradebee:latest)
#   DEPLOY_SSH_PUBKEY     - SSH public key for git-push deploys (written to dokku authorized_keys)
#   CLERK_SECRET_KEY      - Clerk backend API key
#   OPENAI_API_KEY        - OpenAI API key
#   LETSENCRYPT_EMAIL     - Contact email for Let's Encrypt cert
#   BACKUP_S3_BUCKET      - S3 bucket URL (e.g. s3://gradebee-backups)
#   APP_DB_PATH           - SQLite path inside container (default: /data/${APP_NAME}.db)
#   APP_UPLOADS_DIR       - Upload directory (default: /data/uploads)
#   APP_UPLOAD_RETENTION_HOURS - Hours to keep processed audio (default: 168)
#   APP_ALLOWED_ORIGIN    - CORS origin (default: *)
#   APP_LOG_LEVEL         - Log level (default: INFO)
#   APP_LOG_FORMAT        - Log format: json or text (default: json)

set -euo pipefail

# --- Load common helpers (env, run_remote, scp_to_remote, log) ---
source "$(dirname "$0")/lib/common.sh"

# --- Validate required vars ---
require_vars \
  SSH_HOST APP_NAME GHCR_IMAGE \
  DEPLOY_SSH_PUBKEY \
  CLERK_SECRET_KEY OPENAI_API_KEY \
  LETSENCRYPT_EMAIL \
  BACKUP_S3_BUCKET

# --- Derived values (override in .env.infra if needed) ---
DATA_DIR="${DATA_DIR:-/data/${APP_NAME}}"
SCRIPTS_DIR="${SCRIPTS_DIR:-/opt/${APP_NAME}/scripts}"
BACKUP_LOG="${BACKUP_LOG:-/var/log/${APP_NAME}-backup.log}"

# --- App config defaults (mirror ansible/vars.yml) ---
APP_DB_PATH="${APP_DB_PATH:-/data/${APP_NAME}.db}"
APP_UPLOADS_DIR="${APP_UPLOADS_DIR:-/data/uploads}"
APP_UPLOAD_RETENTION_HOURS="${APP_UPLOAD_RETENTION_HOURS:-168}"
APP_ALLOWED_ORIGIN="${APP_ALLOWED_ORIGIN:-*}"
APP_LOG_LEVEL="${APP_LOG_LEVEL:-INFO}"
APP_LOG_FORMAT="${APP_LOG_FORMAT:-json}"

log "=== provision-app.sh starting (app: ${APP_NAME}, host: ${SSH_HOST}) ==="

# ---------------------------------------------------------------------------
# Render backup script locally (substitutes APP_NAME and BACKUP_S3_BUCKET)
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TMP_BACKUP=$(mktemp /tmp/backup-db.XXXXXX.sh)
TMP_AUTHORIZED_KEYS=$(mktemp /tmp/authorized_keys.XXXXXX)

cleanup() {
  rm -f "$TMP_BACKUP" "$TMP_AUTHORIZED_KEYS"
}
trap cleanup EXIT

envsubst < "${SCRIPT_DIR}/lib/backup-db.sh.tmpl" > "$TMP_BACKUP"
chmod 755 "$TMP_BACKUP"

# Write deploy pubkey to a temp file (avoid any shell quoting issues with key content)
printf '%s\n' "$DEPLOY_SSH_PUBKEY" > "$TMP_AUTHORIZED_KEYS"

# ---------------------------------------------------------------------------
# 1. Create Dokku app (idempotent)
# ---------------------------------------------------------------------------
log "--- 1. Create Dokku app ---"
run_remote bash <<REMOTE
set -euo pipefail
if dokku apps:exists ${APP_NAME} 2>/dev/null; then
  echo "App '${APP_NAME}' already exists."
else
  dokku apps:create ${APP_NAME}
fi
REMOTE

# ---------------------------------------------------------------------------
# 2. Persistent storage
# ---------------------------------------------------------------------------
log "--- 2. Persistent storage ---"
run_remote bash <<REMOTE
set -euo pipefail
mkdir -p ${DATA_DIR}
# NOTE: Container write access depends on Dockerfile USER. Verify after task #14.
if ! dokku storage:list ${APP_NAME} 2>/dev/null | grep -q "${DATA_DIR}"; then
  dokku storage:mount ${APP_NAME} ${DATA_DIR}:/data
else
  echo "Storage mount already present."
fi
REMOTE

# ---------------------------------------------------------------------------
# 3. Deploy SSH key
# ---------------------------------------------------------------------------
log "--- 3. Deploy SSH key ---"
run_remote bash <<'REMOTE'
set -euo pipefail
mkdir -p /home/dokku/.ssh
chown dokku:dokku /home/dokku/.ssh
chmod 700 /home/dokku/.ssh
touch /home/dokku/.ssh/authorized_keys
chown dokku:dokku /home/dokku/.ssh/authorized_keys
chmod 600 /home/dokku/.ssh/authorized_keys
REMOTE

# Use scp to transfer the pubkey file, then append it on the remote
scp_to_remote "$TMP_AUTHORIZED_KEYS" "/tmp/deploy_pubkey_$$"
run_remote bash <<REMOTE
set -euo pipefail
PUBKEY=\$(cat /tmp/deploy_pubkey_$$)
rm -f /tmp/deploy_pubkey_$$
if ! grep -qF "\$PUBKEY" /home/dokku/.ssh/authorized_keys; then
  echo "\$PUBKEY" >> /home/dokku/.ssh/authorized_keys
  echo "Deploy SSH key added."
else
  echo "Deploy SSH key already present."
fi
REMOTE

# ---------------------------------------------------------------------------
# 4. Dokku application environment variables
# ---------------------------------------------------------------------------
log "--- 4. App environment variables ---"
# Write secrets to a temp file on the remote host and source it.
# This avoids interpolating secret values as shell arguments (they could contain
# spaces, $, quotes, or other metacharacters that would silently corrupt them).
TMP_APP_ENV=$(mktemp /tmp/app-env.XXXXXX)
chmod 600 "$TMP_APP_ENV"
cat > "$TMP_APP_ENV" <<EOF
CLERK_SECRET_KEY=${CLERK_SECRET_KEY}
OPENAI_API_KEY=${OPENAI_API_KEY}
EOF
scp_to_remote "$TMP_APP_ENV" "/tmp/app-env-$$"
rm -f "$TMP_APP_ENV"

run_remote bash <<REMOTE
set -euo pipefail
# Source secrets from temp file
set -a
source /tmp/app-env-$$
set +a
rm -f /tmp/app-env-$$

dokku config:set --no-restart ${APP_NAME} \\
  CLERK_SECRET_KEY="\$CLERK_SECRET_KEY" \\
  OPENAI_API_KEY="\$OPENAI_API_KEY" \\
  DB_PATH=${APP_DB_PATH} \\
  UPLOADS_DIR=${APP_UPLOADS_DIR} \\
  UPLOAD_RETENTION_HOURS=${APP_UPLOAD_RETENTION_HOURS} \\
  ALLOWED_ORIGIN=${APP_ALLOWED_ORIGIN} \\
  LOG_LEVEL=${APP_LOG_LEVEL} \\
  LOG_FORMAT=${APP_LOG_FORMAT}
REMOTE

# ---------------------------------------------------------------------------
# 5. Initial app deployment
# ---------------------------------------------------------------------------
# Triggers the predeploy hook (/gradebee --migrate-only) before routing traffic.
# Re-running is safe — it simply redeploys the same (or newer) image.
log "--- 5. Deploy image from GHCR ---"
run_remote bash <<REMOTE
set -euo pipefail
dokku git:from-image ${APP_NAME} ${GHCR_IMAGE}
REMOTE

# ---------------------------------------------------------------------------
# 6. TLS — Let's Encrypt per-app cert
# ---------------------------------------------------------------------------
# Requires domain DNS to resolve to this host and app to be running on port 80.
log "--- 6. Let's Encrypt TLS ---"
run_remote bash <<REMOTE
set -euo pipefail
dokku letsencrypt:set ${APP_NAME} email ${LETSENCRYPT_EMAIL}
# letsencrypt:enable is idempotent when the cert already exists and is valid,
# but fails on some Dokku versions if already enabled. Guard defensively.
if dokku letsencrypt:list 2>/dev/null | grep -q "^${APP_NAME} "; then
  echo "Let's Encrypt already enabled for ${APP_NAME}."
else
  dokku letsencrypt:enable ${APP_NAME}
fi
REMOTE

# ---------------------------------------------------------------------------
# 7. Per-app backup script, log file, and cron job
# ---------------------------------------------------------------------------
log "--- 7. Backup script and cron ---"
run_remote bash <<REMOTE
set -euo pipefail
mkdir -p ${SCRIPTS_DIR}
touch ${BACKUP_LOG}
chmod 644 ${BACKUP_LOG}
REMOTE

scp_to_remote "$TMP_BACKUP" "${SCRIPTS_DIR}/backup-db.sh"
run_remote bash <<REMOTE
set -euo pipefail
chmod 755 ${SCRIPTS_DIR}/backup-db.sh
# RPO note: 6-hourly matches the original cloud-init cadence.
# Cron name includes app_name to avoid collisions across environments.
(crontab -l 2>/dev/null | grep -v "${APP_NAME}-db-backup"; \
 echo "0 */6 * * * ${SCRIPTS_DIR}/backup-db.sh >> ${BACKUP_LOG} 2>&1  # ${APP_NAME}-db-backup") \
 | crontab -
echo "Cron job installed."
REMOTE

log "=== provision-app.sh complete ==="
