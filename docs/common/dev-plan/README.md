# 开发路线图 — 单人三端开发计划

> AgentEnd MVP 已完成，本计划聚焦 Go Backend + React Frontend 的开发。
> 单人执行，串行叠代，每一步产出可运行的成果。

## 当前状态（2026-06-03 更新）

```
AgentEnd (Python)  ~95%  ← MVP 可用，Orchestrator Agent 模式已重构（REASON + Memory + Wave Executor）
                        ← 跨 Agent 记忆持久化、SOUL.md 身份文档、Skill 分发、规划审查已实现
                        ← Git merge 冲突处理、执行级重试、动态重规划、会话恢复已实现
Backend  (Go)      ~93%  ← SSE + CRUD + Redis 缓冲 + 消息持久化 + Admin 面板 + 头像上传 + Git Graph
                        ← Workspace 完整代理（diff/commit/revert/preview/merge）+ Agent Profile 管理
                        ← 公告管理 + Pin/Unpin 通知机制
Frontend (React)   ~92%  ← IM 聊天 + 会话管理 + Agent 选择 + Markdown + 11 种卡片 + Admin 面板 + Git Graph
                        ← 规划审查 UI + 右侧栏增强（公告/成员/历史搜索/Git Graph/路径信息）
```

**Phase 1-5 ✅ 已完成 | Phase 6 ⚠️ 大部分完成 | Phase 7 📋 待收尾**

## 总体策略

从内到外，串行叠代：先把 Go 胶水层接上 AgentEnd，再在 Go 之上搭 React UI。

```
  ┌─────────────────────────────────┐
  │  React (UI 层)        ← 后做    │
  │  ┌─────────────────────────────┐│
  │  │  Go (胶水层)        ← 先做  ││
  │  │  ┌─────────────────────────┐││
  │  │  │  AgentEnd (已能用)     │││
  │  │  └─────────────────────────┘││
  │  └─────────────────────────────┘│
  └─────────────────────────────────┘
```

## 阶段总览

| Phase | 名称 | 目标 | 预估 | 状态 | 详细文档 |
|-------|------|------|------|------|----------|
| 1 | Go 胶水层 | curl 走通 Go → AgentEnd SSE 流 | 2 天 | ✅ 完成 | [phase1-go-glue.md](phase1-go-glue.md) |
| 2 | 最小聊天界面 | 浏览器发消息，看 Agent 流式回复 | 3 天 | ✅ 完成 | [phase2-chat-ui.md](phase2-chat-ui.md) |
| 3 | IM 体验补全 | 会话管理 + Agent 切换 + 历史加载 | 2 天 | ✅ 完成 | [phase3-im-exp.md](phase3-im-exp.md) |
| 4 | 产物与打磨 | 代码块/工具卡片 + 产物预览 | 2-3 天 | ✅ 完成 | [phase4-artifacts.md](phase4-artifacts.md) |
| 5 | Orchestrator 群聊 | Agent 模式重构（有记忆的 Orchestrator） | 5-6 天 | ✅ 完成 | [phase5-orchestrator.md](phase5-orchestrator.md) |
| 5a | 群聊增强 | 规划审查 + 右侧栏增强 + Git Graph | 3 天 | ✅ 完成 | [phase5a/](phase5a/) |
| 6 | 预览 + 部署 | Runtime 升级 + Profile System + MergeManager | TBD | ⚠️ 大部分完成 | [phase6-preview-deploy.md](phase6-preview-deploy.md) |
| 7 | 演示 + 交付 | 演示打磨 + 交付物整理 | 2 天 | 📋 待收尾 | [phase7-demo-deliver.md](phase7-demo-deliver.md) |

**Phase 1-5a 已全部完成。Phase 6 Runtime 核心能力大部分已实现（记忆持久化、冲突处理、重试、重规划、会话恢复、Merge API），剩余部署容器化和 Profile 权限。Phase 7 交付物大部分已具备。**

## 核心纪律

1. **先跑通，再优化** — 每个 Phase 结束都有可演示成果
2. **Go 是薄壳代理** — Phase 1-4 的 Go 只做 SSE 透传 + 基础 CRUD，不碰 Runtime 逻辑
3. **串行执行** — Phase 4 完成后再集中做 Orchestrator
4. **只做 Web 端** — 不做桌面端/移动端

## Phase 依赖关系

```
Phase 1 (Go 胶水)       ✅
    │
Phase 2 (前端聊天)      ✅
    │
Phase 3 (IM 体验)       ✅
    │
Phase 4 (产物卡片)      ✅
    │
Phase 5 (Orchestrator)  ✅
    │
Phase 5a (群聊增强)     ✅
    │
    ├── Phase 6 (Runtime 升级)  ⚠️ 大部分完成（核心能力已实现，部署容器化待做）
    │
    └── Phase 7 (演示+交付)     📋 待收尾
```
