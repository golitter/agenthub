# Phase 6: Runtime 升级 + 部署发布

> 目标: Runtime 能力补全、Profile System 完善、MergeManager 升级、容器化部署
> 前置: Phase 5a 完成 (群聊增强迭代)
> 状态: ⚠️ 部分完成
> 备注: 产物预览功能已在 Phase 4/5 中提前实现，此处聚焦 Runtime 升级和部署

## 已实现（Phase 4/5/5a 提前完成）

| 功能 | 状态 | 说明 |
|------|------|------|
| DiffCard（代码差异视图） | ✅ | Split/Unified + 多文件 Tab + CodeMirror 编辑 |
| PreviewCard（文件预览） | ✅ | 文件内容预览 |
| ImageCard（图片预览） | ✅ | 图片直接展示 |
| HtmlCard（HTML 渲染） | ✅ | HTML 内容内联渲染 |
| AttachmentCard（附件下载） | ✅ | 文件附件下载 |
| Preview Server | ✅ | AgentEnd aiohttp 静态文件服务器 |
| Workspace 代理 | ✅ | Go Backend 代理文件操作 / diff / commit / revert / preview |
| Diff Snapshot | ✅ | 代码差异快照保存/恢复 |
| Agent Workspace 隔离 | ✅ | Git worktree 按 RuntimeAgent 隔离 |
| Wave Executor 并行 | ✅ | DAG 拓扑排序 + 波内并行执行 |
| SOUL.md 身份文档 | ✅ | 可编辑 + 注入 system prompt |
| 跨 Agent 记忆 | ✅ | 群聊窗口消息注入 |
| Skill 分发 | ✅ | 渐进式 Skill 发现 + L1/L2 加载 |
| Git Graph 面板 | ✅ | 前端实时 Git 可视化 |
| 规划审查 | ✅ | PlanReviewCard + Agent 审查逻辑 |

## 待实现

### Runtime 升级

| 功能 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| MemorySaver 持久化 | P1 | 📋 待实现 | 当前内存级，需迁移到持久存储（SQLite/文件） |
| Conflict-Resolution Task | P1 | 📋 待实现 | 冲突时自动 spawn reviewer 解冲突 |
| Retry / Cancellation | P1 | 📋 待实现 | 执行级失败重试和任务取消（规划级已有） |
| Dynamic Replanning | P2 | 📋 待实现 | REVIEW 后动态调整计划 |
| Durable Resume | P2 | 📋 待实现 | 断线恢复能力 |

### Profile System (SOUL)

| 功能 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| Profile 目录结构 | P1 | 📋 待完善 | `agentend/src/profiles/` 下完整 SOUL 定义 |
| Capability Permission | P2 | 📋 待实现 | 基于 SOUL 的权限检查 |
| Prompt Renderer | P2 | 📋 待实现 | 模板化 Prompt 组装 |

### MergeManager 完善

| 功能 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| Merge 冲突处理 | P1 | 📋 待实现 | 冲突 → 自动 spawn reviewer → 解冲突 → 重试 |
| Merge 事件 | P1 | 📋 待实现 | `workspace.branch.created` / `workspace.merge.*` 事件流 |

### 部署

| 功能 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| Docker Compose | P2 | 📋 待实现 | 三端容器化部署（目前无 Dockerfile） |
| Nginx 反向代理 | P2 | 📋 待实现 | 生产环境代理 |
| 部署状态卡片 | P2 | 📋 待实现 | 前端展示部署进度 |

## 不做

| 不做 | 理由 |
|------|------|
| Artifact DAG / versioning / lineage | MVP 不需要 |
| 源码打包下载 | P2 |
| 桌面端 / 移动端 | 只做 Web 端 |
| Diff 版本历史 | 任务要求标记 P2 |

## 预估

- P1 项（MemorySaver 持久化 + Conflict-Resolution + Retry/Cancellation + MergeManager + Profile 目录）：约 3-4 天
- P2 项（Dynamic Replanning + Durable Resume + 部署容器化）：约 2-3 天
- 总计约 5-7 个工作日
