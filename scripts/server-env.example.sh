#!/usr/bin/env bash

# 复制为 scripts/server-env.sh 后按当前开发环境修改。
# server-env.sh 已被 .gitignore 忽略，并由相关 Make target 自动 source。
export PNPM=/absolute/path/to/pnpm

# 多人服务器上默认端口被占用时，只需在本机覆盖以下变量。
# run.sh、Backend、Frontend 代理和 Agentend 回调会使用同一个 Backend 端口。
export SERVER_PORT=28080
export API_PROXY_TARGET="http://127.0.0.1:${SERVER_PORT}"
export AGENTHUB_BACKEND_URL="http://127.0.0.1:${SERVER_PORT}"

# 可选：覆盖其他开发服务端口与启动健康检查超时。
# export FRONTEND_PORT=5173
# export AGENTEND_PORT=8001
# export START_TIMEOUT_SECONDS=30

if [[ ! -x "$PNPM" ]]; then
  echo "PNPM 指向的文件不存在或不可执行：$PNPM" >&2
  return 1
fi
