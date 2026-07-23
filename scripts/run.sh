#!/usr/bin/env bash
set -euo pipefail

# ── 服务定义 ──────────────────────────────────────
# name:port 格式（均为热重载模式）
SERVICES=(frontend:5173 backend:8080 agentend:8001)

# ── 日志目录 ──────────────────────────────────────
LOG_DIR="$(cd "$(dirname "$0")/.." && pwd)/logs"
mkdir -p "$LOG_DIR"

# ── 颜色 ──────────────────────────────────────────
GREEN='\033[32m'
RED='\033[31m'
RESET='\033[0m'

# ── 工具函数 ──────────────────────────────────────
name_of() { cut -d: -f1 <<< "$1"; }
port_of() { cut -d: -f2 <<< "$1"; }

# 取监听某端口的进程 PID。优先用 ss（netlink，~0.1s），回退 lsof（扫 /proc，~1.4s）。
# 注意：本函数在 set -e / pipefail 下运行，grep 未匹配会返回非零，必须显式吞掉，
# 否则「端口无监听」会被误判为脚本错误而中断。
pid_on_port() {
  local port=$1
  local out
  out=$(ss -ltnp 2>/dev/null | grep -E "[:.]${port}\b" | grep -oE 'pid=[0-9]+' | head -1 || true)
  if [ -n "$out" ]; then
    echo "${out#pid=}"
    return
  fi
  # ss 未安装 / 无权限 / 仍未拿到 PID 时回退到 lsof
  lsof -i ":$port" -sTCP:LISTEN -t 2>/dev/null | head -1 || true
}

# 判断端口是否在监听。只用 ss -ltn（不解析进程，~0.05s），
# 供 start/stop 的轮询循环高频调用；需要 PID 时再单独调 pid_on_port。
is_running() {
  local port
  port=$(port_of "$1")
  # 注意：不加 || true。所有调用点都在 if/while 条件上下文中，
  # set -e 在此处被禁用，grep 的真实退出码（0=监听/1=未监听）会被原样返回。
  ss -ltn 2>/dev/null | grep -qE "[:.]${port}\b"
}

# ── 启动单个服务 ──────────────────────────────────
start_service() {
  local entry=$1
  local name
  name=$(name_of "$entry")
  local port
  port=$(port_of "$entry")

  if is_running "$entry"; then
    echo "$name 已在运行 (port $port, PID $(pid_on_port "$port"))，跳过"
    return
  fi

  # 写入启动分隔线
  local log_file="$LOG_DIR/${name}.log"
  {
    echo ""
    echo "═══════════════════════════════════════════════════════"
    echo "  $name 启动 @ $(date '+%Y-%m-%d %H:%M:%S')"
    echo "═══════════════════════════════════════════════════════"
  } >> "$log_file"

  echo "启动 $name (port $port) ..."
  case "$name" in
    frontend)
      (cd frontend && exec pnpm dev) >> "$log_file" 2>&1 &
      ;;
    backend)
      (cd backend && exec ~/go/bin/air -c .air.toml) >> "$log_file" 2>&1 &
      ;;
    agentend)
      # Watch only handwritten source dirs. Excluding src/generated avoids
      # tearing down active SSE streams when contract generation touches codegen files.
      (
        cd agentend && exec uv run uvicorn src.app.main:app --reload --port "$port" \
          --reload-dir src/adapters \
          --reload-dir src/api \
          --reload-dir src/app \
          --reload-dir src/clients \
          --reload-dir src/orchestrator \
          --reload-dir src/preview \
          --reload-dir src/rules \
          --reload-dir src/schemas \
          --reload-dir src/session \
          --reload-dir src/skills \
          --reload-dir src/workspace
      ) >> "$log_file" 2>&1 &
      ;;
  esac

  # 等待端口就绪（最多 10 秒）
  local waited=0
  while [ $waited -lt 100 ] && ! is_running "$entry"; do
    sleep 0.1
    waited=$((waited + 1))
  done

  if is_running "$entry"; then
    echo "$name 启动成功 (PID $(pid_on_port "$port"))"
  else
    echo "$name 启动超时，请检查日志"
  fi
}

# ── 停止单个服务 ──────────────────────────────────
stop_service() {
  local entry=$1
  local name
  name=$(name_of "$entry")
  local port
  port=$(port_of "$entry")

  if ! is_running "$entry"; then
    echo "$name 未运行"
    return
  fi

  local pid
  pid=$(pid_on_port "$port")
  echo "停止 $name (PID $pid)"
  # 杀整个进程树（父进程及其子进程）
  kill -- -"$pid" 2>/dev/null || kill "$pid" 2>/dev/null || true

  # 清理残留的孤儿守护进程（air/uvicorn 热重载会留下不持有端口的僵尸进程）
  local reaper_cmd
  case "$name" in
    backend)  reaper_cmd="air -c .air.toml" ;;
    agentend) reaper_cmd="uvicorn src.app.main:app" ;;
    *)        reaper_cmd="" ;;
  esac
  if [ -n "$reaper_cmd" ]; then
    local orphan_pids
    orphan_pids=$(pgrep -f "$reaper_cmd" 2>/dev/null || true)
    if [ -n "$orphan_pids" ]; then
      echo "清理 $name 残留进程: $orphan_pids"
      echo "$orphan_pids" | xargs kill 2>/dev/null || true
    fi
  fi

  # 等待进程退出（最多 5 秒）
  local waited=0
  while [ $waited -lt 50 ] && is_running "$entry"; do
    sleep 0.1
    waited=$((waited + 1))
  done
  if is_running "$entry"; then
    pid=$(pid_on_port "$port")
    kill -9 "$pid" 2>/dev/null || true
  fi
  # 强制清理仍未退出的孤儿
  if [ -n "$reaper_cmd" ]; then
    orphan_pids=$(pgrep -f "$reaper_cmd" 2>/dev/null || true)
    if [ -n "$orphan_pids" ]; then
      echo "$orphan_pids" | xargs kill -9 2>/dev/null || true
    fi
  fi
}

# ── 状态表格 ──────────────────────────────────────
# 性能要点：pid_on_port 每次都要做一次端口扫描（ss/lsof ~0.5-1.4s），
# N 个端口串行调用会逐个卡顿。这里改为只做一次 ss 扫描并缓存结果，
# 再从缓存中提取每个端口的 PID，整体耗时从 ~4s 降到 ~0.5s。
show_status() {
  echo "┌──────────┬──────────┬──────┬─────────┐"
  echo "│ 服务     │ 状态     │ 端口 │ PID     │"
  echo "├──────────┼──────────┼──────┼─────────┤"

  local port_map
  port_map=$(ss -ltnp 2>/dev/null || true)

  for entry in "${SERVICES[@]}"; do
    local name
    name=$(name_of "$entry")
    local port
    port=$(port_of "$entry")
    local pid
    pid=$(printf '%s\n' "$port_map" | grep -E "[:.]${port}\b" | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2 || true)
    if [ -n "$pid" ]; then
      printf "│ %-8s │ ${GREEN}%-8s${RESET} │ %-4s │ %-7s │\n" "$name" "运行中" "$port" "$pid"
    else
      printf "│ %-8s │ ${RED}%-8s${RESET} │ %-4s │ %-7s │\n" "$name" "未运行" "$port" "-"
    fi
  done
  echo "└──────────┴──────────┴──────┴─────────┘"
}

# ── 入口 ──────────────────────────────────────────
cmd=${1:-help}
target=${2:-}

# 根据 name 查找完整 entry
find_entry() {
  for e in "${SERVICES[@]}"; do
    [ "$(name_of "$e")" = "$1" ] && echo "$e" && return
  done
  echo ""
}

case "$cmd" in
  start)
    if [ -z "$target" ]; then
      for e in "${SERVICES[@]}"; do start_service "$e"; done
    else
      entry=$(find_entry "$target")
      [ -z "$entry" ] && { echo "未知服务: $target"; exit 1; }
      start_service "$entry"
    fi
    ;;
  stop)
    if [ -n "$target" ]; then
      entry=$(find_entry "$target")
      [ -z "$entry" ] && { echo "未知服务: $target"; exit 1; }
      stop_service "$entry"
    else
      for e in "${SERVICES[@]}"; do stop_service "$e"; done
      echo "✓ 全部已停止"
    fi
    ;;
  restart)
    if [ -z "$target" ]; then
      for e in "${SERVICES[@]}"; do stop_service "$e"; done
      echo "✓ 全部已停止"
      for e in "${SERVICES[@]}"; do start_service "$e"; done
    else
      entry=$(find_entry "$target")
      [ -z "$entry" ] && { echo "未知服务: $target"; exit 1; }
      stop_service "$entry"
      start_service "$entry"
    fi
    ;;
  status)
    show_status
    ;;
  help|*)
    echo "用法: ./scripts/run.sh <start|stop|restart|status> [frontend|backend|agentend]"
    echo ""
    echo "  start    启动服务（不指定则全部启动）"
    echo "  stop     停止服务（不指定则全部停止）"
    echo "  restart  重启服务（不指定则全部重启）"
    echo "  status   查看三端运行状态"
    ;;
esac
