#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"

if ! command -v uv >/dev/null 2>&1; then
  echo "缺少 uv，请先安装 uv。" >&2
  exit 1
fi

if [ -n "${PNPM:-}" ]; then
  if [ ! -x "$PNPM" ]; then
    echo "PNPM 指定的文件不存在或不可执行: $PNPM" >&2
    exit 1
  fi
  PNPM_COMMAND=("$PNPM")
elif command -v pnpm >/dev/null 2>&1; then
  PNPM_COMMAND=(pnpm)
else
  echo "缺少 pnpm，请先安装 pnpm 或通过 PNPM 指定可执行文件。" >&2
  exit 1
fi

port_busy() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | grep -qE "[:.]${port}\b"
  elif command -v lsof >/dev/null 2>&1; then
    lsof -i ":${port}" -sTCP:LISTEN -t >/dev/null 2>&1
  else
    return 1
  fi
}

for port in 5174 9100; do
  if port_busy "$port"; then
    echo "端口 ${port} 已被占用，配置中心未启动。" >&2
    exit 1
  fi
done

uv sync --directory "$ROOT" --locked

(
  cd "$ROOT/web"
  "${PNPM_COMMAND[@]}" install --frozen-lockfile
)

(
  cd "$ROOT"
  exec "$ROOT/.venv/bin/python" -m server.main
) &
API_PID=$!
WEB_PID=""
WEB_PROCESS_GROUP=0

terminate_tree() {
  local parent="$1"
  local child
  if command -v pgrep >/dev/null 2>&1; then
    while read -r child; do
      [ -n "$child" ] && terminate_tree "$child"
    done < <(pgrep -P "$parent" 2>/dev/null || true)
  fi
  kill "$parent" 2>/dev/null || true
}

cleanup() {
  if [ -n "$WEB_PID" ]; then
    if [ "$WEB_PROCESS_GROUP" -eq 1 ]; then
      kill -- -"$WEB_PID" 2>/dev/null || true
    else
      terminate_tree "$WEB_PID"
    fi
    wait "$WEB_PID" 2>/dev/null || true
  fi
  kill "$API_PID" 2>/dev/null || true
  wait "$API_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

api_ready=0
for _ in {1..40}; do
  if ! kill -0 "$API_PID" 2>/dev/null; then
    echo "配置中心 API 启动失败。" >&2
    wait "$API_PID" || true
    exit 1
  fi
  if "$ROOT/.venv/bin/python" -c 'import urllib.request; urllib.request.urlopen("http://127.0.0.1:9100/api/health", timeout=.25).read()' >/dev/null 2>&1; then
    api_ready=1
    break
  fi
  sleep 0.1
done

if [ "$api_ready" -ne 1 ]; then
  echo "配置中心 API 在 4 秒内未就绪。" >&2
  exit 1
fi

echo "配置中心：http://127.0.0.1:5174"
if command -v setsid >/dev/null 2>&1; then
  (
    cd "$ROOT/web"
    exec setsid "${PNPM_COMMAND[@]}" dev --host 127.0.0.1 --port 5174 --strictPort
  ) &
  WEB_PROCESS_GROUP=1
else
  (
    cd "$ROOT/web"
    exec "${PNPM_COMMAND[@]}" dev --host 127.0.0.1 --port 5174 --strictPort
  ) &
fi
WEB_PID=$!
wait "$WEB_PID"
