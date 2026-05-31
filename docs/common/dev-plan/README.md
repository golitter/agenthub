# 开发路线图 — 单人三端开发计划

> AgentEnd MVP 已基本完成，本计划聚焦 Go Backend + React Frontend 的开发。
> 单人执行，串行叠代，每一步产出可运行的成果。

## 当前状态

```
AgentEnd (Python)  ~90%  ← MVP 可用，Orchestrator Agent 模式已重构（REASON + Memory + Wave Executor）
Backend  (Go)      ~90%  ← SSE + CRUD + Redis 缓冲 + 消息持久化 + Admin 面板 + 头像上传
Frontend (React)   ~90%  ← IM 聊天 + 会话管理 + Agent 选择 + Markdown + 11 种卡片 + Admin 面板
```

**Phase 1-4 ✅ 已完成 | Phase 5 ✅ 已完成（核心闭环）| Phase 6-7 📋 待收尾**

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
| 5 | Orchestrator 群聊 | Agent 模式重构（有记忆的 Orchestrator） | 5-6 天 | ✅ 核心闭环 | [phase5-orchestrator.md](phase5-orchestrator.md) / [当前状态审计](phase5-discussions/current-status-and-next-steps.md) |
| 6 | 预览 + 部署 | 产物预览卡片 + 部署发布 | TBD | 📋 待收尾 | [phase6-preview-deploy.md](phase6-preview-deploy.md) |
| 7 | 演示 + 交付 | 演示打磨 + 交付物整理 | 2 天 | 📋 待开始 | [phase7-demo-deliver.md](phase7-demo-deliver.md) |

**Phase 5 已完成核心闭环（REASON 节点 + 生命周期图 + Wave Executor + 群聊 UI）。剩余 Phase 6-7 约 3-5 个工作日。**

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
Phase 5 (Orchestrator)  ✅ 核心闭环
    │
    ├── Phase 6 (预览+部署)  📋 待收尾
    │
    └── Phase 7 (演示+交付)  📋 待开始
```
