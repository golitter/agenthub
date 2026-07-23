# AGENTS.md — docs/
本目录是项目级文档的统一入口。所有 `docs/` 目录遵循相同的 **5 分类约定**。

## 分类约定

```
docs/
├── design/        # 怎么建的 — 架构设计、实现方案
├── reference/     # 是什么  — 技术栈、API 端点
├── guides/        # 怎么做  — Git 规范、环境搭建
├── testing/       # 测试    — 手动测试手册
├── prompts/       # 提示词  — Claude Code Skills prompt
└── common/        # 开发路线图（dev-plan）遗留 TODO
```

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

### guides/
- [git-conventions.md](guides/git-conventions.md) — Git 提交规范
- [makefile-guide.md](guides/makefile-guide.md) — Makefile 命令说明
- [contract-layer.md](guides/contract-layer.md) — 契约层使用指南
- [setup.md](guides/setup.md) — 环境搭建
- [monorepo-setup.md](guides/monorepo-setup.md) — Monorepo 配置
- [docker-deployment.md](guides/docker-deployment.md) — Docker 容器化部署
### testing/
- [inactive-cleanup.md](testing/inactive-cleanup.md) — 会话停用功能测试手册
- [code-audit-report.md](testing/code-audit-report.md) — 代码审计报告
### prompts/
- [autogit.md](prompts/autogit.md) — 自动 Git 提交
- [contracts.md](prompts/contracts.md) — 契约层 prompt
- [前端设计.md](prompts/前端设计.md) — 前端设计 prompt
- [4deepseek.md](prompts/4deepseek.md) — DeepSeek prompt
- [设计审计.md](prompts/设计审计.md) — 设计审计 prompt（审查 + 修正）
- [多模态：bug求助.md](prompts/多模态：bug求助.md) — 多模态 bug 求助 prompt
### dev-plan/
- [TODO.md](common/dev-plan/TODO.md) — 遗留待办项（P2 增强迭代输入）

## 子项目 docs/ & 规则
- [frontend/docs/](../frontend/docs/) / [backend/docs/](../backend/docs/) / [agentend/docs/](../agentend/docs/)
> 按语义入对应分类（kebab-case）；子项目文档放子项目 `docs/`，跨端放根 `docs/`，添加后在本索引补充
