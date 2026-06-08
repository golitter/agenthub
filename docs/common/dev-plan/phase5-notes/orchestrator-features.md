# Orchestrator 最终实现功能清单

> 基于 agentend/ 现有代码 + architecture 文档，梳理 Orchestrator 的完整功能图谱。
> 标注每项功能的当前状态：✅ 已实现 / ✏️ 需重构 / 🆕 需新建

---

## 一、总览

Orchestrator 是 AgentEnd Runtime 的核心协调器，负责将用户需求拆解为多个 Task，调度不同 Agent 执行，收集结果并汇总。

**三层架构：** Conversation（对话语义）→ Routing（路由策略）→ Runtime（执行运行时）。

**最终形态：RuntimeCoordinator（Runtime Kernel）**

```
用户消息
    ↓
① Conversation Service     — 消息图、@mention、reply/quote、上下文加载
    ↓
② Routing Policy           — 简单路由（@mention/启发式） vs Orchestrator（复杂任务）
    ↓
③ Planner（LangGraph）     — LLM 拆解需求为 ExecutionPlan（仅复杂任务）
    ↓
④ Scheduler                — 按依赖顺序调度 Task
    ↓
⑤ ExecutionEngine          — spawn RuntimeAgent，调用 Adapter，normalize 事件
    ↓
⑥ MergeManager             — 合并各 Agent 分支
    ↓
⑦ Aggregator               — LLM 汇总执行结果
    ↓
⑧ Evolution Store          — 记录编排经验
    ↓
统一 RuntimeEvent Stream → SSE → 前端 Runtime Timeline
```

---

## 二、功能清单

### 2.1 Planner（任务规划）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | LLM 拆解用户需求为多 Task | ✅ | `graph.py` plan_node 调用 ChatOpenAI | 保持，加 profile 字段 |
| 2 | 生成 ExecutionPlan（overview + tasks） | ✅ | `models.py` PlanOutput + TaskDef | 扩展为 ExecutionPlan + ExecutionTask |
| 3 | 注入 Pin 约束到 Prompt | ✅ | `prompts.py` build_planner_prompt 调用 PinMemory | 保持 |
| 4 | 注入 Evolution 经验到 Prompt | ✅ | `prompts.py` 调用 EvolutionStore | 保持 |
| 5 | JSON 解析 + markdown code block 提取 | ✅ | `graph.py` _extract_json | 保持 |
| 6 | Plan 失败时返回 ERROR 事件 | ✅ | `orchestrator.py` plan=None 分支 | 保持 |
| 7 | 单次规划 Agent 分配上限（≤5）；一个群聊可多轮规划 | ✅ | PLAN_PROMPT 规则约束 | 保持 |
| 8 | Task 按执行顺序排列 | ✅ | PLAN_PROMPT 规则约束 | 保持 |
| 9 | 群聊 Agent 来源：建群时指定 / Orchestrator 自动选 / 混合动态拉入 | 🆕 | 仅 config.agents 限定列表 | 三种模式 |
| 10 | 多轮规划（一个群聊可多次触发规划） | 🆕 | 仅单次规划 | 多轮 |
| 11 | 单次规划内多轮协调（最多 10 轮，每轮可协调后重新规划） | 🆕 | 无 | Coordinator 循环 |

**当前产出：**

```python
PlanOutput(overview="...", tasks=[
    TaskDef(task_id="task-001", session_id="claude-code", title="...", content="...")
])
```

**目标产出：**

```python
ExecutionPlan(tasks=[
    ExecutionTask(id="task-001", profile="frontend-engineer", adapter="claude-code",
                  prompt="...", depends_on=[])
])
```

**改造点：** TaskDef.session_id → ExecutionTask.profile + adapter；TaskDef.content → ExecutionTask.prompt；新增 depends_on 字段。

---

### 2.2 Dispatcher（任务分发）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | 将 Plan 的 tasks 映射到具体 Agent | ✅ | `dispatcher.py` Dispatcher.dispatch() | 扩展：映射 profile → workspace_path |
| 2 | 生成 DispatchResult（含 workspace_path） | ✅ | DispatchResult 含 workspace_path | 扩展：含 branch 名 |
| 3 | 依赖关系传递 | ⚠️ | DispatchResult.depends_on 存在但未使用 | Phase 5 串行忽略，Phase 6 并行生效 |

**当前产出：**

```python
DispatchResult(task_id="task-001", agent="claude-code", mention="@claude-code",
               content="...", depends_on=[], workspace_path="")
```

**目标：** Dispatcher 从 Registry 查询 profile 信息，填充 workspace_path 和 branch。

---

### 2.3 Runtime State（运行时状态）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | Task 状态追踪（PENDING/RUNNING/COMPLETED/FAILED） | ✅ | `state.py` RuntimeState | 升级为 OrchestratorRuntime |
| 2 | Agent 执行结果存储 | ✅ | RuntimeState.results dict | 迁入 TaskRuntime |
| 3 | Running agent 追踪 | ✅ | RuntimeState.running_agents dict | 迁入 TaskRuntime |
| 4 | Runtime 整体状态 | 🆕 | 无 | OrchestratorRuntime.state |
| 5 | ExecutionPlan 作为 Source of Truth | 🆕 | 无 | OrchestratorRuntime.plan |
| 6 | SharedContext 管理 | 🆕 | 无 | OrchestratorRuntime.shared_context |
| 7 | 当前执行步骤追踪 | 🆕 | 无 | OrchestratorRuntime.current_step |

**当前状态模型（state.py）：**

```python
class RuntimeState:
    tasks: dict[str, TaskState]          # {task_id: PENDING/RUNNING/...}
    results: dict[str, str]              # {task_id: result_content}
    running_agents: dict[str, str]       # {agent_id: task_id}
```

**目标状态模型：**

```python
@dataclass
class OrchestratorRuntime:
    runtime_id: str
    state: RuntimeState                  # PLANNING / EXECUTING / COMPLETED / FAILED
    plan: ExecutionPlan                  # Source of Truth
    current_step: int
    task_states: dict[str, TaskRuntime]  # per-task runtime
    shared_context: SharedContext
```

---

### 2.4 Scheduler（执行调度）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | 串行执行 Task | ⚠️ | orchestrator.py 内联逻辑 | 抽象为独立 Scheduler 类 |
| 2 | 逐 Task spawn RuntimeAgent | 🆕 | 无（当前 mock 执行） | Scheduler.spawn + execute |
| 3 | 等待当前 Task 完成后调度下一个 | 🆕 | 无 | Scheduler.run() 串行遍历 |
| 4 | 产出统一 RuntimeEvent Stream | 🆕 | 无 | yield RuntimeEvent |
| 5 | DAG 拓扑排序（Phase 6 并行） | 🆕 | 无 | 数据结构 ready，执行逻辑 Phase 6 |

**当前问题：** OrchestratorAdapter 的 Phase 3（Collect）使用 `results_callback` 或 mock，没有真正调用 Agent Adapter 执行。

**目标：** Scheduler 遍历 ExecutionPlan，对每个 Task 通过 ExecutionEngine 调用真实 Agent Adapter，获取流式事件。

---

### 2.5 ExecutionEngine（执行引擎）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | 从 Registry 获取目标 Agent Adapter | 🆕 | 无 | ExecutionEngine.execute() |
| 2 | 调用 adapter.stream_chat() 获取流式输出 | 🆕 | 无 | async for event in adapter.stream() |
| 3 | normalize 各 Agent 原生事件为 RuntimeEvent | 🆕 | 无 | normalize(event, task) |
| 4 | 注入 Profile System Prompt | 🆕 | 无 | PromptRenderer.render(context) |
| 5 | 检查 Permission（spawn_agent 等） | 🆕 | 无 | ExecutionEngine 权限检查 |

**normalize 映射（需实现）：**

```
Claude Adapter StreamEvent → RuntimeEvent
  type=TEXT          → agent.delta
  type=TOOL_CALL     → agent.tool_call
  type=TOOL_RESULT   → agent.tool_result
  type=DONE          → (不转换，由 Scheduler 生成 task.completed)

OpenCode Adapter → 同上映射

Codex Adapter → 同上映射
```

---

### 2.6 Aggregator（结果汇总）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | LLM 汇总多 Agent 执行结果 | ✅ | `aggregator.py` Aggregator.aggregate() 调用 ChatOpenAI | 保持 |
| 2 | 结构化汇总报告（完成情况 + 关键产出 + 后续建议） | ✅ | _AGGREGATE_PROMPT 模板 | 保持 |
| 3 | 各 Task 成功/失败标记 | ✅ | TaskResult.success bool | 保持 |

**目标 Aggregator：**

```python
class Aggregator:
    async def aggregate(self, results: list[TaskResult], overview: str) -> str:
        # 调用 LLM 汇总，返回结构化报告
```

---

### 2.7 Evolution Store（编排经验）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | 记录每次编排经验到 evolution.yaml | ✅ | `evolution.py` EvolutionStore.record() | 保持 |
| 2 | 最近 N 次经验摘要注入 Planner Prompt | ✅ | EvolutionStore.get_recent_experience() | 保持 |
| 3 | Agent 性能记录（success + duration） | ✅ | record() 的 agent_performance 参数 | 保持 |
| 4 | 经验条目上限（20 条，FIFO） | ✅ | _MAX_ENTRIES = 20 | 保持 |

**存储格式（evolution.yaml）：**

```yaml
- timestamp: "2026-05-28T..."
  message_summary: "用 Claude Code 写登录页..."
  plan_summary: "拆解为 2 个任务..."
  results_summary: "成功完成，登录页已实现..."
  success: true
  agent_performance:
    - agent_id: claude-code
      success: true
      duration: 12.5
```

---

### 2.8 Pin Memory（用户约束）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | 用户 pin 约束文件 | ✅ | `pin_memory.py` PinMemory.pin() | 保持 |
| 2 | LLM 自动生成摘要 | ✅ | _generate_summary() 调用 LLM | 保持 |
| 3 | pin 已有文件 | ✅ | PinMemory.pin_existing() | 保持 |
| 4 | unpin 文件 | ✅ | PinMemory.unpin() | 保持 |
| 5 | 约束注入到 Planner Prompt | ✅ | PinMemory.get_context() | 保持 |
| 6 | API 端点暴露 pin/unpin | ✅ | /v1/pin 路由 | 保持 |

**存储格式（_pins.yaml）：**

```yaml
- filename: coding-standards.md
  title: 编码规范
  source: user
  pinned_at: "2026-05-28T..."
  summary: "TypeScript strict mode, 2-space indent..."
```

---

### 2.9 Profile System / SOUL（Agent 身份）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | Profile 目录结构（soul.yaml + system.md + rules.yaml） | 🆕 | 无 | 新建 profiles/ 目录 |
| 2 | soul.yaml 定义身份/职责/权限/工具/风格 | 🆕 | 无 | 新建 |
| 3 | Profile 加载和查询 | 🆕 | 无 | AgentRegistry.profiles |
| 4 | Capability-Based Permission 检查 | 🆕 | 无 | ExecutionEngine 检查 spawn_agent |
| 5 | Context Rendering Pipeline | 🆕 | 无 | PromptRenderer 替代拼字符串 |
| 6 | 初始 Profile：frontend-engineer | 🆕 | 无 | 新建 |
| 7 | 初始 Profile：reviewer | 🆕 | 无 | 新建 |

---

### 2.10 AgentRegistry（Agent 注册中心）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | Adapter 注册和查询 | ✅ | `adapters/registry.py` AdapterRegistry | 保持 |
| 2 | Profile 注册和查询 | 🆕 | 无 | 扩展 AgentRegistry |
| 3 | spawn RuntimeAgent（Profile + Adapter） | 🆕 | 无 | AgentRegistry.spawn() |
| 4 | RuntimeAgent 实例生命周期管理 | 🆕 | 无 | AgentRegistry 管理实例 |

**当前 AdapterRegistry：**

```python
class AdapterRegistry:
    _adapters: dict[str, type[BaseAgentAdapter]]   # {agent_type: AdapterClass}

    register(agent_type, adapter_cls)
    get(agent_type) → type[BaseAgentAdapter]
```

**目标 AgentRegistry：**

```python
class AgentRegistry:
    profiles: dict[str, AgentProfile]               # {profile_id: Profile}
    adapters: dict[str, type[BaseAgentAdapter]]     # {adapter_id: AdapterClass}

    spawn(profile, adapter) → RuntimeAgent          # 创建运行时实例
    get_adapter(adapter_id) → BaseAgentAdapter
    get_profile(profile_id) → AgentProfile
```

---

### 2.11 Workspace Isolation（工作区隔离）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | Git worktree 创建/删除 | ✅ | `workspace/git_ops.py` GitOps | 保持 |
| 2 | Per-session worktree 隔离 | ✅ | `workspace/models.py` Workspace | 保持，扩展为 per-RuntimeAgent |
| 3 | 独立 branch（agent/{session_id}/{task_id}） | ✅ | _generate_branch_name() | 保持 |
| 4 | Worktree 路径生成 | ✅ | _generate_worktree_path() | 保持 |
| 5 | Workspace 持久化（JSON store） | ✅ | `workspace/store.py` JsonFileWorkspaceStore | 保持 |
| 6 | 崩溃恢复（worktree reconciliation） | ✅ | `workspace/recovery.py` | 保持 |
| 7 | 定时清理 inactive session workspace | ✅ | WorkspaceManager._inactive_cleanup_loop() | 保持 |
| 8 | 技能供给（provision skills to worktree） | ✅ | SkillProvisioner.provision() | 保持 |
| 9 | Git exclude（排除 .agent/ 目录） | ✅ | GitOps.write_exclude() | 保持 |
| 10 | Task 级分支（task/{task_id}） | ✅ | task_branch_name() | 保持 |
| 11 | Branch 合并到 main | ✅ | GitOps.merge_branch() | 保持，扩展为 MergeManager |
| 12 | orchestrator 专属 workspace（无 repo） | 🆕 | 无 | workspaces/{task_id}/orchestrator/ |
| 13 | shared/ 共享上下文目录 | ✅ | SkillProvisioner.init_shared_dirs() | 保持，升级为 SharedContext |

**当前 Workspace 结构：**

```
worktrees/{task_id}/{session_id}/   ← 每个 session 独立 worktree
```

**目标 Workspace 结构：**

```
workspaces/{task_id}/
├── orchestrator/          ← 🆕 Orchestrator 专属空间（无 repo）
│   ├── runtime.json
│   ├── plan.json
│   └── events/
├── shared/                ← Agent 间共享上下文
│   ├── artifacts/
│   ├── memory/
│   └── ...
└── {session_agent_id}/    ← RuntimeAgent 工作区
    ├── repo/              ← Git worktree
    ├── state.json
    └── artifacts/
```

---

### 2.12 MergeManager（分支合并）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | Git merge 操作 | ✅ | GitOps.merge_branch() | 保持，包装为 MergeManager |
| 2 | RuntimeAgent branch → main 合并 | 🆕 | 无 | MergeManager.merge_agent_branch() |
| 3 | 合并冲突检测 | ⚠️ | merge_branch 失败时 abort | 返回 MergeResult 含冲突文件 |
| 4 | 冲突时生成新 Task | 🆕 | 无 | Phase 6: spawn reviewer 解冲突 |

---

### 2.13 统一 RuntimeEvent（事件流）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | SSE 事件发送（PLANNING / TEXT / DONE / ERROR） | ✅ | StreamEvent + EventType | 保持，扩展 |
| 2 | PLANNING 事件（规划进度） | ✅ | EventType.PLANNING | 保持 |
| 3 | TEXT 事件（流式文本） | ✅ | EventType.TEXT | 保持 |
| 4 | DONE 事件（完成） | ✅ | EventType.DONE | 保持 |
| 5 | ERROR 事件（错误） | ✅ | EventType.ERROR | 保持 |
| 6 | Runtime 级事件（started/completed/failed） | 🆕 | 无 | RuntimeEvent runtime.* |
| 7 | Task 级事件（queued/started/completed/failed） | 🆕 | 无 | RuntimeEvent task.* |
| 8 | Agent 级事件（delta/tool_call/tool_result） | 🆕 | 无 | RuntimeEvent agent.* |
| 9 | Workspace 级事件（branch.created/merge.*） | 🆕 | 无 | RuntimeEvent workspace.* |
| 10 | Artifact 级事件（created） | 🆕 | 无 | RuntimeEvent artifact.* |
| 11 | Coordination 级事件（协调通道消息） | 🆕 | 无 | RuntimeEvent coordination.* |

**当前 SSE 事件流（Orchestrator）：**

```
PLANNING (status="started")
PLANNING (node="plan")
PLANNING (node="write_shared")
PLANNING (node="dispatch", dispatch={...})     ← 每个 task 一个
TEXT (text=overview)
DONE (text=aggregated, files_written=[...])
```

**目标 RuntimeEvent 流：**

```
runtime.started
runtime.planning

coordination.started                           ← 🆕 协调通道开启
coordination.message (orchestrator → frontend-engineer: "需要 OAuth？")
coordination.message (frontend-engineer → orchestrator: "先做邮箱登录")
coordination.message (orchestrator → reviewer: "审查重点？")
coordination.message (reviewer → orchestrator: "密码/CSRF/XSS")
coordination.ended                             ← 🆕 协调结束

runtime.plan.completed                         ← 包含最终 ExecutionPlan（结合协调结果）
runtime.agent.spawned (agent-a1b2, profile=frontend-engineer)
workspace.branch.created (feature/task-001-ui)
task.started (task-001)
  agent.delta (...)                            ← 子事件
  agent.tool_call (...)                        ← 子事件
  agent.tool_result (...)                      ← 子事件
task.completed (task-001)
runtime.agent.spawned (agent-c3d4, profile=reviewer)
workspace.branch.created (feature/task-002-review)
task.started (task-002)
  agent.delta (...)
task.completed (task-002)
workspace.merge.started
workspace.merge.completed
runtime.completed
```

---

### 2.14 OrchestratorAdapter（对外接口）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | 实现 BaseAgentAdapter 接口 | ✅ | OrchestratorAdapter | 保持 |
| 2 | stream_chat() 返回 AsyncIterator[StreamEvent] | ✅ | 已实现 | 重构：内部用 RuntimeCoordinator |
| 3 | chat() 返回完整 AgentResponse | ✅ | 已实现 | 保持 |
| 4 | create/destroy session（无状态） | ✅ | 空实现 | 保持 |
| 5 | interrupt（暂不支持） | ✅ | 返回 False | Phase 6 |
| 6 | results_callback（外部回调） | ✅ | kwargs.get("results_callback") | 重构：Scheduler 内部执行 |
| 7 | 接收 config（agents/task_id/shared_dir） | ✅ | 从 kwargs 获取 | 扩展：含 profile 信息 |

---

### 2.15 Conversation Layer（对话层）

> 详细设计见 [conversation-layer-design.md](conversation-layer-design.md)

Conversation Layer 处理消息图、@mention、回复、引用、重新生成。
**核心原则：Conversation 语义不泄漏到 Runtime，Adapter 不暴露给 Conversation。**

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | @mention 解析（`@frontend` → profile） | 🆕 | 无 | `conversation/mentions.py` |
| 2 | 回复消息（`reply_to_message_id`） | 🆕 | 无 | `conversation/router.py` |
| 3 | 引用消息（`quote_message_ids`，传 ID 不传文本） | 🆕 | 无 | `conversation/context_builder.py`（辅助，主路径走 taskctl skill） |
| 4 | 重新生成（同一消息多次 Execution） | 🆕 | 无 | `conversation/regenerate.py` |
| 5 | 服务端加载对话上下文（只传 `conversation_id`） | 🆕 | 无 | `conversation/store.py` |
| 6 | ExecutionRecord（Message ≠ Execution） | 🆕 | 无 | `conversation/models.py` |
| 7 | ConversationService（串联以上） | 🆕 | 无 | `conversation/service.py` |

**AgentRequest 扩展：**

```python
class AgentRequest(BaseModel):
    # 现有字段不变
    ...
    # 新增 conversation 层
    conversation_id: str                       # 对话 ID（替代前端传 history）
    target_profile: str | None = None          # @mention 指定 profile（不是 adapter）
    reply_to_message_id: str | None = None     # 回复的消息 ID
    quote_message_ids: list[str] = []          # 引用的消息 ID 列表（不是文本）
    regenerate_message_id: str | None = None   # 重新生成目标消息 ID
```

**关键设计决策：**
- `target_profile` 不用 `target_agent` — adapter 是 implementation detail
- `conversation_id` 替代 `conversation_history` — 服务端自己加载上下文
- `quote_message_ids` 不用 `quote: str` — quote 是 reference 不是文本

---

### 2.16 Routing Policy（路由策略）

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | 简单路由（@mention → 直接 profile） | 🆕 | 无 | `routing/simple_router.py` |
| 2 | 启发式路由（简单问题 → 单 Agent） | 🆕 | 无 | `routing/simple_router.py` |
| 3 | Orchestrator 路由（复杂任务 → 编排） | 🆕 | 无 | `routing/orchestrator_router.py` |
| 4 | RoutingPolicy 统一接口 | 🆕 | 无 | `routing/policies.py` |

**为什么不让 Orchestrator 做简单路由：**

如果所有请求都过 Orchestrator，Orchestrator 会退化成 message router。
Orchestrator 只做复杂编排，简单问题用 SimpleRoutingPolicy 直接路由。

```python
class RoutingPolicy:
    def route(self, message: str, target_profile: str | None) -> RouteResult:
        if target_profile:
            return RouteResult(mode=DIRECT, profile=target_profile)
        if self._is_simple(message):
            return RouteResult(mode=DIRECT, profile=self._pick_best(message))
        return RouteResult(mode=ORCHESTRATOR)
```

---

### 2.17 Agent 间上下文共享（Skill-based）

> **核心认知：上下文共享的主要机制是 Skill（taskctl），不是 Prompt 注入。**

Agent 间的上下文共享分三层，**主要靠 Agent 自己通过 skill 按需拉取，不是靠 Runtime 往 prompt 里塞**：

| 层 | 机制 | 谁主动 | 优先级 |
|---|---|---|---|
| **Skill（主要）** | taskctl summary / common-memory / ls | Agent 按需拉取 | 主要 |
| **Git（文件）** | worktree merge 到 task 分支 | 自动可见 | 主要 |
| **Prompt（辅助）** | ContextBuilder 注入最少必要信息 | Runtime 推送 | 辅助 |

#### Skill 层：taskctl（已实现）

每个 Agent 启动时，SkillProvisioner 已将 taskctl 注入 workspace：

```
worktrees/{task_id}/{session_id}/
└── .claude/skills/taskctl/      ← 自动注入
    ├── SKILL.md                 ← 使用说明
    └── taskctl                  ← Go 二进制
```

Agent 可以自主调用：

```
taskctl summary           → 看到之前 Agent 做了什么（读 config.yaml + plans/）
taskctl common-memory     → 读共享记忆
taskctl sub-memory        → 读自己的私有记忆
taskctl write-sub-memory  → 写私有记忆
taskctl ls                → 看文件结构
taskctl merge             → 安全合并分支
```

#### Git 层：worktree 分支策略（已实现）

```
repo (main)
├── task/task-001                       ← task 级分支（共享基线）
│   ├── agent/{session-a}/{task-001}    ← Agent A 的分支
│   └── agent/{session-b}/{task-001}    ← Agent B 的分支

Agent A 执行完 → taskctl merge → 合并到 task/task-001
Agent B 的 worktree 基于 task/task-001 → 自动能看到 A 的代码
```

#### Prompt 层：ContextBuilder（辅助，仅注入最少信息）

Skill 和 Git 覆盖了大部分上下文共享。ContextBuilder 只注入 Agent 无法通过 skill 获取的信息：

```python
class ContextBuilder:
    def build(self, request, conversation) -> str:
        parts = []

        # 只注入对话级上下文（skill 拿不到的）
        if request.reply_to_message_id:
            msg = conversation.get_message(request.reply_to_message_id)
            parts.append(f"> 回复 [{msg.profile}]: {msg.content[:200]}")

        if request.quote_message_ids:
            for msg_id in request.quote_message_ids:
                msg = conversation.get_message(msg_id)
                parts.append(f"> 引用 [{msg.profile}]: {msg.content[:200]}")

        parts.append(request.message)
        return "\n\n".join(parts)
```

不注入：Agent 执行历史、产物文件列表、共享记忆 — 这些 Agent 自己通过 taskctl 获取。

---

### 2.18 Coordination Channel（协调通道）

> Orchestrator 规划时可以和 Agent 真实对话，澄清疑问后再开始执行。

**场景：** Orchestrator 拆解任务时遇到不明确的地方，通过协调通道向对应 Agent 提问，Agent 通过真实 Adapter CLI 回答。对话结束后 Orchestrator 再正式 @Agent 开始执行。

**UI 表现：** 协调消息显示在主聊天区域的一个**可折叠中间窗口**（类似视频会议聊天面板），默认收起，用户手动展开查看。主聊天只显示摘要。

```
[主聊天窗口]
Orchestrator: 正在规划...
  ┌─ 协调通道（点击展开）──────────────────┐
  │ Orchestrator → @frontend:               │
  │   "登录页需要支持 OAuth 还是邮箱？"      │
  │                                          │
  │ @frontend → Orchestrator:                │
  │   "建议先做邮箱登录，OAuth 后续迭代"     │
  │                                          │
  │ Orchestrator → @reviewer:                │
  │   "审查时重点关注哪些？"                  │
  │                                          │
  │ @reviewer → Orchestrator:                │
  │   "密码存储、CSRF、XSS"                  │
  └──────────────────────────────────────────┘
Orchestrator: 规划完成，开始分配任务
  @frontend 写登录页（邮箱登录）...          ← 正式执行
  @reviewer 审查安全性（密码/CSRF/XSS）...   ← 正式执行
```

**AgentEnd 实现：**

| # | 功能 | 状态 | 说明 |
|---|------|------|------|
| 1 | Orchestrator 识别需要澄清的问题 | 🆕 | Planner prompt 增加"不确定时生成 consultation 请求"规则 |
| 2 | 真实调用 Agent CLI 回答协调问题 | 🆕 | 通过 ExecutionEngine 调用 adapter.chat()（非流式，短对话） |
| 3 | 协调消息通过 SSE 推送到前端 | 🆕 | 新增 RuntimeEvent 类型 `coordination.message` |
| 4 | 协调结果注入 Planner 重新规划 | 🆕 | 协调对话作为额外 context 重新调用 Planner |
| 5 | 协调结束 → 正式执行 | 🆕 | Orchestrator @Agent 开始实施 |

**执行流（多轮规划，最多 10 轮）：**

```
第 1 轮：Planner 分析需求
    ↓
  需要澄清？
    ├── 否 → 直接生成 ExecutionPlan → 正式执行
    └── 是 → 生成 ConsultationRequest → 进入协调
              ↓
          真实调用 Agent CLI 回答
              ↓
          协调结果 + 原始需求 + 历史协调记录
              ↓
第 2 轮：Planner 重新规划
    ↓
  还有疑问？
    ├── 否 → 生成 ExecutionPlan → 正式执行
    └── 是 → 再次协调 → 第 3 轮 ...
              ↓
          ...
              ↓
第 N 轮（N ≤ 10）：强制生成 ExecutionPlan（不再协调）
```

```python
_MAX_PLANNING_ROUNDS = 10

class Coordinator:
    async def plan_with_coordination(self, message, agents, shared_dir):
        coordination_history = []

        for round_num in range(1, _MAX_PLANNING_ROUNDS + 1):
            # Planner 本轮规划
            result = await self.planner.plan(message, agents, shared_dir, coordination_history)

            if result.plan is not None:
                # 规划完成，不需要更多协调
                return result

            if result.consultation_requests and round_num < _MAX_PLANNING_ROUNDS:
                # 需要协调：真实调用 Agent CLI
                yield RuntimeEvent(type="coordination.started", round=round_num)

                for req in result.consultation_requests:
                    yield RuntimeEvent(type="coordination.message",
                                       from="orchestrator", to=req.profile, content=req.question)

                    # 真实调用 Agent
                    adapter = self.registry.get_adapter(req.profile)
                    answer = await adapter.chat(session_id=req.profile, message=req.question)

                    yield RuntimeEvent(type="coordination.message",
                                       from=req.profile, to="orchestrator", content=answer)

                    coordination_history.append({
                        "round": round_num,
                        "profile": req.profile,
                        "question": req.question,
                        "answer": answer,
                    })

                yield RuntimeEvent(type="coordination.ended", round=round_num)
                # 继续下一轮规划（coordination_history 会注入 prompt）
            else:
                # 第 10 轮强制生成（不再协调）
                break

        # 兜底：强制生成最终 ExecutionPlan
        result = await self.planner.force_plan(message, agents, coordination_history)
        return result
```

**新增事件类型：**

```
coordination.started                        ← 协调开始
coordination.message (from/to/content)      ← 每条协调消息
coordination.ended                          ← 协调结束
```

**前端渲染：**

- `coordination.started` → 渲染折叠面板
- `coordination.message` → 面板内追加消息
- `coordination.ended` → 面板折叠，主聊天显示摘要
- 面板默认收起，用户点击展开可查看完整对话

---

### 2.19 Session 管理

> Phase 5 保留 session，但**不继续强化 session 概念**。
> 未来 session 退化为 provider runtime transport（内部细节）。
> 核心业务对象转移到 conversation + execution。

| # | 功能 | 状态 | 当前实现 | 目标实现 |
|---|------|------|----------|----------|
| 1 | Session 状态机（IDLE → RUNNING → COMPLETED/ERROR） | ✅ | `session/models.py` Session | 保持，不加强 |
| 2 | 状态转换校验 | ✅ | _VALID_TRANSITIONS dict | 保持 |
| 3 | Session 创建/查询/销毁 | ✅ | SessionManager | 保持 |
| 4 | CLI session 映射持久化 | ✅ | SessionMappingStore | 保持 |
| 5 | 进程管理（terminate/kill） | ✅ | SessionManager.destroy() | 保持 |

---

## 三、文件与功能对照表

### 已有文件 → 功能映射

| 文件 | 负责功能 | 改动 |
|------|----------|------|
| `orchestrator/graph.py` | Planner（LangGraph plan → write_shared） | ✏️ write_shared_node 改为生成 ExecutionPlan 内存对象 |
| `orchestrator/models.py` | 数据模型（PlanOutput, TaskDef, TaskResult, DispatchResult） | ✏️ 新增 ExecutionPlan, ExecutionTask |
| `orchestrator/prompts.py` | Planner Prompt + build_planner_prompt | ✏️ 保持，profile 信息注入 |
| `orchestrator/dispatcher.py` | 任务分发 | ✏️ 扩展 profile/branch 信息 |
| `orchestrator/aggregator.py` | 结果汇总 | ✅ Aggregator + _AGGREGATE_PROMPT，功能正确 |
| `orchestrator/evolution.py` | 编排经验记录 | ✅ 保持 |
| `orchestrator/pin_memory.py` | 用户约束管理 | ✅ 保持 |
| `orchestrator/state.py` | RuntimeState（Task 状态追踪） | ✏️ 升级为 OrchestratorRuntime |
| `adapters/orchestrator.py` | 对外接口（OrchestratorAdapter） | ✏️ 重构：使用 RuntimeCoordinator |
| `workspace/manager.py` | 工作区创建/清理/合并 | ✏️ 扩展 per-RuntimeAgent |
| `workspace/git_ops.py` | Git 操作（worktree/branch/merge） | ✅ 保持 |
| `workspace/models.py` | Workspace 数据模型 | ✏️ 扩展 |
| `workspace/store.py` | Workspace 持久化 | ✅ 保持 |
| `workspace/recovery.py` | 崩溃恢复 | ✅ 保持 |
| `workspace/db.py` | DB 查询（inactive cleanup） | ✅ 保持 |
| `adapters/registry.py` | Adapter 注册 | ✏️ 扩展为 AgentRegistry |
| `adapters/base.py` | Adapter 基类 | ✅ 保持 |
| `session/manager.py` | Session 管理 | ✅ 保持 |
| `session/models.py` | Session 数据模型 | ✅ 保持 |
| `session/store.py` | CLI session 映射 | ✅ 保持 |

### 需新建文件 → 功能映射

| 文件 | 负责功能 | 层 |
|------|----------|---|
| **Conversation Layer** | | |
| `conversation/models.py` | Message, ExecutionRecord, Conversation | Conversation |
| `conversation/store.py` | ConversationStore（服务端加载上下文） | Conversation |
| `conversation/router.py` | MessageRouter（@mention 解析 + 路由决策） | Conversation |
| `conversation/mentions.py` | @mention 解析 + profile 映射 | Conversation |
| `conversation/context_builder.py` | 构建 Agent 可用上下文（reply/quote/history） | Conversation |
| `conversation/regenerate.py` | Regenerate 逻辑（新 Execution） | Conversation |
| `conversation/service.py` | ConversationService（串联以上） | Conversation |
| **Routing Layer** | | |
| `routing/policies.py` | RoutingPolicy + RoutingMode + RouteResult | Routing |
| `routing/simple_router.py` | SimpleRoutingPolicy（单 Agent 直接路由） | Routing |
| `routing/orchestrator_router.py` | OrchestratorRouter（复杂任务编排） | Routing |
| **Runtime Layer** | | |
| `runtime/events.py` | 统一 RuntimeEvent + 事件类型枚举 | Runtime |
| `runtime/models.py` | RuntimeAgent, Permissions, AgentContext | Runtime |
| `runtime/registry.py` | AgentRegistry（profiles + adapters + spawn） | Runtime |
| `runtime/context.py` | PromptRenderer（Context Rendering Pipeline） | Runtime |
| **Orchestrator** | | |
| `orchestrator/coordinator.py` | RuntimeCoordinator（Runtime Kernel） | Runtime |
| `orchestrator/runtime.py` | OrchestratorRuntime, TaskRuntime | Runtime |
| `orchestrator/scheduler.py` | Scheduler（串行/并行调度） | Runtime |
| `orchestrator/execution.py` | ExecutionEngine + normalize | Runtime |
| `orchestrator/merge.py` | MergeManager | Runtime |
| **Profiles** | | |
| `profiles/frontend-engineer/soul.yaml` | 前端工程师 Profile | Identity |
| `profiles/frontend-engineer/system.md` | 前端工程师 System Prompt | Identity |
| `profiles/frontend-engineer/rules.yaml` | 前端工程师规则 | Identity |
| `profiles/reviewer/soul.yaml` | 审查员 Profile | Identity |
| `profiles/reviewer/system.md` | 审查员 System Prompt | Identity |
| `profiles/reviewer/rules.yaml` | 审查员规则 | Identity |

---

## 四、实施优先级

### P0 — 必须实现（Phase 5 MVP）

**Conversation + Routing 层：**

1. **ConversationService** — `conversation/service.py` + `models.py` + `store.py`
   服务端加载对话上下文，ExecutionRecord（Message ≠ Execution）。

2. **MessageRouter + Mentions** — `conversation/router.py` + `mentions.py`
   @mention 解析（`@frontend` → profile），`target_profile` 路由。

3. **RoutingPolicy** — `routing/policies.py` + `simple_router.py`
   简单路由（@mention / 启发式）vs Orchestrator 路由（复杂任务）。

4. **ContextBuilder** — `conversation/context_builder.py`
   构建 Agent 可用上下文（reply/quote/history 注入 enhanced message）。

**Runtime 层：**

5. **统一 RuntimeEvent** — `runtime/events.py`
   所有后续模块依赖此事件模型。先定义事件类型和 RuntimeEvent 数据结构。

6. **Profile System** — `profiles/` + `runtime/models.py`
   ExecutionPlan 依赖 Profile。先建 2 个初始 Profile（frontend-engineer、reviewer）。

7. **AgentRegistry** — `runtime/registry.py`
   扩展现有 AdapterRegistry，加 Profile 注册和 spawn 能力。

8. **Runtime 数据结构** — `orchestrator/runtime.py` + 扩展 `models.py`
   OrchestratorRuntime、TaskRuntime、ExecutionPlan、ExecutionTask。

9. **ExecutionEngine** — `orchestrator/execution.py`
   从 Registry 获取 Adapter，调用 stream_chat()，normalize 事件。

10. **Scheduler** — `orchestrator/scheduler.py`
    串行遍历 ExecutionPlan，spawn RuntimeAgent，execute Task。

11. **重构 OrchestratorAdapter** — `adapters/orchestrator.py`
    串联新流程，替代当前 mock 模式。

### P1 — 应该实现（Phase 5）

12. **Context Rendering Pipeline** — `runtime/context.py`
    PromptRenderer 替代拼字符串。

13. **Workspace Isolation 升级** — 扩展 `workspace/manager.py`
    orchestrator/ 专属空间、per-RuntimeAgent 工作区。

14. **MergeManager 基础版** — `orchestrator/merge.py`
    merge 成功路径，冲突留 Phase 6。

15. **Reply / Quote** — `conversation/router.py` 扩展
    reply_to_message_id + quote_message_ids 上下文注入。

16. **Regenerate** — `conversation/regenerate.py`
    基于同一 trigger message 创建新 Execution。

### P2 — 可以后做（Phase 6+）

17. **RuntimeCoordinator** — `orchestrator/coordinator.py`
    整合 Scheduler + MergeManager + Replanner 为 Runtime Kernel。

18. **并行执行** — Scheduler DAG 拓扑排序。

19. **冲突自动处理** — 冲突时 spawn reviewer Task。

20. **Dynamic Replanning** — 执行过程中根据结果重新规划。

21. **Retry / Cancellation** — Task 级别重试和取消。

22. **更多 Profile** — architect、tester。

---

## 五、群聊 Agent 来源：三种模式

### 模式 A：建群时指定 Agent 列表

用户建群时选好成员，Orchestrator 只能从这些成员中分配任务。

```python
config = {
    "agents": [
        {"profile": "frontend-engineer", "adapter": "claude-code", "name": "Claude Code"},
        {"profile": "reviewer", "adapter": "opencode", "name": "OpenCode"},
    ]
}
```

Planner Prompt 只展示这些 Agent：

```
- **frontend-engineer**（Claude Code）: 代码生成与编辑
- **reviewer**（OpenCode）: 代码审查与优化
```

### 模式 B：只拉 Orchestrator，自动从全局选

```python
config = {
    "agents": []   # 不指定
}
```

Orchestrator 从全局 Registry 的所有 Profile 中自动选择。

### 模式 C：混合 — 初始成员 + 允许动态拉入

```python
config = {
    "agents": [
        {"profile": "frontend-engineer", "adapter": "claude-code", "name": "Claude Code"},
    ],
    "allow_dynamic_spawn": True    # Orchestrator 可以拉新 Agent 进群
}
```

Orchestrator 可以在规划时引入初始列表之外的 Agent（如自动拉 reviewer）。

### Prompt 中的 Agent 展示

Planner Prompt 展示的是 **Agent 的名字**（name 字段），不是角色描述：

```
# ❌ 错误：用角色描述
- frontend-engineer（前端工程师）: ...
- reviewer（审查员）: ...

# ✅ 正确：用 Agent 名字
- frontend-engineer（Claude Code）: 代码生成与编辑
- reviewer（OpenCode）: 代码审查与优化
```

名字来源链路：`soul.yaml → identity.role` 或 `config.agents[].name`。

---

## 六、Bug 记录

### ~~aggregator.py 内容被覆盖~~ — 已验证无此问题

经核实，`aggregator.py` 内容是正确的 `Aggregator` 类实现（含 `_AGGREGATE_PROMPT` + `aggregate()` 方法），与 `evolution.py` 的 `EvolutionStore` 不重复。此为文档撰写时的误判。
