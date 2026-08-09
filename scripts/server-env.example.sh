#!/usr/bin/env bash

# 复制为 scripts/server-env.sh 后按当前开发环境修改。
# server-env.sh 已被 .gitignore 忽略，并由相关 Make target 自动 source。
export PNPM=/absolute/path/to/pnpm

if [[ ! -x "$PNPM" ]]; then
  echo "PNPM 指向的文件不存在或不可执行：$PNPM" >&2
  return 1
fi
