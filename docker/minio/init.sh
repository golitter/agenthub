#!/bin/sh
set -eu

alias_name=local
endpoint="http://minio:9000"

until mc alias set "$alias_name" "$endpoint" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null 2>&1; do
  sleep 2
done

mc ready "$alias_name" >/dev/null
mc mb --ignore-existing "$alias_name/$MINIO_BUCKET"
# Make the private-bucket invariant explicit even when an operator reuses an
# existing volume that was previously configured with an anonymous policy.
mc anonymous set none "$alias_name/$MINIO_BUCKET"
sed "s/skill-packages/${MINIO_BUCKET}/g" /config/skill-package-policy.json > /tmp/skill-package-policy.json
mc admin policy create "$alias_name" agenthub-skill-package /tmp/skill-package-policy.json >/dev/null 2>&1 || true

app_user="${MINIO_APP_USER:-${MINIO_ACCESS_KEY:-agenthub}}"
app_password="${MINIO_APP_PASSWORD:-${MINIO_SECRET_KEY:-change-me-agenthub-secret}}"

if ! mc admin user info "$alias_name" "$app_user" >/dev/null 2>&1; then
  mc admin user add "$alias_name" "$app_user" "$app_password"
fi
mc admin policy attach "$alias_name" agenthub-skill-package --user "$app_user"
