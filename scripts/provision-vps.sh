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
