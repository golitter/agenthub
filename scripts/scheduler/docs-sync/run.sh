#!/usr/bin/env bash
#
# docs-sync —— 定时文档同步工作流
#
# 顺序执行两个【独立】的 Claude Code 会话：
#   阶段 1: /doc-linter        对全项目 docs/ 文档做同步精炼（直接改文件）
#   阶段 2: /agentsmd-linter   对全项目 AGENTS.md 做同步精炼（直接改文件）
#
# 阶段 2 在阶段 1 之后的新会话里运行，故能读到最新文档。
# 两个 skill 都会直接改文件，故需要 Edit/Write；全量扫描需要 Glob/Grep。
#
set -euo pipefail

# ============ 可配置参数（环境变量 / .env 可覆盖） ============
# CLAUDE_HOME（claude binary 完整路径）见同目录 .env

# 工具白名单。skill 要扫描 + 改文档，故包含 Glob/Grep/Edit。
ALLOWED_TOOLS="${ALLOWED_TOOLS:-Read,Edit,Write,Glob,Grep,Bash}"

# 每个会话最大轮次（全项目 lint 较重，按需调大）
MAX_TURNS="${MAX_TURNS:-50}"

# 单会话超时（秒），0 = 不限（GNU timeout 语义）
PER_SESSION_TIMEOUT="${PER_SESSION_TIMEOUT:-1800}"
# =====================================================

# 定位项目根：脚本位于 <root>/scripts/scheduler/docs-sync/run.sh
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$PROJECT_ROOT"

LOG_DIR="$PROJECT_ROOT/logs"
LOG_FILE="$LOG_DIR/docs-sync.log"
mkdir -p "$LOG_DIR"

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG_FILE"; }

# 加载本地配置（CLAUDE_HOME 等）；文件不存在则忽略，改用环境变量 / 兜底探测
ENV_FILE="$SCRIPT_DIR/.env"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

# 解析 claude binary：优先用 .env / 环境变量的 CLAUDE_HOME；
# 为空或失效（扩展升级版本号变）时，fallback 到通配符探测
CLAUDE_BIN="${CLAUDE_HOME:-}"
if [[ -z "$CLAUDE_BIN" || ! -x "$CLAUDE_BIN" ]]; then
  CLAUDE_BIN="$(ls /home/leixu/.vscode-server/extensions/anthropic.claude-code-*-linux-x64/resources/native-binary/claude 2>/dev/null | head -n1 || true)"
fi
if [[ -z "$CLAUDE_BIN" || ! -x "$CLAUDE_BIN" ]]; then
  log "❌ 找不到 claude binary（请在 .env 配置 CLAUDE_HOME）" >&2
  exit 1
fi

# 跑一个 skill 会话。$1 = skill 名（不带斜杠）
run_skill() {
  local skill="$1" rc
  log "──── 阶段：/$skill（独立会话）────"

  set +e
  timeout "$PER_SESSION_TIMEOUT" "$CLAUDE_BIN" -p "/$skill" \
      --verbose \
      --dangerously-skip-permissions \
      --allowedTools "$ALLOWED_TOOLS" \
      --max-turns "$MAX_TURNS" \
      2>&1 | tee -a "$LOG_FILE"
  rc=${PIPESTATUS[0]}
  set -e

  if   (( rc == 0 ));   then log "✅ /$skill 完成"
  elif (( rc == 124 )); then log "⏰ /$skill 超时（>${PER_SESSION_TIMEOUT}s），中止工作流"; return 124
  else                       log "❌ /$skill 失败（退出码 $rc），中止工作流"; return "$rc"
  fi
}

log "============ docs-sync 工作流开始 ============"
log "项目根: $PROJECT_ROOT"
log "claude: $CLAUDE_BIN"
log "工具白名单: $ALLOWED_TOOLS | 每会话轮次: $MAX_TURNS | 超时: ${PER_SESSION_TIMEOUT}s"

run_skill "doc-linter"      || exit $?
run_skill "agentsmd-linter" || exit $?

log "============ docs-sync 工作流完成 ============"
