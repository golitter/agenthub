#!/usr/bin/env bash
#
# docs-sync —— 定时文档同步 + 自动提交工作流（单一闭环任务）
#
# 流程：
#   阶段 0    前置检查：必须在 main 分支、工作区干净（全部已提交）
#   阶段 0.5  切到工作分支 WORK_BRANCH（默认 auto-docs-update）
#   阶段 1    /doc-linter        全项目 docs/ 文档同步精炼（直接改文件）
#   阶段 2    /agentsmd-linter   全项目 AGENTS.md 同步精炼（--resume 共享会话）
#   阶段 3    /autogit           若产生了文档变更，按项目规范生成中文 commit 并提交
#   阶段 4    合并 WORK_BRANCH → main，删除工作分支，最后停留在 main
#
# 任一 skill 失败即中止；前置检查失败直接退出，不触碰任何文档。
# 三阶段共用同一 session：阶段1 由 CLI 生成并捕获，阶段2/3 用 --resume 复用。
#
set -euo pipefail

# ============ 可配置参数（环境变量 / .env 可覆盖） ============
# CLAUDE_HOME（claude binary 完整路径）见同目录 .env

# 工具白名单。skill 要扫描+改文档+git 提交，故包含 Glob/Grep/Edit/Bash。
ALLOWED_TOOLS="${ALLOWED_TOOLS:-Read,Edit,Write,Glob,Grep,Bash}"

# 每个会话最大轮次（全项目 lint 较重，按需调大）
MAX_TURNS="${MAX_TURNS:-5000}"

# 单会话超时（秒），0 = 不限（GNU timeout 语义）
PER_SESSION_TIMEOUT="${PER_SESSION_TIMEOUT:-10000}"

# 前置检查要求的分支（也是最终合并目标）
REQUIRE_BRANCH="${REQUIRE_BRANCH:-main}"

# 工作分支：文档同步在此分支进行，完成后合并回 REQUIRE_BRANCH
WORK_BRANCH="${WORK_BRANCH:-auto-docs-update}"
# =====================================================

# 定位项目根：脚本位于 <root>/scripts/scheduler/docs-sync/run.sh
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$PROJECT_ROOT"

LOG_DIR="$PROJECT_ROOT/logs"
LOG_FILE="$LOG_DIR/docs-sync.log"
mkdir -p "$LOG_DIR"

log() { echo "[$(date '+%F %T')] $*" | tee -a "$LOG_FILE"; }

# 统一中止：skill 失败后工作区可能残留部分修改，提示用户手动处理
abort() {
  local br
  br="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?')"
  log "⚠️ 工作流中止（当前分支: $br）。残留改动用 'git restore .' 丢弃，或 'git checkout $REQUIRE_BRANCH' 切回。"
  exit "${1:-1}"
}

# 加载本地配置（CLAUDE_HOME 等）
ENV_FILE="$SCRIPT_DIR/.env"
if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

# 校验 claude binary：路径来自 .env / 环境变量 CLAUDE_HOME；未配或不可执行则报错退出
if [[ -z "${CLAUDE_HOME:-}" || ! -x "$CLAUDE_HOME" ]]; then
  log "❌ 找不到 claude binary：请在 .env 配置 CLAUDE_HOME（claude 可执行文件完整路径）" >&2
  exit 1
fi

# 工作区是否有未提交变更（含已修改/已暂存/未跟踪，已忽略文件不计）
has_changes() { [[ -n "$(git status --porcelain)" ]]; }

# 三阶段共用同一会话：首次由 CLI 生成 session_id 并捕获，后续用 --resume 复用。
# $1 = skill 名（不带斜杠）
SESSION_ID=""

run_skill() {
  local skill="$1" rc raw resume_args=()
  [[ -n "$SESSION_ID" ]] && resume_args=(--resume "$SESSION_ID")
  log "──── 阶段：/$skill（会话: ${SESSION_ID:-<新建>}）────"

  raw="$(mktemp)"
  set +e
  timeout "$PER_SESSION_TIMEOUT" "$CLAUDE_HOME" -p "/$skill" \
      --verbose --output-format stream-json \
      "${resume_args[@]}" \
      --dangerously-skip-permissions \
      --allowedTools "$ALLOWED_TOOLS" \
      --max-turns "$MAX_TURNS" \
      >"$raw" 2>&1
  rc=$?
  set -e
  cat "$raw" >> "$LOG_FILE"

  # 首次调用：从 init 消息捕获 session_id，供后续阶段 --resume
  if [[ -z "$SESSION_ID" ]]; then
    SESSION_ID="$(grep -aoE '"session_id"[[:space:]]*:[[:space:]]*"[^"]+"' "$raw" | head -1 | sed -E 's/.*:"([^"]+)".*/\1/')"
    if [[ -n "$SESSION_ID" ]]; then
      log "📌 session_id=$SESSION_ID（后续阶段复用此会话）"
    else
      log "⚠️ 未能捕获 session_id，后续阶段将以独立会话运行"
    fi
  fi
  rm -f "$raw"

  if   (( rc == 0 ));   then log "✅ /$skill 完成"
  elif (( rc == 124 )); then log "⏰ /$skill 超时（>${PER_SESSION_TIMEOUT}s）"; return 124
  else                       log "❌ /$skill 失败（退出码 $rc）"; return "$rc"
  fi
}

log "============ docs-sync 工作流开始 ============"
log "项目根: $PROJECT_ROOT | claude: $CLAUDE_HOME"
log "工具白名单: $ALLOWED_TOOLS | 每会话轮次: $MAX_TURNS | 超时: ${PER_SESSION_TIMEOUT}s"

# ---------- 阶段 0：前置 git 检查 ----------
log "──── 阶段0：前置检查 ────"
current_branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$current_branch" != "$REQUIRE_BRANCH" ]]; then
  log "❌ 当前分支为 '$current_branch'，需在 '$REQUIRE_BRANCH' 分支执行"
  abort 1
fi
if has_changes; then
  log "❌ 工作区不干净，请先提交所有变更："
  git status --short | tee -a "$LOG_FILE"
  abort 1
fi
log "✅ 前置检查通过（分支=$current_branch，工作区干净）"

# ---------- 阶段 0.5：切到工作分支（每次从最新 main 重建，保证同步且幂等）----------
log "──── 阶段0.5：切到工作分支 $WORK_BRANCH ────"
git checkout -B "$WORK_BRANCH" "$REQUIRE_BRANCH"
log "✅ 已基于 $REQUIRE_BRANCH 创建/重置工作分支 $WORK_BRANCH"

# ---------- 阶段 1：文档同步 ----------
run_skill "doc-linter" || abort $?

# ---------- 阶段 2：AGENTS.md 同步（--resume 共享会话）----------
run_skill "agentsmd-linter" || abort $?

# ---------- 阶段 3：自动提交 ----------
log "──── 阶段3：自动提交 ────"
if has_changes; then
  log "检测到文档变更，调用 /autogit 提交"
  run_skill "autogit" || abort $?
else
  log "无文档变更，跳过 /autogit"
fi

# ---------- 阶段 4：合并回 main ----------
log "──── 阶段4：合并 $WORK_BRANCH → $REQUIRE_BRANCH ────"
git checkout "$REQUIRE_BRANCH"
if ! git merge "$WORK_BRANCH"; then
  log "❌ 合并失败，请手动解决后 'git merge --continue'，再 'git branch -d $WORK_BRANCH'"
  abort 1
fi
git branch -d "$WORK_BRANCH"
log "✅ 已合并到 $REQUIRE_BRANCH 并删除工作分支，停留于 $REQUIRE_BRANCH"

log "============ docs-sync 工作流完成 ============"
