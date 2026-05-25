# 开发路线图 — 单人三端开发计划

> AgentEnd MVP 已基本完成，本计划聚焦 Go Backend + React Frontend 的开发。
> 单人执行，串行叠代，每一步产出可运行的成果。

## 当前状态

```
AgentEnd (Python)  ~85%  ← MVP 基本可用，Orchestrator 待接入
Backend  (Go)      ~80%  ← SSE + CRUD + Redis 缓冲 + 消息持久化
Frontend (React)   ~70%  ← IM 聊天 + 会话管理 + Agent 选择 + Markdown
```

**Phase 1-3 ✅ 已完成 | Phase 4 🔧 待执行 | Phase 5-7 📋 待开始**

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
| 4 | 产物与打磨 | 代码块/工具卡片 + 产物预览 | 2-3 天 | 🔧 待执行 | [phase4-artifacts.md](phase4-artifacts.md) |
| 5 | Orchestrator 群聊 | LangGraph 接入 + 多 Agent 协作 | 3-4 天 | 📋 待开始 | [phase5-orchestrator.md](phase5-orchestrator.md) |
| 6 | 预览 + 部署 | 产物预览卡片 + 部署发布 | TBD | 📋 draft | [phase6-preview-deploy.md](phase6-preview-deploy.md) |
| 7 | 演示 + 交付 | 演示打磨 + 交付物整理 | 2 天 | 📋 待开始 | [phase7-demo-deliver.md](phase7-demo-deliver.md) |

**剩余预估约 7-11 个工作日（Phase 4-7）。**

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
Phase 4 (产物卡片)      🔧 ← 当前
    │
Phase 5 (Orchestrator)  📋
    │
    ├── Phase 6 (预览+部署)  📋 draft
    │
    └── Phase 7 (演示+交付)  📋
```
