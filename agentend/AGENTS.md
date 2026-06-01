# AGENTS.md — agentend

基于 FastAPI 的 Agent Runtime 服务，桥接外部 Agent（Claude CLI / OpenCode CLI），提供会话管理、规则引擎、工作区隔离、技能供给和 Orchestrator 多 Agent 规划。Python >=3.10，包管理 uv，代码检查 ruff，测试 pytest。


## 目录结构

```
src/
├── adapters/                # Agent 适配器（Claude CLI / OpenCode CLI / Codex CLI / Orchestrator）
├── api/v1/                  # API 路由（agent, session, workspace, validate, health, pin, resources）
├── app/                     # 应用入口与配置（FastAPI 生命周期 + Pydantic Settings）
├── clients/                 # 外部服务客户端（BackendClient — Orchestrator 与 Go Backend 通信）
├── orchestrator/            # Orchestrator 规划模块（LangGraph + LLM 任务拆解与分发）
├── preview/                 # 工作区预览服务（aiohttp 静态文件服务器）
├── rules/                   # 规则引擎（Safety / Soul / GroupChat / Scope / Taskctl）
├── schemas/                 # 数据模型（request, response, events）
├── session/                 # 会话管理（状态机 + 持久化）
├── skills/                  # 技能供给系统（内置 taskctl + render）
├── workspace/               # 工作区管理（Git Worktree 隔离）
└── generated/               # 契约生成的 Python 类型（勿手改）

docs/
├── design/                  # 设计文档（架构、实现方案）
├── reference/               # 参考文档（API 端点、适配器差异）
├── testing/                 # 测试手册（手动测试流程）
└── backlog/                 # 待办 / 设计笔记
```

## 常用命令

> 通过根目录 Makefile 统一管理，需在项目根目录执行。

```bash
make run-agentend          # 启动（热重载）
make stop-agentend         # 停止
make restart-agentend      # 重启
make status                # 查看状态
```

- Makefile 完整说明：[docs/guides/makefile-guide.md](../docs/guides/makefile-guide.md)
- 排查问题查看 `../logs/agentend.log`

## 详细文档

详见 [docs/reference/details.md](docs/reference/details.md)。
