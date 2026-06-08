# 开发路线图 — 单人三端开发计划（已交付）

> AgentEnd MVP + Go Backend + React Frontend 三端联调完成，本期开发周期已结束。
> 未实现的 P2 增强项整理在 [TODO.md](TODO.md)，作为后续迭代的输入。

## 交付状态（2026-06-09 收尾）

```
AgentEnd (Python)  ✅ 已交付  ← MVP 可用，Orchestrator Agent 模式已重构（REASON + Memory + Wave Executor）
                        ← 跨 Agent 记忆持久化、SOUL.md 身份文档、Skill 分发、规划审查已实现
                        ← Git merge 冲突处理、执行级重试、动态重规划、会话恢复已实现
Backend  (Go)      ✅ 已交付  ← SSE + CRUD + Redis 缓冲 + 消息持久化 + Admin 面板 + 头像上传 + Git Graph
                        ← Workspace 完整代理（diff/commit/revert/preview/merge）+ Agent Profile 管理
                        ← 公告管理 + Pin/Unpin 通知机制 + Docker 容器化
Frontend (React)   ✅ 已交付  ← IM 聊天 + 会话管理 + Agent 选择 + Markdown + 11 种卡片 + Admin 面板 + Git Graph
                        ← 规划审查 UI + 右侧栏增强（公告/成员/历史搜索/Git Graph/路径信息）
```

**Phase 1-7 全部交付。** 剩余 P2 增强项（响应式断点、Profile 权限、Demo 视频等）整理在 [TODO.md](TODO.md) 的"遗留清单"段。

## 总体策略（回顾）

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
| 5 | Orchestrator 群聊 | Agent 模式重构（有记忆的 Orchestrator） | 5-6 天 | ✅ 完成 | [phase5-orchestrator.md](phase5-orchestrator.md) + [phase5-1-ask-agent/](phase5-1-ask-agent/) |
| 5a | 群聊增强 | 规划审查 + 右侧栏增强 + Git Graph | 3 天 | ✅ 完成 | [phase5-2-chat-enhanced/](phase5-2-chat-enhanced/) |
| 6 | 预览 + 部署 | Runtime 升级 + Profile System + MergeManager + Docker | TBD | ✅ 完成（核心能力 + Docker 部署） | [phase6-preview-deploy.md](phase6-preview-deploy.md) |
| 7 | 演示 + 交付 | 演示打磨 + 交付物整理 | 2 天 | ✅ 完成（文档与 Demo 场景就绪） | [phase7-demo-deliver.md](phase7-demo-deliver.md) |

## 核心纪律（回顾）

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
Phase 5.1 (ask-agent)   ✅
    │
Phase 5.2 (群聊增强)    ✅
    │
Phase 6 (Runtime + 部署)  ✅
    │
Phase 7 (演示+交付)       ✅
```

## 相关文档

- [TODO.md](TODO.md) — 本期未实现的 P1/P2 遗留清单（作为后续迭代输入）
- [phase5-notes/](phase5-notes/) — Phase 5 期间的设计讨论与实现笔记
