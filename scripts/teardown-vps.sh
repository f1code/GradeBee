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
scw instance server terminate "$ID" zone="$ZONE" with-ip=true with-block=true
echo "Done."
