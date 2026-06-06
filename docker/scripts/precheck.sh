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

# agentend 不在 Docker 内，仅检查宿主机 .env 是否存在
if [ ! -f "$AGENTEND_ENV" ]; then
    echo -e "  ${YELLOW}⚠ agentend/.env 不存在，make run-agentend 前需配置${RESET}"
    warnings=$((warnings + 1))
else
    echo -e "  ${GREEN}✓ agentend/.env${RESET}"
fi

# ──────────────────────────────────────
# 2. 检查配置安全性（仅提醒，不阻断）
# ──────────────────────────────────────

echo ""
echo -e "${BOLD}[2/3] 检查配置安全性${RESET}"

check_yaml_value() {
    local file="$1"
    local key="$2"
    local bad_value="$3"
    local label="$4"

    if [ ! -f "$file" ]; then
        return
    fi

    if grep -q "^[[:space:]]*${key}.*${bad_value}" "$file" 2>/dev/null; then
        echo -e "  ${YELLOW}⚠ $label 仍为默认值 ($bad_value)${RESET}"
        warnings=$((warnings + 1))
    else
        echo -e "  ${GREEN}✓ $label${RESET}"
    fi
}

check_yaml_value "$BACKEND_CONFIG" "password" '"123456"' "backend MySQL 密码"
check_yaml_value "$BACKEND_CONFIG" "secret" "agenthub-demo-secret" "backend JWT 密钥"
check_yaml_value "$BACKEND_CONFIG" "password.*123456" "123456" "backend Admin 密码"

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
