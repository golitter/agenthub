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
mc admin policy create "$alias_name" agenthub-skill-package /tmp/skill-package-policy.json >/dev/null

skill_storage_enabled=$(printf '%s' "${SKILL_STORAGE_ENABLED:-false}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
app_user=""
if [ "$skill_storage_enabled" = "true" ]; then
  : "${MINIO_ACCESS_KEY:?MINIO_ACCESS_KEY must be set when Skill storage is enabled}"
  : "${MINIO_SECRET_KEY:?MINIO_SECRET_KEY must be set when Skill storage is enabled}"
  # Keep initialization aligned with the exact credentials injected into
  # Backend; there is no second legacy app-user namespace.
  app_user="$MINIO_ACCESS_KEY"
  app_password="$MINIO_SECRET_KEY"
  if [ "${#app_password}" -lt 8 ]; then
    echo "Skill application secret must be at least 8 characters" >&2
    exit 1
  fi

  if [ "$app_user" = "$MINIO_ROOT_USER" ]; then
    echo "Skill application user must not be the MinIO root user" >&2
    exit 1
  fi

  # `mc admin user add` also updates the password for an existing built-in
  # user, keeping a rotated env-file secret in sync on every restart.
  mc admin user add "$alias_name" "$app_user" "$app_password"
  mc admin policy attach "$alias_name" agenthub-skill-package --user "$app_user"
else
  echo "Skill storage disabled; skipping Skill application user"
fi

artifact_storage_enabled=$(printf '%s' "${ARTIFACT_STORAGE_ENABLED:-false}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
artifact_user=""
artifact_bucket=""
if [ "$artifact_storage_enabled" = "true" ]; then
  : "${ARTIFACT_MINIO_ACCESS_KEY:?ARTIFACT_MINIO_ACCESS_KEY must be set when Artifact storage is enabled}"
  : "${ARTIFACT_MINIO_SECRET_KEY:?ARTIFACT_MINIO_SECRET_KEY must be set when Artifact storage is enabled}"
  artifact_bucket="${ARTIFACT_MINIO_BUCKET:-agenthub-artifacts}"
  artifact_user="$ARTIFACT_MINIO_ACCESS_KEY"
  artifact_password="$ARTIFACT_MINIO_SECRET_KEY"
  if [ "${#artifact_password}" -lt 8 ]; then
    echo "Artifact application secret must be at least 8 characters" >&2
    exit 1
  fi
  if [ "$artifact_user" = "$MINIO_ROOT_USER" ] || { [ -n "$app_user" ] && [ "$artifact_user" = "$app_user" ]; }; then
    echo "Artifact application user must be separate from the MinIO root and Skill users" >&2
    exit 1
  fi
  if [ "$artifact_bucket" = "$MINIO_BUCKET" ]; then
    echo "Artifact and Skill buckets must be different" >&2
    exit 1
  fi
  mc mb --ignore-existing "$alias_name/$artifact_bucket"
  mc anonymous set none "$alias_name/$artifact_bucket"
  sed "s/agenthub-artifacts/${artifact_bucket}/g" /config/artifact-policy.json > /tmp/artifact-policy.json
  mc admin policy create "$alias_name" agenthub-artifact /tmp/artifact-policy.json >/dev/null
  mc admin user add "$alias_name" "$artifact_user" "$artifact_password"
  mc admin policy attach "$alias_name" agenthub-artifact --user "$artifact_user"
else
  echo "Artifact storage disabled; skipping Artifact bucket and application user"
fi

asset_storage_enabled=$(printf '%s' "${ASSET_MINIO_ENABLED:-true}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
if [ "$asset_storage_enabled" != "true" ]; then
  echo "Avatar MinIO disabled; skipping Asset bucket and application user"
  exit 0
fi

asset_bucket="${ASSET_MINIO_BUCKET:-agenthub-assets}"
: "${ASSET_MINIO_ACCESS_KEY:?ASSET_MINIO_ACCESS_KEY must be set}"
: "${ASSET_MINIO_SECRET_KEY:?ASSET_MINIO_SECRET_KEY must be set}"
asset_user="$ASSET_MINIO_ACCESS_KEY"
asset_password="$ASSET_MINIO_SECRET_KEY"
if [ "${#asset_password}" -lt 8 ]; then
  echo "Asset application secret must be at least 8 characters" >&2
  exit 1
fi

if [ "$asset_bucket" = "$MINIO_BUCKET" ]; then
  echo "Asset and Skill buckets must be different" >&2
  exit 1
fi
if [ -n "$artifact_user" ] && [ "$asset_bucket" = "$artifact_bucket" ]; then
  echo "Asset and Artifact buckets must be different" >&2
  exit 1
fi
if [ "$asset_user" = "$MINIO_ROOT_USER" ] || { [ -n "$app_user" ] && [ "$asset_user" = "$app_user" ]; } || { [ -n "$artifact_user" ] && [ "$asset_user" = "$artifact_user" ]; }; then
  echo "Asset application user must be separate from the MinIO root and Skill users" >&2
  exit 1
fi

mc mb --ignore-existing "$alias_name/$asset_bucket"
mc anonymous set none "$alias_name/$asset_bucket"
sed "s/agenthub-assets/${asset_bucket}/g" /config/avatar-policy.json > /tmp/avatar-policy.json
mc admin policy create "$alias_name" agenthub-avatar /tmp/avatar-policy.json >/dev/null

mc admin user add "$alias_name" "$asset_user" "$asset_password"
mc admin policy attach "$alias_name" agenthub-avatar --user "$asset_user"
