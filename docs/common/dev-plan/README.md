# 开发路线图 — 单人三端开发计划

> AgentEnd MVP 已基本完成，本计划聚焦 Go Backend + React Frontend 的开发。
> 单人执行，串行叠代，每一步产出可运行的成果。

## 当前状态

```
AgentEnd (Python)  ~85%  ← MVP 基本可用，Phase 4 前不动
Backend  (Go)      ~10%  ← 只有骨架（Gin + GORM + JWT），无业务代码
Frontend (React)   ~5%   ← 只有脚手架（React 19 + Vite + shadcn/ui），无功能页面
```

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

| Phase | 名称 | 目标 | 预估 | 详细文档 |
|-------|------|------|------|----------|
| 1 | [Go 胶水层](phase1-go-glue.md) | curl 走通 Go → AgentEnd SSE 流 | 2 天 | phase1-go-glue.md |
| 2 | [最小聊天界面](phase2-chat-ui.md) | 浏览器发消息，看 Agent 流式回复 | 3 天 | phase2-chat-ui.md |
| 3 | [IM 体验补全](phase3-im-exp.md) | 会话管理 + Agent 切换 + 历史加载 | 2 天 | phase3-im-exp.md |
| 4 | [产物与打磨](phase4-artifacts.md) | 代码块/工具卡片 + 产物预览 | 2-3 天 | phase4-artifacts.md |

**总计约 9-10 个工作日。**

## 核心纪律

1. **先跑通，再优化** — 不做 EventEnvelope 升级、不做断线重连、不做 EventLog 持久化
2. **Go 是薄壳代理** — Phase 1-2 的 Go 只做 SSE 透传 + 基础 CRUD，不碰 Runtime 逻辑
3. **AgentEnd Phase 4 前不动** — 除非发现 blocking bug
4. **每个 Phase 结束都有可演示成果** — 随时可以停下来交差

## Phase 依赖关系

```
Phase 1 (Go 胶水)
    │
    ▼
Phase 2 (前端聊天) ── 依赖 Phase 1 的真实 API
    │
    ▼
Phase 3 (IM 体验) ── 依赖 Phase 2 的基础聊天
    │
    ▼
Phase 4 (产物打磨) ── 依赖 Phase 3 的 IM 基础
    │
    ▼
(后续迭代: EventEnvelope 升级 / Replay / 多 Agent Timeline / 部署)
```
