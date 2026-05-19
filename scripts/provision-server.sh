#!/usr/bin/env bash
# scripts/provision-server.sh — server-level VPS setup (run once per host).
#
# Usage (from repo root):
#   ./scripts/provision-server.sh
#
# Or via Make:
#   make infra-server
#
# Reads configuration from .env.infra (see .env.infra.example).
# Safe to re-run — all steps are idempotent.
#
# Required variables (from .env.infra):
#   SSH_HOST           - VPS IP or hostname
#   SSH_USER           - SSH user (default: root)
#   DOKKU_DOMAIN       - Primary domain for Dokku global vhost (e.g. gradebee.app)
#   GHCR_USER          - GitHub username / org owning the GHCR packages
#   GHCR_TOKEN         - GitHub PAT with read:packages for docker login ghcr.io
#   GRAFANA_LOKI_URL   - Loki push URL (Scaleway Cockpit endpoint)
#   COCKPIT_TOKEN      - Scaleway Cockpit token (X-Token header)
#   BACKUP_S3_BUCKET   - S3 bucket URL (e.g. s3://gradebee-backups)
#   BACKUP_S3_ENDPOINT - S3-compatible endpoint URL
#   BACKUP_S3_REGION   - S3 region
#   BACKUP_S3_ACCESS_KEY - S3 access key ID
#   BACKUP_S3_SECRET_KEY - S3 secret access key
#   APP_NAME           - Application name (used for log path in Alloy config)

set -euo pipefail

# --- Load common helpers (env, run_remote, scp_to_remote, log) ---
source "$(dirname "$0")/lib/common.sh"

# --- Pinned versions ---
DOKKU_VERSION="${DOKKU_VERSION:-0.38.5}"

# --- Validate required vars ---
require_vars \
  SSH_HOST DOKKU_DOMAIN \
  GHCR_USER GHCR_TOKEN \
  GRAFANA_LOKI_URL COCKPIT_TOKEN \
  BACKUP_S3_BUCKET BACKUP_S3_ENDPOINT BACKUP_S3_REGION \
  BACKUP_S3_ACCESS_KEY BACKUP_S3_SECRET_KEY \
  APP_NAME

log "=== provision-server.sh starting (host: ${SSH_HOST}) ==="

# ---------------------------------------------------------------------------
# Render templates locally (never pass secrets over command-line arguments)
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TMP_ALLOY=$(mktemp /tmp/alloy-config.XXXXXX.alloy)
TMP_AWS_CONFIG=$(mktemp /tmp/aws-config.XXXXXX)
TMP_AWS_CREDS=$(mktemp /tmp/aws-creds.XXXXXX)
TMP_DOCKER_CFG=$(mktemp /tmp/docker-config.XXXXXX.json)

# Ensure temp files are cleaned up even on error
cleanup() {
  rm -f "$TMP_ALLOY" "$TMP_AWS_CONFIG" "$TMP_AWS_CREDS" "$TMP_DOCKER_CFG"
}
trap cleanup EXIT

# Alloy config (uses GRAFANA_LOKI_URL, COCKPIT_TOKEN, APP_NAME — already exported)
envsubst < "${SCRIPT_DIR}/lib/alloy-config.alloy.tmpl" > "$TMP_ALLOY"

# AWS config (no secrets)
cat > "$TMP_AWS_CONFIG" <<EOF
[default]
s3 =
  endpoint_url = ${BACKUP_S3_ENDPOINT}
region = ${BACKUP_S3_REGION}
EOF

# AWS credentials (secrets — written to temp file, never echoed)
cat > "$TMP_AWS_CREDS" <<EOF
[default]
aws_access_key_id = ${BACKUP_S3_ACCESS_KEY}
aws_secret_access_key = ${BACKUP_S3_SECRET_KEY}
EOF
chmod 600 "$TMP_AWS_CREDS"

# GHCR docker config.json (base64-encoded credentials)
GHCR_AUTH=$(printf '%s:%s' "$GHCR_USER" "$GHCR_TOKEN" | base64)
cat > "$TMP_DOCKER_CFG" <<EOF
{
  "auths": {
    "ghcr.io": {
      "auth": "${GHCR_AUTH}"
    }
  }
}
EOF
chmod 600 "$TMP_DOCKER_CFG"

# ---------------------------------------------------------------------------
# 1. System preparation
# ---------------------------------------------------------------------------
log "--- 1. System preparation ---"
run_remote bash <<'REMOTE'
set -euo pipefail
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get upgrade -y -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
  curl git ca-certificates gnupg sqlite3 awscli
# NTP
systemctl enable --now systemd-timesyncd
REMOTE

# ---------------------------------------------------------------------------
# 2. Dokku installation
# ---------------------------------------------------------------------------
log "--- 2. Dokku installation (version: ${DOKKU_VERSION}) ---"
run_remote bash <<REMOTE
set -euo pipefail
if dokku version &>/dev/null; then
  echo "Dokku already installed: \$(dokku version)"
else
  curl -fsSL https://dokku.com/bootstrap.sh -o /tmp/dokku-bootstrap.sh
  chmod +x /tmp/dokku-bootstrap.sh
  DOKKU_TAG=v${DOKKU_VERSION} bash /tmp/dokku-bootstrap.sh
fi

# Set global domain (idempotent)
if ! dokku domains:report --global 2>&1 | grep -q "${DOKKU_DOMAIN}"; then
  dokku domains:set-global ${DOKKU_DOMAIN}
fi
REMOTE

# ---------------------------------------------------------------------------
# 3. Grafana Alloy installation & configuration
# ---------------------------------------------------------------------------
log "--- 3. Grafana Alloy ---"
run_remote bash <<'REMOTE'
set -euo pipefail
# Add Grafana APT repository if not already present
if ! apt-cache policy alloy 2>/dev/null | grep -q grafana; then
  mkdir -p /etc/apt/keyrings
  curl -fsSL https://apt.grafana.com/gpg.key -o /tmp/grafana-gpg.key
  gpg --dearmor -o /etc/apt/keyrings/grafana.gpg /tmp/grafana-gpg.key
  echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://apt.grafana.com stable main" \
    > /etc/apt/sources.list.d/grafana.list
  apt-get update -qq
fi
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq alloy
# Add alloy user to docker group for container log scraping
usermod -aG docker alloy || true
REMOTE

log "  Deploying Alloy config..."
scp_to_remote "$TMP_ALLOY" /etc/alloy/config.alloy
run_remote bash <<'REMOTE'
set -euo pipefail
chown root:alloy /etc/alloy/config.alloy
chmod 640 /etc/alloy/config.alloy
systemctl daemon-reload
systemctl enable alloy
systemctl restart alloy
REMOTE

# ---------------------------------------------------------------------------
# 4. AWS CLI configuration (shared by all per-app backup scripts)
# ---------------------------------------------------------------------------
log "--- 4. AWS CLI config ---"
run_remote mkdir -p /root/.aws
run_remote chmod 700 /root/.aws
scp_to_remote "$TMP_AWS_CONFIG" /root/.aws/config
scp_to_remote "$TMP_AWS_CREDS"  /root/.aws/credentials
run_remote bash <<'REMOTE'
set -euo pipefail
chmod 600 /root/.aws/config /root/.aws/credentials
REMOTE

# ---------------------------------------------------------------------------
# 5. Let's Encrypt plugin
# ---------------------------------------------------------------------------
log "--- 5. Let's Encrypt plugin ---"
run_remote bash <<'REMOTE'
set -euo pipefail
if ! dokku plugin:list | grep -q letsencrypt; then
  dokku plugin:install https://github.com/dokku/dokku-letsencrypt.git
fi
dokku letsencrypt:cron-job --add
REMOTE

# ---------------------------------------------------------------------------
# 6. GHCR docker login (write config directly as dokku user)
# ---------------------------------------------------------------------------
log "--- 6. GHCR docker credentials for dokku user ---"
run_remote mkdir -p /home/dokku/.docker
run_remote chmod 700 /home/dokku/.docker
scp_to_remote "$TMP_DOCKER_CFG" /home/dokku/.docker/config.json
run_remote bash <<'REMOTE'
set -euo pipefail
chown dokku:dokku /home/dokku/.docker/config.json
chmod 600 /home/dokku/.docker/config.json
REMOTE

log "=== provision-server.sh complete ==="
