# AGENTS.md — docs/

本目录是项目级文档的统一入口。所有 `docs/` 目录（根 + 子项目）遵循相同的 **5 分类约定**。

## 分类约定

```
docs/
├── design/        # 怎么建的 — 架构设计、实现方案、数据流、组件设计
├── reference/     # 是什么  — 技术栈、API 端点、适配器差异
├── guides/        # 怎么做  — Git 规范、Makefile 指南、环境搭建
├── testing/       # 测试    — 手动测试手册、测试流程
└── backlog/       # 接下来  — 待办、设计笔记（按需创建）
```

## 本目录文档索引

### design/

- [three-tier-design.md](design/three-tier-design.md) — 三层架构设计（React + Go + Python）
- [planning-context-module.md](design/planning-context-module.md) — Orchestrator Planning ToolMessage 结构化设计
- [soul-md-identity-document.md](design/soul-md-identity-document.md) — SOUL.md Agent 身份文档设计与实现
### reference/

- [skills.md](reference/skills.md) — Claude Code Skills 配置说明
### guides/

- [git-conventions.md](guides/git-conventions.md) — Git 提交规范
- [makefile-guide.md](guides/makefile-guide.md) — Makefile 命令说明
- [contract-layer.md](guides/contract-layer.md) — 契约层使用指南
- [setup.md](guides/setup.md) — 环境搭建
- [monorepo-setup.md](guides/monorepo-setup.md) — Monorepo 配置

### testing/

- [inactive-cleanup.md](testing/inactive-cleanup.md) — 会话停用功能测试手册
- [code-audit-report.md](testing/code-audit-report.md) — 代码审计报告

### bugfix/

- [multi-agent-message-split.md](bugfix/multi-agent-message-split.md) — 多 Agent 消息拆分修复
- [message-history-latest-page.md](bugfix/message-history-latest-page.md) — 消息刷新后输出丢失修复
- [orchestrator-streaming-hang.md](bugfix/orchestrator-streaming-hang.md) — Orchestrator 流式挂起修复
- [sub-agent-message-persistence-bugs.md](bugfix/sub-agent-message-persistence-bugs.md) — 子 Agent 消息持久化修复

### prompts/

- [autogit.md](prompts/autogit.md) — 自动 Git 提交
- [contracts.md](prompts/contracts.md) — 契约层 prompt
- [前端设计.md](prompts/前端设计.md) — 前端设计 prompt
- [4deepseek.md](prompts/4deepseek.md) — DeepSeek prompt

### payloads/

- [codediff-test.md](payloads/codediff-test.md) — CodeDiff 测试
- [orchestrator-test-scenarios.md](payloads/orchestrator-test-scenarios.md) — Orchestrator 测试场景

### dev-plan/（独立保留）

- [dev-plan/](common/dev-plan/) — 开发路线图（Phase 1-7）

## 子项目 docs/

- [frontend/docs/](../frontend/docs/) — 前端设计、组件、数据流、主题
- [backend/docs/](../backend/docs/) — 后端实现方案、技术栈
- [agentend/docs/](../agentend/docs/) — Agent 端架构、API 端点、适配器差异

## 新增文档规则

1. 按内容语义放入对应分类，文件名 kebab-case
2. 子项目文档放子项目 `docs/`，跨端文档放根 `docs/`；添加后在本索引补充条目