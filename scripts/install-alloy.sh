#!/bin/bash
# install-alloy.sh — Install and configure Grafana Alloy on the VPS to ship
# Docker container logs and backup cron logs to Scaleway Cockpit (Loki).
#
# Required env vars (typically from .env):
#   VPS_HOST          — SSH target, e.g. root@1.2.3.4
#   COCKPIT_TOKEN     — Scaleway Cockpit push token (from terraform output)
#
# Run via: make setup-infra (which calls this script)
set -euo pipefail

VPS_HOST="${VPS_HOST:?VPS_HOST is required}"
COCKPIT_TOKEN="${COCKPIT_TOKEN:?COCKPIT_TOKEN is required}"
LOKI_URL="https://logs.cockpit.fr-par.scw.cloud/loki/api/v1/push"

echo "Installing Grafana Alloy on ${VPS_HOST}..."

# 1. Install Alloy package
ssh "$VPS_HOST" 'bash -s' <<'INSTALL_EOF'
set -euo pipefail
# Add Grafana APT repo if not already present
if [ ! -f /etc/apt/sources.list.d/grafana.list ]; then
    apt-get install -y apt-transport-https software-properties-common
    mkdir -p /etc/apt/keyrings/
    curl -fsSL https://apt.grafana.com/gpg.key | gpg --dearmor -o /etc/apt/keyrings/grafana.gpg
    echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://apt.grafana.com stable main" > /etc/apt/sources.list.d/grafana.list
fi
apt-get update
apt-get install -y alloy
INSTALL_EOF

# 2. Write Alloy config
ssh "$VPS_HOST" "cat > /etc/alloy/config.alloy" <<EOF
// --- Docker container logs ---
discovery.docker "containers" {
  host = "unix:///var/run/docker.sock"
}

loki.source.docker "docker_logs" {
  host       = "unix:///var/run/docker.sock"
  targets    = discovery.docker.containers.targets
  forward_to = [loki.write.cockpit.receiver]
  labels     = {
    job = "gradebee-docker",
  }
}

// --- Backup cron log ---
local.file_match "backup_log" {
  path_targets = [
    {__path__ = "/var/log/gradebee-backup.log"},
  ]
}

loki.source.file "backup" {
  targets    = local.file_match.backup_log.targets
  forward_to = [loki.write.cockpit.receiver]
  labels     = {
    job = "gradebee-backup",
  }
}

// --- Ship to Scaleway Cockpit ---
loki.write "cockpit" {
  endpoint {
    url = "${LOKI_URL}"

    headers = {
      "X-Token" = "${COCKPIT_TOKEN}",
    }
  }
}
EOF

# 3. Add alloy user to docker group so it can read container logs
ssh "$VPS_HOST" 'usermod -aG docker alloy 2>/dev/null || true'

# 4. Enable and restart Alloy
ssh "$VPS_HOST" 'systemctl enable alloy && systemctl restart alloy'

echo "Alloy installed and running. Logs shipping to Cockpit."
