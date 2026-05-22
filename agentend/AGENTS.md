# AGENTS.md — agentend

基于 FastAPI 的 Agent Runtime 服务，桥接外部 Agent（Claude CLI / OpenCode CLI），提供会话管理、规则引擎、工作区隔离和技能供给。Python >=3.10，包管理 uv，代码检查 ruff，测试 pytest。

## 目录结构

```
src/
├── adapters/     # Agent 适配器（插件式：Claude CLI、OpenCode CLI）
├── api/v1/       # API 路由（Agent 执行、健康检查、会话、工作区）
├── app/          # 应用入口与配置（FastAPI 生命周期、config.yaml 加载）
├── rules/        # 规则引擎（Safety、Scope 等内置规则）
├── schemas/      # 数据模型（请求、响应、SSE 事件）
├── session/      # 会话管理（状态机、进程管理、持久化）
├── skills/       # 技能供给系统（内置 taskctl 等技能分发至工作区）
└── workspace/    # 工作区管理（Git Worktree 隔离、提交、合并、TTL 回收）
```

## 常用命令

> 需在 `agentend/` 目录下执行。

```bash
uv run python -m src.app.main      # 启动开发服务器（热重载）
```

## 详细文档

详见 [API 端点、核心架构、配置](docs/common/details.md)。
