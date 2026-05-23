# AGENTS.md — agentend

基于 FastAPI 的 Agent Runtime 服务，桥接外部 Agent（Claude CLI / OpenCode CLI），提供会话管理、规则引擎、工作区隔离、技能供给和 Orchestrator 规划。Python >=3.10，包管理 uv，代码检查 ruff，测试 pytest。

## 目录结构

```
src/
├── adapters/       # Agent 适配器（插件式：Claude CLI、OpenCode CLI、Orchestrator）
├── api/v1/         # API 路由（Agent 执行、健康检查、会话、工作区）
├── app/            # 应用入口与配置（FastAPI 生命周期、config.yaml 加载）
├── orchestrator/   # Orchestrator 规划模块（LangGraph + LLM 任务拆解与分发）
├── rules/          # 规则引擎（Safety、Scope 等内置规则）
├── schemas/        # 数据模型（请求、响应、SSE 事件）
├── session/        # 会话管理（状态机、进程管理、持久化）
├── skills/         # 技能供给系统（内置 taskctl 等技能分发至工作区）
└── workspace/      # 工作区管理（Git Worktree 隔离、提交、合并、TTL 回收）

docs/
├── common/         # 公共文档（API 端点、架构概览、适配器差异）
├── impl/           # 实现文档（按功能编号，含架构总览与各模块细节）
├── playbooks/      # 手动测试流程（curl 调用 + 验证步骤）
└── todos/          # 待办事项
```

## 常用命令

> 通过根目录 Makefile 统一管理，需在项目根目录执行。

```bash
make run-agentend          # 启动（热重载）
make stop-agentend         # 停止
make restart-agentend      # 重启
make status                # 查看状态
```

如需手动启动：`cd agentend && uv run uvicorn src.app.main:app --reload`

- Makefile 完整说明：[docs/common/makefile-guide.md](../docs/common/makefile-guide.md)

## 详细文档

- API 端点、核心架构、配置：[docs/common/details.md](docs/common/details.md)
