# AGENTS.md — docs/

本目录是项目级文档的统一入口。所有 `docs/` 目录遵循相同的 **5 分类约定**。

## 分类约定

```
docs/
├── design/        # 怎么建的 — 架构设计、实现方案
├── reference/     # 是什么  — 技术栈、API 端点
├── guides/        # 怎么做  — Git 规范、环境搭建
├── testing/       # 测试    — 手动测试手册
├── bugfix/        # 修了啥  — 历史缺陷修复记录
├── prompts/       # 提示词  — Claude Code Skills prompt
├── payloads/      # 测试数据 — Demo 与测试场景
└── common/        # 接下来  — 开发路线图（dev-plan）
```
## 本目录文档索引

### design/
- [01-three-tier-design.md](design/01-three-tier-design.md) — 三层架构设计
- [02-group-chat-cross-agent-memory.md](design/02-group-chat-cross-agent-memory.md) — 跨 Agent 记忆
- [03-orchestrator-plan-review.md](design/03-orchestrator-plan-review.md) — 规划审查机制
- [04-soul-md-identity-document.md](design/04-soul-md-identity-document.md) — SOUL.md 身份文档
- [06-contacts-pin-leave-group.md](design/06-contacts-pin-leave-group.md) — 通讯录 + 置顶会话 + 退出群聊
- [07-skills-hub-external-skills.md](design/07-skills-hub-external-skills.md) — SkillsHub 内置与外置 Skills 架构
- [08-skills-db-migration.md](design/08-skills-db-migration.md) — Skills 本地文件存储 → 数据库 blob 迁移
- [09-agent-routing-and-dispatch.md](design/09-agent-routing-and-dispatch.md) — Agent 路由与 Orchestrator 自动分派
- [sse-streaming-architecture.md](design/sse-streaming-architecture.md) — SSE 流式输出架构（三端全链路）

### reference/
- [skills.md](reference/skills.md) — Claude Code Skills 配置说明
- [codegraph-openspec.md](reference/codegraph-openspec.md) — CodeGraph 代码知识图谱 & OpenSpec SDD 工作流
- [project-requirements.md](reference/project-requirements.md) — AgentHub 多 Agent 协作平台课题要求

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
- [orchestrator-echo-duplicate-messages.md](bugfix/orchestrator-echo-duplicate-messages.md) — 群聊消息重复存储 + 身份伪造修复
- [sse-streaming-performance-and-rendering.md](bugfix/sse-streaming-performance-and-rendering.md) — SSE 流式输出性能优化 + 渲染修复
- [orchestrator-group-chat-message-fragmentation.md](bugfix/orchestrator-group-chat-message-fragmentation.md) — Orchestrator 群聊消息碎片化问题分析
### prompts/
- [autogit.md](prompts/autogit.md) — 自动 Git 提交
- [contracts.md](prompts/contracts.md) — 契约层 prompt
- [前端设计.md](prompts/前端设计.md) — 前端设计 prompt
- [4deepseek.md](prompts/4deepseek.md) — DeepSeek prompt
- [设计审计.md](prompts/设计审计.md) — 设计审计 prompt（审查 + 修正）
- [多模态：bug求助.md](prompts/多模态：bug求助.md) — 多模态 bug 求助 prompt
### payloads/
- [codediff-test.md](payloads/codediff-test.md) / [orchestrator-test-scenarios.md](payloads/orchestrator-test-scenarios.md) — 测试数据与场景
- Demo: [plan-review](payloads/plan-review-demo.html) / [contacts-pin](payloads/contacts-pin-leave-demo.html) / [skills-hub](payloads/skills-hub-demo.html)
### dev-plan/
- [dev-plan/](common/dev-plan/) — 开发路线图（Phase 1-7）+ [TODO.md](common/dev-plan/TODO.md)

## 子项目 docs/
- [frontend/docs/](../frontend/docs/) — 前端设计、组件、数据流
- [backend/docs/](../backend/docs/) — 后端实现方案、技术栈
- [agentend/docs/](../agentend/docs/) — Agent 端架构、API 端点
> 按语义入对应分类（kebab-case 命名）；子项目文档放子项目 `docs/`，跨端放根 `docs/`，添加后在本索引补充
