#!/usr/bin/env bash
# Docker 部署启动前校验
# - 配置文件缺失 / Docker 未就绪 → 阻断（error）
# - 配置仍为默认值 → 仅提醒（warning），不阻断启动
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DOCKER_DIR="$(dirname "$SCRIPT_DIR")"
PROJECT_DIR="$(dirname "$DOCKER_DIR")"

RED='\033[31m'
GREEN='\033[32m'
YELLOW='\033[33m'
BOLD='\033[1m'
RESET='\033[0m'

errors=0
warnings=0

echo -e "${BOLD}=== AgentHub Docker 部署校验 ===${RESET}"
echo ""

# ──────────────────────────────────────
# 1. 检查配置文件是否存在（阻断）
# ──────────────────────────────────────

BACKEND_CONFIG="$DOCKER_DIR/configs/backend/config.yaml"
BACKEND_ENV="$DOCKER_DIR/configs/backend/.env"
COMPOSE_ENV="$DOCKER_DIR/.env"
AGENTEND_ENV="$PROJECT_DIR/agentend/.env"

echo -e "${BOLD}[1/3] 检查配置文件${RESET}"

for cfg in "$BACKEND_CONFIG" "$BACKEND_ENV"; do
    name=$(basename "$(dirname "$cfg")")/$(basename "$cfg")
    if [ ! -f "$cfg" ]; then
        echo -e "  ${RED}✗ $name 不存在${RESET}"
        errors=$((errors + 1))
    else
        echo -e "  ${GREEN}✓ $name${RESET}"
    fi
done

if [ ! -f "$COMPOSE_ENV" ]; then
    echo -e "  ${YELLOW}⚠ docker/.env 不存在，将使用 Compose 默认插值（生产不安全）${RESET}"
    warnings=$((warnings + 1))
else
    echo -e "  ${GREEN}✓ docker/.env${RESET}"
fi

# agentend 不在 Docker 内，仅检查宿主机 .env 是否存在
if [ ! -f "$AGENTEND_ENV" ]; then
    echo -e "  ${YELLOW}⚠ agentend/.env 不存在${RESET}"
    echo -e "      提示：cp agentend/.env.example agentend/.env，然后填入 DS_API_KEY"
    warnings=$((warnings + 1))
else
    echo -e "  ${GREEN}✓ agentend/.env${RESET}"
fi

# ──────────────────────────────────────
# 2. 检查配置安全性（仅提醒，不阻断）
# ──────────────────────────────────────

echo ""
echo -e "${BOLD}[2/3] 检查配置安全性${RESET}"

check_yaml_section_value() {
    local file="$1"
    local section="$2"
    local key="$3"
    local bad_value="$4"
    local label="$5"

    if [ ! -f "$file" ]; then
        return
    fi

    if awk -v section="$section" -v key="$key" -v bad_value="$bad_value" '
        $0 ~ "^[[:space:]]*" section ":" { in_section = 1; next }
        in_section && $0 ~ "^[^[:space:]#].*:" { in_section = 0 }
        in_section && $0 ~ "^[[:space:]]*" key ":" && index($0, bad_value) > 0 { found = 1 }
        END { exit found ? 0 : 1 }
    ' "$file" 2>/dev/null; then
        echo -e "  ${YELLOW}⚠ $label 仍为默认值 ($bad_value)${RESET}"
        warnings=$((warnings + 1))
    else
        echo -e "  ${GREEN}✓ $label${RESET}"
    fi
}

check_yaml_section_value "$BACKEND_CONFIG" "mysql" "password" "123456" "backend MySQL 密码"
check_yaml_section_value "$BACKEND_CONFIG" "jwt" "secret" "agenthub-demo-secret" "backend JWT 密钥"
check_yaml_section_value "$BACKEND_CONFIG" "admin" "password" "123456" "backend Admin 密码"

compose_env_value() {
    local key="$1"
    if [ -f "$COMPOSE_ENV" ]; then
        sed -n "s/^${key}=\(.*\)$/\1/p" "$COMPOSE_ENV" | head -n 1 | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"
    fi
}

skill_storage_enabled="${SKILL_STORAGE_ENABLED:-}"
if [ -z "$skill_storage_enabled" ]; then
    skill_storage_enabled=$(compose_env_value SKILL_STORAGE_ENABLED)
fi
if [ -z "$skill_storage_enabled" ]; then
    # Compose injects this value into Backend with a false default, so a
    # backend YAML value cannot override it when docker/.env is absent.
    skill_storage_enabled=false
fi
skill_storage_enabled=$(printf '%s' "$skill_storage_enabled" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
if [ "$skill_storage_enabled" != "true" ] && [ "$skill_storage_enabled" != "false" ]; then
    echo -e "  ${RED}✗ SKILL_STORAGE_ENABLED 无效：$skill_storage_enabled（只能是 true 或 false）${RESET}"
    errors=$((errors + 1))
fi

env_file_value_present() {
    local key="$1"
    [ -f "$BACKEND_ENV" ] && grep -Eq "^${key}=(\"[^\"]+\"|'[^']+'|[^#[:space:]]+)" "$BACKEND_ENV"
}

if [ "$skill_storage_enabled" = "true" ]; then
    minio_access_configured=false
    minio_secret_configured=false
    if [ -n "${MINIO_ACCESS_KEY:-}" ] || env_file_value_present "MINIO_ACCESS_KEY"; then
        minio_access_configured=true
    fi
    if [ -n "${MINIO_SECRET_KEY:-}" ] || env_file_value_present "MINIO_SECRET_KEY"; then
        minio_secret_configured=true
    fi
    if [ "$minio_access_configured" != true ] || [ "$minio_secret_configured" != true ]; then
        echo -e "  ${RED}✗ Skill MinIO 已启用，但 MINIO_ACCESS_KEY/MINIO_SECRET_KEY 未配置${RESET}"
        errors=$((errors + 1))
    else
        echo -e "  ${GREEN}✓ Skill MinIO 应用凭据${RESET}"
    fi
fi

artifact_storage_enabled="${ARTIFACT_STORAGE_ENABLED:-}"
if [ -z "$artifact_storage_enabled" ]; then
    artifact_storage_enabled=$(compose_env_value ARTIFACT_STORAGE_ENABLED)
fi
if [ -z "$artifact_storage_enabled" ]; then
    artifact_storage_enabled=false
fi
artifact_storage_enabled=$(printf '%s' "$artifact_storage_enabled" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
if [ "$artifact_storage_enabled" != "true" ] && [ "$artifact_storage_enabled" != "false" ]; then
    echo -e "  ${RED}✗ ARTIFACT_STORAGE_ENABLED 无效：$artifact_storage_enabled（只能是 true 或 false）${RESET}"
    errors=$((errors + 1))
fi
if [ "$artifact_storage_enabled" = "true" ]; then
    artifact_access_configured=false
    artifact_secret_configured=false
    artifact_capability_configured=false
    if [ -n "${ARTIFACT_MINIO_ACCESS_KEY:-}" ] || env_file_value_present "ARTIFACT_MINIO_ACCESS_KEY"; then
        artifact_access_configured=true
    fi
    if [ -n "${ARTIFACT_MINIO_SECRET_KEY:-}" ] || env_file_value_present "ARTIFACT_MINIO_SECRET_KEY"; then
        artifact_secret_configured=true
    fi
    if [ -n "${ARTIFACT_CAPABILITY_SECRET:-}" ] || env_file_value_present "ARTIFACT_CAPABILITY_SECRET"; then
        artifact_capability_configured=true
    fi
    if [ "$artifact_access_configured" != true ] || [ "$artifact_secret_configured" != true ]; then
        echo -e "  ${RED}✗ Artifact MinIO 已启用，但 ARTIFACT_MINIO_ACCESS_KEY/ARTIFACT_MINIO_SECRET_KEY 未配置${RESET}"
        errors=$((errors + 1))
    fi
    if [ "$artifact_capability_configured" != true ]; then
        echo -e "  ${RED}✗ Artifact 已启用，但 ARTIFACT_CAPABILITY_SECRET 未配置${RESET}"
        errors=$((errors + 1))
    else
        echo -e "  ${GREEN}✓ Artifact MinIO 与 capability 凭据${RESET}"
    fi
fi

avatar_write_provider="${AVATAR_STORAGE_WRITE_PROVIDER:-}"
if [ -z "$avatar_write_provider" ]; then
    avatar_write_provider=$(compose_env_value AVATAR_STORAGE_WRITE_PROVIDER)
fi
if [ -z "$avatar_write_provider" ]; then
    # Compose sets minio explicitly by default and overrides env_file/YAML.
    avatar_write_provider=minio
fi
avatar_write_provider=$(printf '%s' "$avatar_write_provider" | tr '[:upper:]' '[:lower:]' | sed 's/[[:space:]]*#.*$//' | tr -d '[:space:]')
avatar_minio_enabled="${ASSET_MINIO_ENABLED:-}"
if [ -z "$avatar_minio_enabled" ]; then
    avatar_minio_enabled=$(compose_env_value ASSET_MINIO_ENABLED)
fi
if [ -z "$avatar_minio_enabled" ]; then
    # Compose defaults Asset MinIO to enabled; backend YAML/env_file values
    # are overridden unless the operator sets docker/.env or the shell env.
    avatar_minio_enabled=true
fi
avatar_minio_enabled=$(printf '%s' "$avatar_minio_enabled" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')

local_storage_enabled=$(env_file_value_present "LOCAL_STORAGE_ENABLED" && sed -n 's/^LOCAL_STORAGE_ENABLED=\(.*\)$/\1/p' "$BACKEND_ENV" | head -n 1 | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//" || true)
if [ -z "$local_storage_enabled" ]; then
    local_storage_enabled=$(awk '
    $0 ~ /^[[:space:]]*storage:[[:space:]]*$/ { in_storage = 1; in_local = 0; next }
    in_storage && $0 ~ /^[^[:space:]#].*:/ { in_storage = 0; in_local = 0 }
    in_storage && $0 ~ /^[[:space:]][[:space:]]local:[[:space:]]*$/ { in_local = 1; next }
    in_storage && in_local && $0 ~ /^[[:space:]][[:space:]][^[:space:]#].*:/ { in_local = 0 }
    in_storage && in_local && $0 ~ /^[[:space:]][[:space:]][[:space:]][[:space:]]enabled:[[:space:]]*/ {
        sub(/^[^:]*:[[:space:]]*/, "")
        print
        exit
    }
    ' "$BACKEND_CONFIG" 2>/dev/null || true)
fi
local_storage_enabled=$(printf '%s' "${local_storage_enabled:-false}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
if [ "$avatar_minio_enabled" != "true" ] && [ "$avatar_minio_enabled" != "false" ]; then
    echo -e "  ${RED}✗ ASSET_MINIO_ENABLED 无效：$avatar_minio_enabled（只能是 true 或 false）${RESET}"
    errors=$((errors + 1))
fi
if [ "$local_storage_enabled" != "true" ] && [ "$local_storage_enabled" != "false" ]; then
    echo -e "  ${RED}✗ LOCAL_STORAGE_ENABLED 无效：$local_storage_enabled（只能是 true 或 false）${RESET}"
    errors=$((errors + 1))
fi

if [ "$avatar_write_provider" != "minio" ] && [ "$avatar_write_provider" != "local" ]; then
    echo -e "  ${RED}✗ Avatar write provider 无效：$avatar_write_provider（只能是 minio 或 local）${RESET}"
    errors=$((errors + 1))
fi
if [ "$avatar_write_provider" = "minio" ] && [ "$avatar_minio_enabled" != "true" ]; then
    echo -e "  ${RED}✗ Avatar 使用 MinIO 写入，但 ASSET_MINIO_ENABLED 未启用${RESET}"
    errors=$((errors + 1))
fi
if [ "$avatar_write_provider" = "local" ] && [ "$local_storage_enabled" != "true" ]; then
    echo -e "  ${RED}✗ Avatar 使用本地写入，但 LOCAL_STORAGE_ENABLED 未启用${RESET}"
    errors=$((errors + 1))
fi
local_storage_volume_mode="${LOCAL_STORAGE_VOLUME_MODE:-}"
if [ -z "$local_storage_volume_mode" ]; then
    local_storage_volume_mode=$(compose_env_value LOCAL_STORAGE_VOLUME_MODE)
fi
local_storage_volume_mode=$(printf '%s' "${local_storage_volume_mode:-ro}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
if [ "$local_storage_volume_mode" != "ro" ] && [ "$local_storage_volume_mode" != "rw" ]; then
    echo -e "  ${RED}✗ LOCAL_STORAGE_VOLUME_MODE 无效：$local_storage_volume_mode（只能是 ro 或 rw）${RESET}"
    errors=$((errors + 1))
fi
if [ "$avatar_write_provider" = "local" ] && [ "$local_storage_volume_mode" != "rw" ]; then
    echo -e "  ${RED}✗ Avatar 使用本地写入时，LOCAL_STORAGE_VOLUME_MODE 必须为 rw${RESET}"
    errors=$((errors + 1))
fi
if [ "$avatar_minio_enabled" = "true" ]; then
    asset_access_configured=false
    asset_secret_configured=false
    if [ -n "${ASSET_MINIO_ACCESS_KEY:-}" ] || env_file_value_present "ASSET_MINIO_ACCESS_KEY"; then
        asset_access_configured=true
    fi
    if [ -n "${ASSET_MINIO_SECRET_KEY:-}" ] || env_file_value_present "ASSET_MINIO_SECRET_KEY"; then
        asset_secret_configured=true
    fi
    if [ "$asset_access_configured" != true ] || [ "$asset_secret_configured" != true ]; then
        echo -e "  ${RED}✗ Avatar MinIO 已启用，但 ASSET_MINIO_ACCESS_KEY/ASSET_MINIO_SECRET_KEY 未配置${RESET}"
        errors=$((errors + 1))
    else
        echo -e "  ${GREEN}✓ Avatar MinIO 应用凭据${RESET}"
    fi
fi
minio_root_password="${MINIO_ROOT_PASSWORD:-$(compose_env_value MINIO_ROOT_PASSWORD)}"
minio_root_password="${minio_root_password:-change-me-minio-secret}"
if [ "$minio_root_password" = "change-me-minio-secret" ]; then
    echo -e "  ${YELLOW}⚠ MinIO Root 密码仍为默认值（生产必须修改）${RESET}"
    warnings=$((warnings + 1))
fi

# agentend DS_API_KEY 检查
if [ -f "$AGENTEND_ENV" ]; then
    if grep -q "DS_API_KEY=sk-CHANGE_ME" "$AGENTEND_ENV" 2>/dev/null || \
       ! grep -q "DS_API_KEY=." "$AGENTEND_ENV" 2>/dev/null; then
        echo -e "  ${YELLOW}⚠ agentend DS_API_KEY 未配置${RESET}"
        warnings=$((warnings + 1))
    else
        echo -e "  ${GREEN}✓ agentend DS_API_KEY${RESET}"
    fi
fi

# ──────────────────────────────────────
# 3. 检查 Docker 环境（阻断）
# ──────────────────────────────────────

echo ""
echo -e "${BOLD}[3/3] 检查 Docker 环境${RESET}"

if ! command -v docker &>/dev/null; then
    echo -e "  ${RED}✗ docker 未安装${RESET}"
    errors=$((errors + 1))
elif ! docker info &>/dev/null 2>&1; then
    echo -e "  ${RED}✗ Docker 未运行${RESET}"
    errors=$((errors + 1))
else
    echo -e "  ${GREEN}✓ Docker 已运行${RESET}"
fi

if command -v docker &>/dev/null && docker compose version &>/dev/null 2>&1; then
    echo -e "  ${GREEN}✓ docker compose 可用${RESET}"
else
    echo -e "  ${RED}✗ docker compose 不可用${RESET}"
    errors=$((errors + 1))
fi

# ──────────────────────────────────────
# 汇总
# ──────────────────────────────────────

echo ""
echo "================================"

if [ $errors -eq 0 ]; then
    if [ $warnings -gt 0 ]; then
        echo -e "${YELLOW}校验通过，$warnings 个提醒${RESET}"
        echo ""
        echo "  需要关注的配置文件:"
        echo "    docker/configs/backend/config.yaml    → MySQL 密码、JWT 密钥、Admin 密码"
        echo "    agentend/.env                         → DS_API_KEY（LLM 密钥）"
    else
        echo -e "${GREEN}校验通过${RESET}"
    fi
    echo ""
    echo "Docker 启动后，运行 agentend:"
    echo "  cd agentend && uv sync && cd .."
    echo "  make run-agentend"
    echo ""
    echo -n "是否继续启动 Docker？[y/N] "
    read -r confirm
    if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
        echo "已取消。"
        exit 1
    fi
    exit 0
else
    echo -e "${RED}$errors 个错误，请修复后再启动。${RESET}"
    exit 1
fi
