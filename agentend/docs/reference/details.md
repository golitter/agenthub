# 详细文档

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/agent/stream` | 流式 Agent 响应（SSE） |
| POST | `/v1/agent/execute` | 同步 Agent 执行 |
| POST | `/v1/agent/review` | 提交 Orchestrator 规划审查 |
| GET | `/v1/session` | 列出所有会话 |
| GET | `/v1/session/{id}` | 获取会话详情 |
| POST | `/v1/session/{id}/interrupt` | 中断运行中会话 |
| DELETE | `/v1/session/{id}` | 删除会话 |
| POST | `/v1/workspace/create` | 创建工作区（Git Worktree） |
| GET | `/v1/workspace` | 列出所有工作区 |
| GET | `/v1/workspace/{id}/files/{path}` | 读取工作区文件 |
| PUT | `/v1/workspace/{id}/files/{path}` | 写入工作区文件 |
| GET | `/v1/workspace/{id}/diff` | 获取工作区 diff |
| POST | `/v1/workspace/{id}/commit` | 提交工作区变更 |
| POST | `/v1/workspace/{id}/revert` | 撤销工作区变更 |
| POST | `/v1/workspace/{id}/merge` | 合并工作区到目标分支 |
| POST | `/v1/workspace/{id}/preview/start` | 启动预览服务器 |
| POST | `/v1/workspace/{id}/preview/stop` | 停止预览服务器 |
| POST | `/v1/workspace/task/{task_id}/merge-to-main` | 合并任务分支到 main |
| GET | `/v1/workspace/task/{task_id}/git-info` | 获取 task 分支 Git 信息 |
| GET | `/v1/workspace/by-session/{session_id}` | 按 session 查找工作区 |
| DELETE | `/v1/workspace/{id}` | 清理工作区 |
| DELETE | `/v1/workspace/task/{task_id}` | 清理 task 下所有 workspace |
| POST | `/v1/workspace/task/{task_id}/cleanup-branches` | 强制清理 task 分支（无活跃 workspace 时） |
| POST | `/v1/validate-repo-path` | 验证 repo 路径 |
| POST | `/v1/init-git-repo` | 初始化 Git 仓库 |
| POST | `/v1/pin/add` | 添加 Pin 到共享内存 |
| POST | `/v1/pin/remove` | 移除 Pin |
| POST | `/v1/pin/announcement-unpin` | Backend 通知 pinned announcement 已删除 |
| GET | `/v1/pin/list` | 列出所有 Pin |
| GET | `/v1/resources` | 系统资源监控（磁盘 + 内存） |
| GET | `/v1/skills/{agent_type}` | 扫描已安装的技能列表 |
| POST | `/v1/skills/{agent_type}/{skill_name}/install` | 安装指定技能 |
| DELETE | `/v1/skills/{agent_type}/{skill_name}` | 移除指定技能 |
| GET | `/v1/agents/configs` | 读取各 Agent CLI 的系统级配置文件 |
| GET | `/health` | 健康检查 |

## 项目结构

```
agentend/
├── src/
│   ├── adapters/       # Adapter 适配器层（Claude / OpenCode / Codex / Orchestrator）
│   ├── api/            # FastAPI HTTP 端点
│   │   └── v1/         # v1 版本 API（agent, session, workspace, validate, health, pin, resources, skills）
│   ├── app/            # 应用入口、配置、DI
│   ├── clients/        # 外部服务客户端（BackendClient 与 Go Backend 通信）
│   ├── generated/      # 契约生成的 Python 类型（勿手改）
│   ├── orchestrator/   # Orchestrator 规划模块
│   │   ├── planning/   #   LangGraph 规划（graph + prompts + tools + skill_loader）
│   │   ├── execution/  #   任务执行（engine + dispatcher + coordination + state + wave）
│   │   ├── memory/     #   持久记忆（pin_memory + conversation_memory + evolution）
│   │   ├── prompts/    #   提示模板（group_chat 跨 Agent 上下文构建）
│   │   └── reporting/  #   报告汇总（aggregator）
│   ├── preview/        # 工作区预览服务（aiohttp 静态文件服务器）
│   ├── rules/          # Rule Engine 规则引擎
│   ├── schemas/        # 数据模型
│   ├── session/        # Session 会话管理
│   ├── skills/         # 技能供给系统（SkillProvisioner + manifest，内置 taskctl + render）
│   └── workspace/      # 工作区管理（Git Worktree 隔离 + 持久化 + 恢复）
├── docs/
│   ├── design/         # 设计文档（架构、schemas、adapters、session 等）
│   ├── reference/      # 参考文档（API 端点、适配器差异）
│   ├── testing/        # 测试手册（手动测试流程）
│   └── backlog/        # 待办 / 设计笔记
├── pyproject.toml      # 项目配置与依赖
└── ruff.toml           # 代码风格
```

## 核心架构

- **执行流程**：请求到达 → 规则引擎评估 → 适配器注册表解析 → 会话管理器跟踪状态 → 适配器执行 → 结果流式/同步返回
- **会话状态机**：`IDLE → RUNNING → COMPLETED / INTERRUPTED / ERROR`，另含 `INACTIVE` 状态用于标记不活跃会话
- **适配器模式**：通过抽象基类支持不同 Agent 类型，当前实现 Claude CLI、OpenCode CLI、Codex CLI 与 Orchestrator 适配器
- **外部客户端**：`BackendClient`（`src/clients/backend_client.py`）与 Go Backend 通信，用于 Orchestrator 协调
- **Orchestrator 规划**：通过 LangGraph + LLM 将用户需求拆解为多 Agent 子任务，写入 `shared/.agent/` 目录供各 agent 消费。模块分为 planning（规划）、execution（执行调度）、memory（持久记忆）、reporting（汇总报告）四个子模块
- **规则引擎**：执行前评估 Safety（阻止危险工具）、Pin（Backend 置顶公告约束注入）、Soul（SOUL.md 身份注入）、GroupChat（跨 Agent 上下文注入）、Scope（校验工作区路径）、Taskctl（合并指令注入）、Skill（输出技能提示）等规则，可修改 system prompt 和工具白名单
- **会话持久化**：API session_id 与 CLI session_id 映射持久化至 `logs/session_mappings.json`
- **工作区管理**：基于 Git Worktree 的任务级隔离，支持自动创建任务分支（`task/{task_id}`）、提交、合并与清理，含 TTL 自动回收与启动恢复
- **Pin 内存系统**：通过 `/v1/pin` 端点管理共享内存中的固定条目，支持多 Agent 间共享上下文

## 配置

统一通过 `config.yaml` 管理（`pydantic-settings` + `YamlConfigSettingsSource`），LLM 密钥通过 `.env` 读取。配置项分组如下：

- **server** — 监听地址、端口、CORS、热重载
- **app** — 应用标题和版本
- **workspace** — Worktree 根目录、清理巡检间隔、存储路径、默认分支
- **session** — 会话映射持久化路径
- **database** — MySQL 连接信息（用于 inactive session 清理查询）
- **execution** — 最大轮次、执行超时、进程终止超时
- **skills** — 内置技能目录与分发清单
- **llm** — Orchestrator LLM 配置（优先从 `.env` 读取 `DS_MODEL`、`DS_BASE_URL`、`DS_API_KEY`）
- **backend** — Go Backend 连接地址（默认 `http://localhost:8080`）

详见 [config.yaml](../../config.yaml) 中的注释。

## 文档索引

### design/（开发实施文档）

- [01-schemas.md](../design/01-schemas.md) — 数据模型（AgentRequest / AgentResponse / StreamEvent）
- [02-adapters.md](../design/02-adapters.md) — 适配器层（Claude CLI / OpenCode CLI / Codex CLI / Orchestrator）
- [03-session.md](../design/03-session.md) — 会话管理（状态机 + 持久化）
- [04-rules.md](../design/04-rules.md) — 规则引擎（Safety / Pin / Soul / GroupChat / Scope / Taskctl / Skill）
- [05-api.md](../design/05-api.md) — API 端点（SSE 流式 / 同步执行 / Session CRUD）
- [06-app-wiring.md](../design/06-app-wiring.md) — 应用组装（FastAPI 生命周期 + DI）
- [07-session-mapping.md](../design/07-session-mapping.md) — CLI Session ID 映射
- [08-workspace.md](../design/08-workspace.md) — 工作区管理（Git Worktree 隔离）
- [09-cli-session-id-writeback.md](../design/09-cli-session-id-writeback.md) — CLI session_id 回写机制
- [10-workspace-branch-cleanup.md](../design/10-workspace-branch-cleanup.md) — Workspace 分支清理
- [11-orchestrator-planning.md](../design/11-orchestrator-planning.md) — Orchestrator 规划（LangGraph）
- [12-cli-adapter-comparison.md](../design/12-cli-adapter-comparison.md) — CLI Adapter 适配差异对比
- [13-preview-server.md](../design/13-preview-server.md) — 工作区预览服务（aiohttp 静态文件服务器）
- [14-pin-memory.md](../design/14-pin-memory.md) — 约束钉住与上下文注入（PinMemory）
- [15-merge-conflict-resolution.md](../design/15-merge-conflict-resolution.md) — Orchestrator Merge 冲突处理机制
- [16-system-prompt-tuning.md](../design/16-system-prompt-tuning.md) — 系统提示词动态组装
- [17-conversation-memory.md](../design/17-conversation-memory.md) — Orchestrator 跨轮推理记忆持久化
- [18-langfuse-trace.md](../design/18-langfuse-trace.md) — Langfuse Cloud 可观测性与隐私设计
- [19-skills-taskctl.md](../design/19-skills-taskctl.md) — Agent 共享上下文管理工具（taskctl CLI）
- [20-planning-context-module.md](../design/20-planning-context-module.md) — Planning ToolMessage 结构化
- [21-unpin-history-persistence.md](../design/21-unpin-history-persistence.md) — Pin 取消事件持久化 + save_mem_node 去重
- [00-architecture.md](../design/00-architecture.md) — 架构总览

### reference/

- [adapter-diff.md](adapter-diff.md) — 适配器差异对比
- [langfuse-trace.md](langfuse-trace.md) — Langfuse Cloud Tokyo 配置与使用
- [orchestrator-architecture.md](orchestrator-architecture.md) — Orchestrator 架构概览

### testing/

- [01-session-id-writeback.md](../testing/01-session-id-writeback.md) — CLI session_id 回写测试
- [02-workspace-git-ops.md](../testing/02-workspace-git-ops.md) — Workspace Git 操作测试
- [03-taskctl-merge.md](../testing/03-taskctl-merge.md) — taskctl merge 命令测试
- [04-orchestrator-planning.md](../testing/04-orchestrator-planning.md) — Orchestrator 规划测试

### backlog/

- [orchestrator-drawbacks.md](../backlog/orchestrator-drawbacks.md) — Orchestrator 已知缺陷与改进方向
- [session-persistence.md](../backlog/session-persistence.md) — 会话持久化设计笔记
- [system-architecture-and-frontend-cards.md](../backlog/system-architecture-and-frontend-cards.md) — 系统架构与前端卡片联动设计
- [trace-system.md](../backlog/trace-system.md) — Trace 可观测性系统设计笔记
