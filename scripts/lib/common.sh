#!/usr/bin/env bash
# scripts/lib/common.sh — shared helpers for provision-server.sh and provision-app.sh
#
# Source this file at the top of each provisioning script:
#   source "$(dirname "$0")/lib/common.sh"
#
# After sourcing:
#   - All vars from .env.infra are exported
#   - require_vars <VAR1> <VAR2> ... validates required variables
#   - run_remote <cmd> executes a command on $SSH_HOST as $SSH_USER via SSH
#   - scp_to_remote <local> <remote> copies a file to the remote host

set -euo pipefail

# ---------------------------------------------------------------------------
# Locate repo root (scripts live under scripts/, so root is one level up)
# ---------------------------------------------------------------------------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# ---------------------------------------------------------------------------
# Load .env.infra
# ---------------------------------------------------------------------------
ENV_FILE="${ENV_FILE:-${REPO_ROOT}/.env.infra}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: env file not found: $ENV_FILE" >&2
  echo "       Copy .env.infra.example → .env.infra and fill in the values." >&2
  exit 1
fi

# Export all KEY=VALUE pairs (ignore blank lines and comments)
set -a
# shellcheck source=/dev/null
source "$ENV_FILE"
set +a

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
SSH_USER="${SSH_USER:-root}"
SSH_PORT="${SSH_PORT:-22}"

# ---------------------------------------------------------------------------
# Sudo handling
# ---------------------------------------------------------------------------
# When SSH_USER is root, SUDO is empty (no-op).  For any other user, SUDO is
# "sudo" so callers can write:
#   run_remote $SUDO some-command
# or use run_remote_sudo to wrap an entire heredoc in "sudo bash".
if [[ "${SSH_USER}" == "root" ]]; then
  SUDO=""
else
  SUDO="sudo"
fi
export SUDO

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# require_vars VAR1 VAR2 ...
# Exits with an error listing all missing variables (fail fast, not one-by-one).
require_vars() {
  local missing=()
  for var in "$@"; do
    if [[ -z "${!var:-}" ]]; then
      missing+=("$var")
    fi
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "ERROR: the following required variables are not set in $ENV_FILE:" >&2
    for v in "${missing[@]}"; do
      echo "  $v" >&2
    done
    exit 1
  fi
}

# run_remote CMD
# Executes CMD on the remote host over SSH.
# Secrets are never passed via command-line arguments — callers should use
# environment variables that are already set in the remote session via
# the remote heredoc, or pass them as explicit env vars prefixed on the call.
run_remote() {
  ssh -p "${SSH_PORT}" \
      -o StrictHostKeyChecking=accept-new \
      -o BatchMode=yes \
      "${SSH_USER}@${SSH_HOST}" \
      "$@"
}

# run_remote_sudo
# Like run_remote but wraps the piped heredoc in "sudo bash".
# Usage (note: must pipe a heredoc, same as run_remote bash <<'REMOTE'):
#   run_remote_sudo <<'REMOTE'
#   apt-get install -y foo
#   REMOTE
run_remote_sudo() {
  ssh -p "${SSH_PORT}" \
      -o StrictHostKeyChecking=accept-new \
      -o BatchMode=yes \
      "${SSH_USER}@${SSH_HOST}" \
      ${SUDO:+sudo} bash "$@"
}

# scp_to_remote LOCAL_FILE REMOTE_PATH
# Copies a local file to the remote host.
scp_to_remote() {
  local local_file="$1"
  local remote_path="$2"
  scp -P "${SSH_PORT}" \
      -o StrictHostKeyChecking=accept-new \
      -o BatchMode=yes \
      "$local_file" \
      "${SSH_USER}@${SSH_HOST}:${remote_path}"
}

# scp_to_remote_sudo LOCAL_FILE REMOTE_PATH [OWNER] [MODE]
# Copies a local file to a privileged remote path.
# Uploads to /tmp first, then uses sudo to move it into place.
# Optionally sets owner (e.g. "root:alloy") and mode (e.g. "640").
scp_to_remote_sudo() {
  local local_file="$1"
  local remote_path="$2"
  local owner="${3:-}"
  local mode="${4:-}"
  local tmp_file
  tmp_file="/tmp/provision_upload_$(basename "$remote_path").$$"
  scp -P "${SSH_PORT}" \
      -o StrictHostKeyChecking=accept-new \
      -o BatchMode=yes \
      "$local_file" \
      "${SSH_USER}@${SSH_HOST}:${tmp_file}"
  run_remote_sudo <<REMOTE
set -euo pipefail
mv "$tmp_file" "$remote_path"
${owner:+chown "$owner" "$remote_path"}
${mode:+chmod "$mode" "$remote_path"}
REMOTE
}

# log MSG
# Prints a timestamped info line to stdout.
log() {
  echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] $*"
}
