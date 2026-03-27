#!/bin/bash
set -euo pipefail

# --- Configuration ---
DB_PATH="${DB_PATH:-/opt/gradebee/data/gradebee.db}"
S3_BUCKET="${S3_BUCKET:-s3://gradebee-backups}"
BACKUP_RETENTION=30  # number of backups to keep

# --- Timestamp ---
TIMESTAMP=$(date -u +%Y%m%dT%H%M%Sz)
TMPFILE="/tmp/gradebee-${TIMESTAMP}.db"

echo "[${TIMESTAMP}] Starting backup..."

# --- Safe online backup (does not lock WAL writers) ---
sqlite3 "$DB_PATH" ".backup '${TMPFILE}'"
echo "  SQLite backup created: ${TMPFILE} ($(du -h "$TMPFILE" | cut -f1))"

# --- Upload to S3 ---
aws s3 cp "$TMPFILE" "${S3_BUCKET}/db/${TIMESTAMP}.db" --quiet
echo "  Uploaded to ${S3_BUCKET}/db/${TIMESTAMP}.db"

# --- Cleanup temp file ---
rm -f "$TMPFILE"

# --- Prune old backups (keep newest $BACKUP_RETENTION) ---
OBJECTS=$(aws s3 ls "${S3_BUCKET}/db/" | sort | awk '{print $4}')
COUNT=$(echo "$OBJECTS" | grep -c . || true)

if [ "$COUNT" -gt "$BACKUP_RETENTION" ]; then
    DELETE_COUNT=$((COUNT - BACKUP_RETENTION))
    echo "  Pruning ${DELETE_COUNT} old backup(s) (keeping ${BACKUP_RETENTION})..."
    echo "$OBJECTS" | head -n "$DELETE_COUNT" | while read -r obj; do
        aws s3 rm "${S3_BUCKET}/db/${obj}" --quiet
        echo "    Deleted ${obj}"
    done
fi

echo "[${TIMESTAMP}] Backup complete."
