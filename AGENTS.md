# AGENTS.md

## 项目简介

Monorepo 项目，包含前端、后端、Agent 端三个子项目，通过契约层（contracts）统一跨端类型定义。多 Agent 聊天系统，支持 Claude Code、OpenCode CLI、Codex CLI、Orchestrator 四类 Agent，具备实时 SSE 流式通信、会话管理、工作区隔离、技能供给等能力。

## 目录结构

```
agenthub/
├── frontend/      # React 前端 → 参见 frontend/AGENTS.md
├── backend/       # Go 后端   → 参见 backend/AGENTS.md
├── agentend/      # Python Agent 端 → 参见 agentend/AGENTS.md
├── contracts/     # 三端共享契约（schemas + logs）→ 参见 contracts/AGENTS.md
├── docs/          # 项目文档
│   ├── design/    #   架构设计
│   ├── reference/ #   参考文档
│   ├── guides/    #   操作指南
│   ├── testing/   #   测试手册
│   ├── prompts/   #   Claude Code Skills prompt
│   └── common/    #   开发路线图（dev-plan）遗留 TODO
├── docker/        # Docker 部署（docker-compose.yml + Backend/Frontend Dockerfile + Nginx + precheck）
├── scripts/       # 工程脚本
│   ├── run.sh               # 三端服务管理（启动/停止/重启/状态）
│   ├── generate_contracts.py # 契约代码生成器（YAML → Python/TS/Go）
│   └── test-clean.sh        # 测试数据一键清理（MySQL + Redis）
├── logs/          # 运行日志（run.sh 启动时自动生成）
│   ├── frontend.log         #   Vite dev server 日志
│   ├── backend.log          #   Air / Go 后端日志
│   └── agentend.log         #   uvicorn / Agent 端日志
├── Makefile       # 统一命令入口
└── CLAUDE.md      # Claude Code 指令入口（@AGENTS.md）
```

子项目的框架选型、构建命令、测试方式等详情，请查阅对应目录下的 AGENTS.md。

## Makefile

通过 `make <命令>` 管理三端服务（热重载），详情参见 [docs/guides/makefile-guide.md](docs/guides/makefile-guide.md)。排查问题时查看 `logs/` 目录下的三端日志（`frontend.log` / `backend.log` / `agentend.log`）。


## 契约优先原则

跨端协议的类型定义以 `contracts/schemas/` 为单一来源。修改协议时：

1. 先更新 `contracts/schemas/*.yaml`
2. 运行 `make generate` 生成三端类型
3. 在 `contracts/logs/` 写入变更记录

详见 [contracts/AGENTS.md](contracts/AGENTS.md)。

## Git 规范

详见 [docs/guides/git-conventions.md](docs/guides/git-conventions.md)。

## 文档体系

详见 [docs/AGENTS.md](docs/AGENTS.md) — 文档分类约定、索引、新增规则。
