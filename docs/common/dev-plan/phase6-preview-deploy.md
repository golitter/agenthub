# Phase 6: 产物预览 + 部署发布

> 目标: Agent 产物的内联预览完善、Runtime 升级、部署发布
> 前置: Phase 5 完成 (Orchestrator 群聊核心闭环)
> 状态: 📋 待收尾
> 备注: 部分功能已在 Phase 4/5 中提前实现

## 已实现（Phase 4/5 提前完成）

| 功能 | 状态 | 说明 |
|------|------|------|
| DiffCard（代码差异视图） | ✅ | Split/Unified + 多文件 Tab + CodeMirror 编辑 |
| PreviewCard（文件预览） | ✅ | 文件内容预览 |
| ImageCard（图片预览） | ✅ | 图片直接展示 |
| HtmlCard（HTML 渲染） | ✅ | HTML 内容内联渲染 |
| AttachmentCard（附件下载） | ✅ | 文件附件下载 |
| Preview Server | ✅ | AgentEnd aiohttp 静态文件服务器 |
| Workspace 代理 | ✅ | Go Backend 代理文件操作 / diff / commit / revert |
| Diff Snapshot | ✅ | 代码差异快照保存/恢复 |
| Agent Workspace 隔离 | ✅ | Git worktree 按 RuntimeAgent 隔离 |

## 待实现

### Runtime 升级

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Scheduler 并行执行 | P1 | Wave Executor 已预留 DAG 拓扑，需完善并行调度 |
| Conflict-Resolution Task | P1 | 冲突时自动 spawn reviewer 解冲突 |
| MemorySaver 持久化 | P1 | 当前内存级，需迁移到持久存储（SQLite/文件） |
| Retry / Cancellation | P2 | 失败重试和任务取消 |
| Dynamic Replanning | P2 | REVIEW 后动态调整计划 |
| Durable Resume | P2 | 断线恢复能力 |

### Profile System (SOUL)

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Profile 目录结构 | P1 | `agentend/src/profiles/` 下完整 SOUL 定义 |
| Capability Permission | P2 | 基于 SOUL 的权限检查 |
| Prompt Renderer | P2 | 模板化 Prompt 组装 |

### MergeManager 完善

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Merge 冲突处理 | P1 | 冲突 → 自动 spawn reviewer → 解冲突 → 重试 |
| Merge 事件 | P1 | `workspace.branch.created` / `workspace.merge.*` 事件流 |

### 部署

| 功能 | 优先级 | 说明 |
|------|--------|------|
| Docker Compose | P2 | 三端容器化部署 |
| Nginx 反向代理 | P2 | 生产环境代理 |
| 部署状态卡片 | P2 | 前端展示部署进度 |

## 不做

| 不做 | 理由 |
|------|------|
| Artifact DAG / versioning / lineage | MVP 不需要 |
| 容器化部署 | P2，留后续迭代 |
| 源码打包下载 | P2 |
| 桌面端 / 移动端 | 只做 Web 端 |
| Diff 版本历史 | 任务要求标记 P2 |

## 预估

粗略估计 2-3 天（含 Runtime 升级 + MergeManager + Profile System 基础版）。
