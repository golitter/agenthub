#!/bin/sh
set -eu

: "${MINIO_ENDPOINT:?set MINIO_ENDPOINT}"
: "${MINIO_ACCESS_KEY:?set MINIO_ACCESS_KEY}"
: "${MINIO_SECRET_KEY:?set MINIO_SECRET_KEY}"
: "${MINIO_BUCKET:=skill-packages}"
: "${BACKUP_OBJECTS_DIR:?set BACKUP_OBJECTS_DIR to a verified backup objects directory}"

alias_name="agenthub-restore"
mc alias set "$alias_name" "$MINIO_ENDPOINT" "$MINIO_ACCESS_KEY" "$MINIO_SECRET_KEY" >/dev/null
mc mirror --overwrite "$BACKUP_OBJECTS_DIR" "$alias_name/$MINIO_BUCKET"
echo "Skill MinIO objects restored; run make skill-reconcile ARGS=\"--verify\" before opening write traffic"
