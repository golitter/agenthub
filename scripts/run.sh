#!/usr/bin/env bash
set -euo pipefail

# ── 服务定义 ──────────────────────────────────────
# 端口可由 scripts/server-env.sh 覆盖；Backend 自身也读取 SERVER_PORT。
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
BACKEND_PORT="${SERVER_PORT:-8080}"
AGENTEND_PORT="${AGENTEND_PORT:-8001}"
START_TIMEOUT_SECONDS="${START_TIMEOUT_SECONDS:-30}"
SERVICES=(
  "frontend:$FRONTEND_PORT"
  "backend:$BACKEND_PORT"
  "agentend:$AGENTEND_PORT"
)

# ── 运行目录 ──────────────────────────────────────
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="$PROJECT_ROOT/logs"
PID_DIR="$LOG_DIR/pids"
FRONTEND_PNPM="${PNPM:-pnpm}"
mkdir -p "$LOG_DIR" "$PID_DIR"

# ── 颜色 ──────────────────────────────────────────
GREEN='\033[32m'
YELLOW='\033[33m'
RED='\033[31m'
RESET='\033[0m'

# ── 工具函数 ──────────────────────────────────────
name_of() { cut -d: -f1 <<< "$1"; }
port_of() { cut -d: -f2 <<< "$1"; }
pid_file_of() { echo "$PID_DIR/$1.pid"; }

health_url() {
  local name=$1
  local port=$2
  case "$name" in
    frontend) echo "http://127.0.0.1:$port/" ;;
    backend)  echo "http://127.0.0.1:$port/ping" ;;
    agentend) echo "http://127.0.0.1:$port/health/live" ;;
  esac
}

port_is_listening() {
  local port=$1
  ss -ltn 2>/dev/null | grep -qE "[:.]${port}\b"
}

service_is_healthy() {
  local entry=$1
  local name
  name=$(name_of "$entry")
  local port
  port=$(port_of "$entry")
  local body
  body=$(curl --fail --silent --show-error --max-time 1 "$(health_url "$name" "$port")" 2>/dev/null) || return 1
  case "$name" in
    frontend) [[ "$body" == *"<title>AgentHub"* ]] ;;
    backend)  [[ "$body" == *'"message":"pong"'* ]] ;;
    agentend) [[ "$body" == *'"status":"ok"'* ]] ;;
    *) return 1 ;;
  esac
}

# 输出 running / occupied / stopped。健康接口是服务身份的唯一判据；
# 只有端口监听但健康接口不匹配时，明确视为其他服务占用。
service_state() {
  local entry=$1
  local port
  port=$(port_of "$entry")
  if service_is_healthy "$entry"; then
    echo "running"
  elif port_is_listening "$port"; then
    echo "occupied"
  else
    echo "stopped"
  fi
}

# 取监听某端口的进程 PID。拿不到 PID 不影响服务状态判断。
pid_on_port() {
  local port=$1
  local out
  out=$(ss -ltnp 2>/dev/null | grep -E "[:.]${port}\b" | grep -oE 'pid=[0-9]+' | head -1 || true)
  if [ -n "$out" ]; then
    echo "${out#pid=}"
    return
  fi
  lsof -i ":$port" -sTCP:LISTEN -t 2>/dev/null | head -1 || true
}

process_start_time() {
  local pid=$1
  awk '{print $22}' "/proc/$pid/stat" 2>/dev/null || true
}

write_pid_file() {
  local name=$1
  local pid=$2
  local start_time
  start_time=$(process_start_time "$pid")
  [ -n "$start_time" ] || return 1
  printf '%s %s\n' "$pid" "$start_time" > "$(pid_file_of "$name")"
}

process_matches_service() {
  local name=$1
  local pid=$2
  local cwd
  cwd=$(readlink -f "/proc/$pid/cwd" 2>/dev/null || true)
  case "$name" in
    frontend) [[ "$cwd" == "$PROJECT_ROOT/frontend" ]] ;;
    backend)  [[ "$cwd" == "$PROJECT_ROOT/backend" ]] ;;
    agentend) [[ "$cwd" == "$PROJECT_ROOT/agentend" ]] ;;
    *) return 1 ;;
  esac
}

# 只返回由本脚本记录、仍是同一进程、属于当前用户且工作目录匹配的 PID。
managed_pid() {
  local name=$1
  local pid_file
  pid_file=$(pid_file_of "$name")
  [ -f "$pid_file" ] || return 1

  local pid recorded_start
  read -r pid recorded_start < "$pid_file" || return 1
  [[ "$pid" =~ ^[0-9]+$ && "$recorded_start" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  [ "$(process_start_time "$pid")" = "$recorded_start" ] || return 1
  [ "$(ps -o uid= -p "$pid" 2>/dev/null | tr -d ' ')" = "$(id -u)" ] || return 1
  process_matches_service "$name" "$pid" || return 1
  echo "$pid"
}

remove_pid_file() {
  local name=$1
  rm -f -- "$(pid_file_of "$name")"
}

show_log_tail() {
  local name=$1
  local log_file="$LOG_DIR/${name}.log"
  if [ -f "$log_file" ]; then
    echo "── ${name}.log 最后 20 行 ──"
    tail -n 20 "$log_file"
  fi
}

check_agentend_skills() {
  local missing=0
  for bin in \
    "$PROJECT_ROOT/agentend/src/skills/builtin/taskctl/taskctl" \
    "$PROJECT_ROOT/agentend/src/skills/builtin/render/render"
  do
    if [ ! -x "$bin" ]; then
      echo "缺少内置 skill CLI: $bin"
      missing=1
    fi
  done
  if [ "$missing" -ne 0 ]; then
    echo "请先在项目根目录运行: make skills build"
    exit 1
  fi
}

# ── 启动单个服务 ──────────────────────────────────
start_service() {
  local entry=$1
  local name
  name=$(name_of "$entry")
  local port
  port=$(port_of "$entry")
  local state
  state=$(service_state "$entry")

  if [ "$state" = "running" ]; then
    local running_pid
    running_pid=$(managed_pid "$name" 2>/dev/null || pid_on_port "$port")
    echo "$name 已在运行 (port $port, PID ${running_pid:-?})，跳过"
    return
  fi
  if [ "$state" = "occupied" ]; then
    local conflict_pid
    conflict_pid=$(pid_on_port "$port" || true)
    echo "$name 无法启动：端口 $port 已被其他服务占用（健康检查不匹配，PID ${conflict_pid:-?}）" >&2
    return 1
  fi

  remove_pid_file "$name"
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
      (cd frontend && exec setsid "$FRONTEND_PNPM" dev --port "$port") >> "$log_file" 2>&1 &
      ;;
    backend)
      local air_bin
      air_bin=$(command -v air 2>/dev/null || echo "$(go env GOPATH 2>/dev/null || echo "$HOME/go")/bin/air")
      (cd backend && exec setsid "$air_bin" -c .air.toml) >> "$log_file" 2>&1 &
      ;;
    agentend)
      check_agentend_skills
      # Watch only handwritten source dirs. Excluding src/generated avoids
      # tearing down active SSE streams when contract generation touches codegen files.
      (
        cd agentend && exec setsid uv run uvicorn src.app.main:app --reload --port "$port" \
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

  local launcher_pid=$!
  if ! write_pid_file "$name" "$launcher_pid"; then
    echo "$name 启动后无法记录 PID，请检查进程状态" >&2
    return 1
  fi

  local deadline=$((SECONDS + START_TIMEOUT_SECONDS))
  while (( SECONDS < deadline )); do
    if service_is_healthy "$entry"; then
      echo "$name 启动成功 (PID $launcher_pid)"
      return
    fi
    if ! kill -0 "$launcher_pid" 2>/dev/null; then
      echo "$name 启动失败：进程已退出" >&2
      remove_pid_file "$name"
      show_log_tail "$name"
      return 1
    fi
    sleep 0.2
  done

  echo "$name 在 ${START_TIMEOUT_SECONDS} 秒内未通过健康检查；进程仍在运行，未自动停止" >&2
  show_log_tail "$name"
  return 1
}

# ── 停止单个服务 ──────────────────────────────────
stop_service() {
  local entry=$1
  local name
  name=$(name_of "$entry")
  local port
  port=$(port_of "$entry")
  local pid
  pid=$(managed_pid "$name" 2>/dev/null || true)

  if [ -z "$pid" ]; then
    local state
    state=$(service_state "$entry")
    remove_pid_file "$name"
    case "$state" in
      stopped)
        echo "$name 未运行"
        ;;
      running)
        echo "$name 正在运行，但不是由当前 PID 文件管理；为避免误杀，拒绝停止 (port $port, PID $(pid_on_port "$port" || true))" >&2
        return 1
        ;;
      occupied)
        echo "$name 端口 $port 被其他服务占用；拒绝停止该服务" >&2
        return 1
        ;;
    esac
    return
  fi

  local pgid
  pgid=$(ps -o pgid= -p "$pid" 2>/dev/null | tr -d ' ')
  echo "停止 $name (PID $pid)"
  if [ "$pgid" = "$pid" ]; then
    kill -- -"$pid" 2>/dev/null || true
  else
    kill "$pid" 2>/dev/null || true
  fi

  local deadline=$((SECONDS + 5))
  while (( SECONDS < deadline )) && kill -0 "$pid" 2>/dev/null; do
    sleep 0.1
  done
  if kill -0 "$pid" 2>/dev/null; then
    if [ "$pgid" = "$pid" ]; then
      kill -KILL -- -"$pid" 2>/dev/null || true
    else
      kill -KILL "$pid" 2>/dev/null || true
    fi
  fi
  remove_pid_file "$name"
}

# ── 状态表格 ──────────────────────────────────────
show_status() {
  echo "┌──────────┬──────────────┬──────┬─────────┐"
  echo "│ 服务     │ 状态         │ 端口 │ PID     │"
  echo "├──────────┼──────────────┼──────┼─────────┤"

  for entry in "${SERVICES[@]}"; do
    local name
    name=$(name_of "$entry")
    local port
    port=$(port_of "$entry")
    local state
    state=$(service_state "$entry")
    local pid
    pid=$(managed_pid "$name" 2>/dev/null || pid_on_port "$port")
    pid=${pid:-?}
    case "$state" in
      running)
        printf "│ %-8s │ ${GREEN}%-12s${RESET} │ %-4s │ %-7s │\n" "$name" "运行中" "$port" "$pid"
        ;;
      occupied)
        printf "│ %-8s │ ${YELLOW}%-12s${RESET} │ %-4s │ %-7s │\n" "$name" "端口被占用" "$port" "$pid"
        ;;
      stopped)
        printf "│ %-8s │ ${RED}%-12s${RESET} │ %-4s │ %-7s │\n" "$name" "未运行" "$port" "-"
        ;;
    esac
  done
  echo "└──────────┴──────────────┴──────┴─────────┘"
}

# ── 入口 ──────────────────────────────────────────
cmd=${1:-help}
target=${2:-}

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
