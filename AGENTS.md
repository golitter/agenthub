# AGENTS.md

## 项目简介

Monorepo 项目，包含前端、后端、Agent 端三个子项目，通过契约层（contracts）统一跨端类型定义。多 Agent 聊天系统，支持 Claude Code、OpenCode CLI、Orchestrator 三类 Agent，具备实时 SSE 流式通信、会话管理、工作区隔离、技能供给等能力。

## 目录结构

```
bytedanceai/
├── frontend/      # React 前端 → 参见 frontend/AGENTS.md
├── backend/       # Go 后端   → 参见 backend/AGENTS.md
├── agentend/      # Python Agent 端 → 参见 agentend/AGENTS.md
├── contracts/     # 三端共享契约（schemas + logs）→ 参见 contracts/AGENTS.md
├── docs/          # 项目文档
│   ├── common/    #   公共文档（Git 规范、Makefile 指南、契约层说明）
│   └── architecture/ # 架构设计（三层架构设计）
├── scripts/       # 工程脚本
│   ├── run.sh               # 三端服务管理（启动/停止/重启/状态）
│   ├── generate_contracts.py # 契约代码生成器（YAML → Python/TS/Go）
│   └── test-clean.sh        # 测试数据一键清理（MySQL + Redis）
├── Makefile       # 统一命令入口
└── CLAUDE.md      # Claude Code 指令入口（@AGENTS.md）
```

子项目的框架选型、构建命令、测试方式等详情，请查阅对应目录下的 AGENTS.md。

## Makefile

通过 `make <命令>` 管理三端服务（热重载），详情参见 [docs/common/makefile-guide.md](docs/common/makefile-guide.md)。


## 契约优先原则

跨端协议的类型定义以 `contracts/schemas/` 为单一来源。修改协议时：

1. 先更新 `contracts/schemas/*.yaml`
2. 运行 `make generate` 生成三端类型
3. 在 `contracts/logs/` 写入变更记录

详见 [contracts/AGENTS.md](contracts/AGENTS.md)。

## Git 规范

详见 [docs/common/git-conventions.md](docs/common/git-conventions.md)。
