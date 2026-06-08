# Phase 6: Runtime 升级 + 部署发布

> 目标: Runtime 能力补全、Profile System 完善、MergeManager 升级、容器化部署
> 前置: Phase 5a 完成 (群聊增强迭代)
> 状态: ✅ 已完成（本期开发周期 2026-06-09 结束）
> 备注: 产物预览功能已在 Phase 4/5 中提前实现，此处聚焦 Runtime 升级和部署
> 备注: 未在本期实现的 P2 增强项（Profile 权限、Prompt Renderer、部署状态卡片等）整理在 [TODO.md](TODO.md)

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
| ~~MemorySaver 持久化~~ | P1 | ✅ 已实现 | 文件系统级持久化（conversation_memory.json + _pins.yaml），增量保存 |
| ~~Conflict-Resolution Task~~ | P1 | ✅ 已实现 | `git_ops.py` merge_branch() 自动检测冲突，支持 merge --abort 回滚 |
| ~~Retry / Cancellation~~ | P1 | ✅ 已实现 | `graph.py` ask_agent 最多重试 3 次，固定延迟递增（1.0*(attempt+1)s） |
| ~~Dynamic Replanning~~ | P2 | ✅ 已实现 | REVIEW 节点检查失败任务，触发重规划（max_iterations 控制） |
| ~~Durable Resume~~ | P2 | ✅ 已实现 | LangGraph MemorySaver checkpoint + is_resume 会话恢复逻辑 |

### Profile System (SOUL)

| 功能 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| Profile 目录结构 | P1 | 📋 未在本期实现 | `agentend/src/profiles/` 下完整 SOUL 定义（SOUL.md 注入已可用，目录结构未补全） |
| Capability Permission | P2 | 📋 未在本期实现 | 基于 SOUL 的权限检查 |
| Prompt Renderer | P2 | 📋 未在本期实现 | 模板化 Prompt 组装 |

### MergeManager 完善

| 功能 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| ~~Merge 冲突处理~~ | P1 | ✅ 已实现 | `git_ops.py` merge_branch() 冲突检测 + 回滚。Backend Merge API 已实现（`POST /api/workspace/task/:taskId/merge-to-main`） |
| Merge 事件 | P1 | 📋 未在本期实现 | `workspace.branch.created` / `workspace.merge.*` 事件流 |

### 部署

| 功能 | 优先级 | 状态 | 说明 |
|------|--------|------|------|
| ~~Docker Compose~~ | P2 | ✅ 已实现 | `docker/` 目录含 docker-compose.yml + Backend/Frontend Dockerfile + Nginx 配置 + precheck 脚本 |
| ~~Nginx 反向代理~~ | P2 | ✅ 已实现 | `docker/frontend/nginx.conf` 已配置 SPA 路由 + /api 代理 + SSE 支持 |
| 部署状态卡片 | P2 | 📋 未在本期实现 | 前端展示部署进度 |

## 不做

| 不做 | 理由 |
|------|------|
| Artifact DAG / versioning / lineage | MVP 不需要 |
| 源码打包下载 | P2 |
| 桌面端 / 移动端 | 只做 Web 端 |
| Diff 版本历史 | 任务要求标记 P2 |

## 预估（回顾）

- ~~P1 项（MemorySaver 持久化 + Conflict-Resolution + Retry/Cancellation + MergeManager）~~：✅ 已完成
- ~~P2 项（Docker Compose + Nginx 反向代理）~~：✅ 已完成
- 未在本期实现：Profile 目录结构、Capability Permission、Prompt Renderer、Merge 事件、部署状态卡片 — 见 [TODO.md](TODO.md)
