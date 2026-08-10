#!/bin/sh
set -eu

: "${MINIO_ENDPOINT:?set MINIO_ENDPOINT}"
: "${MINIO_ACCESS_KEY:?set MINIO_ACCESS_KEY}"
: "${MINIO_SECRET_KEY:?set MINIO_SECRET_KEY}"
: "${MINIO_BUCKET:=skill-packages}"
: "${BACKUP_DIR:?set BACKUP_DIR to a dedicated backup directory}"
: "${MYSQL_SNAPSHOT_ID:?set MYSQL_SNAPSHOT_ID to the matching database snapshot identifier}"

alias_name="agenthub-backup"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
target="${BACKUP_DIR}/${MINIO_BUCKET}/${stamp}"
mkdir -p "$target"
mc alias set "$alias_name" "$MINIO_ENDPOINT" "$MINIO_ACCESS_KEY" "$MINIO_SECRET_KEY" >/dev/null
mc mirror --preserve "$alias_name/$MINIO_BUCKET" "$target/objects"
# Include version IDs when bucket versioning is enabled; the manifest is
# bound to the matching MySQL snapshot below for point-in-time recovery.
mc ls --recursive --versions --json "$alias_name/$MINIO_BUCKET" > "$target/manifest.json"
{
	printf '{"created_at":"%s","bucket":"%s","mysql_snapshot_id":"%s"}\n' "$stamp" "$MINIO_BUCKET" "$MYSQL_SNAPSHOT_ID"
} > "$target/checkpoint.json"
echo "Skill MinIO backup written to $target"
