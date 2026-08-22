# AGENTS.md

## 项目简介

Monorepo 项目，包含前端、后端、Agent 端三个子项目，通过契约层（contracts）统一跨端类型定义。多 Agent 聊天系统，支持 Claude Code、OpenCode CLI、Codex CLI、Pi CLI、Orchestrator 五类 Agent，具备实时 SSE 流式通信、会话管理、工作区隔离、技能供给等能力。

## 目录结构

```
agenthub/
├── .agents/       # Codex 本地技能（skills/<name>/SKILL.md）
├── .claude/       # Claude Code 本地技能（skills/<name>/SKILL.md，与 .agents/ 同源）
├── frontend/      # React 前端 → 参见 frontend/AGENTS.md
├── backend/       # Go 后端   → 参见 backend/AGENTS.md
├── agentend/      # Python Agent 端 → 参见 agentend/AGENTS.md
├── config-center/ # 独立配置编辑器（example/actual 双栏，Web 5174 / API 9100）
├── contracts/     # 三端共享契约（schemas + logs）→ 参见 contracts/AGENTS.md
├── docs/          # 项目文档
│   ├── design/    #   架构设计
│   ├── reference/ #   参考文档
│   ├── guides/    #   操作指南
│   ├── testing/   #   测试手册
│   ├── prompts/   #   Claude Code Skills prompt
│   └── common/    #   开发路线图（dev-plan）遗留 TODO
├── docker/        # Docker 部署（docker-compose.yml + Backend/Frontend Dockerfile + Nginx + precheck）
├── openspec/      # OpenSpec 变更 / 规格归档
├── scripts/       # 工程脚本
│   ├── server-env.example.sh # 可选的本地运行环境变量模板
│   ├── run.sh               # 三端服务管理（启动/停止/重启/状态）
│   ├── generate_contracts.py # 契约代码生成器（YAML → Python/TS/Go）
│   ├── scheduler/          # 本地调度工具（docs-sync 定时任务脚本）
│   └── test-clean.sh        # 测试数据一键清理（MySQL + Redis）
├── logs/          # 运行日志（frontend/backend/agentend.log + config-center.log，run.sh 启动时生成）
├── Makefile       # 统一命令入口
└── CLAUDE.md      # Claude Code 指令入口（@AGENTS.md）
```

子项目的框架选型、构建命令、测试方式等详情，请查阅对应目录下的 AGENTS.md。

## Makefile

通过根目录 Makefile 管理三端服务、契约生成、技能构建和 Docker 部署，详情参见 [docs/guides/makefile-guide.md](docs/guides/makefile-guide.md)：

```bash
make all                         # 启动全部（先检查内置 skill CLI）
make run-frontend                # 单端启动；run-backend / run-agentend 同类
make stop                        # 全部停止；restart / status / 单端 stop-* / restart-* 同类
make generate                    # 生成契约；make backend tidy 整理 Go 依赖
make skills build               # 构建内置 taskctl/render；skills check 检查
make skills migrate             # 历史 Skill BLOB 迁移/校验；skills reconcile 对账
make docker up                  # Docker 启动；down/build/logs/status 同组
make config start               # 配置编辑器（Web 5174 / API 9100）；config test 验收
make env wsl                    # 打印 WSL2 运行说明
make help                       # 查看完整命令入口
```

## 契约优先原则

跨端协议的类型定义以 `contracts/schemas/` 为单一来源。修改协议时：

1. 先更新 `contracts/schemas/*.yaml`
2. 运行 `make generate` 生成三端类型
3. 在 `contracts/logs/` 写入变更记录

详见 [contracts/AGENTS.md](contracts/AGENTS.md)。

## Git 规范

详见 [docs/guides/git-conventions.md](docs/guides/git-conventions.md)。文档体系与分类约定见 [docs/AGENTS.md](docs/AGENTS.md)。
