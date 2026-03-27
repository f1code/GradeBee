# VPS Provisioning Script

## Goal

Automate Scaleway STARDUST1-S creation and provisioning so the VPS is ready for `make deploy` with a single command. Replace the manual "VPS Setup" steps in `docs/deployment.md`.

## Prerequisites

- `scw` CLI installed and configured (`scw init`)
- SSH key registered in Scaleway account

## Proposed Changes

### New file: `scripts/cloud-init.yml`

Cloud-init user data that runs on first boot:

```yaml
#cloud-config
packages:
  - ca-certificates
  - curl
runcmd:
  - install -m 0755 -d /etc/apt/keyrings
  - curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  - chmod a+r /etc/apt/keyrings/docker.asc
  - echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" > /etc/apt/sources.list.d/docker.list
  - apt-get update
  - apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
  - systemctl enable docker
  - mkdir -p /opt/gradebee
```

### New file: `scripts/provision-vps.sh`

```bash
#!/bin/bash
set -euo pipefail

INSTANCE_TYPE="${INSTANCE_TYPE:-STARDUST1-S}"
INSTANCE_NAME="${INSTANCE_NAME:-gradebee}"
IMAGE="ubuntu_jammy"
ZONE="${ZONE:-fr-par-1}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "Creating Scaleway $INSTANCE_TYPE instance..."
OUTPUT=$(scw instance server create \
  type="$INSTANCE_TYPE" \
  image="$IMAGE" \
  name="$INSTANCE_NAME" \
  zone="$ZONE" \
  cloud-init=@"$SCRIPT_DIR/cloud-init.yml" \
  --output json)

IP=$(echo "$OUTPUT" | jq -r '.public_ip.address')
ID=$(echo "$OUTPUT" | jq -r '.id')

echo ""
echo "Instance created:"
echo "  ID:   $ID"
echo "  IP:   $IP"
echo "  Zone: $ZONE"
echo ""
echo "Next steps:"
echo "  1. Point DNS A record to $IP"
echo "  2. Wait ~2 min for cloud-init to finish"
echo "  3. Copy .env to VPS:  scp .env.production root@${IP}:/opt/gradebee/.env"
echo "  4. Set VPS_HOST:      export VPS_HOST=root@${IP}"
echo "  5. Deploy:            make deploy"
```

### Edit: `Makefile`

Add `provision` target:

```makefile
provision:
	bash scripts/provision-vps.sh

teardown:
	bash scripts/teardown-vps.sh
```

### New file: `scripts/teardown-vps.sh`

```bash
#!/bin/bash
set -euo pipefail

INSTANCE_NAME="${INSTANCE_NAME:-gradebee}"
ZONE="${ZONE:-fr-par-1}"

ID=$(scw instance server list zone="$ZONE" name="$INSTANCE_NAME" --output json | jq -r '.[0].id')

if [ "$ID" = "null" ] || [ -z "$ID" ]; then
  echo "No instance named '$INSTANCE_NAME' found in $ZONE"
  exit 1
fi

echo "Terminating instance $INSTANCE_NAME ($ID)..."
scw instance server terminate "$ID" zone="$ZONE" with-ip=true with-block=local
echo "Done."
```

### Edit: `.env.example`

Add comment for `INSTANCE_TYPE` / `ZONE` (optional overrides):

```
# VPS provisioning (optional overrides for scripts/provision-vps.sh)
# INSTANCE_TYPE=STARDUST1-S
# ZONE=fr-par-1
```

### Edit: `docs/deployment.md`

Replace the manual "VPS Setup (one-time)" section with:

```markdown
## VPS Setup (one-time)

Requires [Scaleway CLI](https://www.scaleway.com/en/cli/) configured (`scw init`).

    make provision

This creates a STARDUST1-S instance with Docker pre-installed via cloud-init.
The script outputs the IP and next steps (DNS, .env, first deploy).

To tear down:

    scw instance server terminate <instance-id> with-ip=true with-block=local
```

Keep the rest of deployment.md as-is.

## Open Questions

None — DNS stays at AWS Route 53 (just set an A record for the domain to the VPS IP).
