# 群聊 / 多 Agent 编排实现计划（AgentEnd + Go Backend）

> 基于 demo.html 展示的群聊交互，规划 AgentEnd + Go Backend 端的实现方案。
> AgentEnd 是核心运行时，Go Backend 是 SSE 透明网关。
> 新事件类型（runtime.*, coordination.*）通过 Redis Stream 自动透传，无需特殊处理。

---

## 一、现状分析

### 当前架构

```
API → RuleEngine → AdapterRegistry → OrchestratorAdapter
                                          ↓
                                    LangGraph (plan_node → write_shared_node)
                                          ↓
                                    Dispatcher (task→agent 映射)
                                          ↓
                                    Mock 执行（不调用真实 Agent CLI）
                                          ↓
                                    Aggregator → EvolutionStore
```

### 关键差距

| 功能 | 当前 | 目标 |
|------|------|------|
| Agent 执行 | Mock（无真实调用） | 调用真实 Claude/OpenCode CLI |
| 协调通道 | 无 | 多轮（≤10）咨询，真实 adapter.chat() |
| Profile 系统 | 无 | AgentProfile（身份/权限/约束） |
| 任务调度 | 简单 for 循环 | Scheduler + ExecutionEngine |
| 事件类型 | 8 种基础事件 | 20+ 种 RuntimeEvent（含协调） |
| 消息上下文 | 前端传 history | 服务端 conversation_id 加载 |
| Agent 命名 | adapter type（claude-code） | 用户自定义名称（前端小助手） |

---

## 二、实现计划（按依赖顺序）

### Step 1: 数据模型层

#### 1.1 `src/runtime/models.py` — Profile + RuntimeAgent

```python
class AgentProfile(BaseModel):
    id: str                          # "frontend-engineer"
    display_name: str                # "前端工程师"
    adapter_type: str                # "claude-code"
    responsibilities: list[str]
    constraints: list[str]
    permissions: dict                # write_workspace, review_code 等

class RuntimeAgent(BaseModel):
    session_agent_id: str            # 运行时生成的唯一 ID
    profile: AgentProfile
    adapter_type: str
    workspace_path: str = ""
    state: Literal["idle","running","completed","failed"]
```

#### 1.2 `src/runtime/events.py` — RuntimeEvent

新增事件类型，SSE 格式兼容（`event: runtime.started`, `data: JSON`）：

```python
class RuntimeEventType(str, Enum):
    # runtime.*
    RUNTIME_STARTED = "runtime.started"
    RUNTIME_PLANNING = "runtime.planning"
    RUNTIME_PLAN_COMPLETED = "runtime.plan_completed"
    RUNTIME_COMPLETED = "runtime.completed"
    RUNTIME_FAILED = "runtime.failed"
    # coordination.*
    COORDINATION_STARTED = "coordination.started"
    COORDINATION_MESSAGE = "coordination.message"
    COORDINATION_TYPING = "coordination.typing"
    COORDINATION_ENDED = "coordination.ended"
    # task.*
    TASK_STARTED = "task.started"
    TASK_COMPLETED = "task.completed"
    TASK_FAILED = "task.failed"
    # agent.*
    AGENT_SPAWNED = "agent.spawned"
    AGENT_DELTA = "agent.delta"
    AGENT_TOOL_CALL = "agent.tool_call"
    AGENT_TOOL_RESULT = "agent.tool_result"
    # workspace.*
    WORKSPACE_BRANCH_CREATED = "workspace.branch.created"
    WORKSPACE_MERGE_STARTED = "workspace.merge.started"
    WORKSPACE_MERGE_COMPLETED = "workspace.merge.completed"
    WORKSPACE_MERGE_CONFLICT = "workspace.merge.conflict"

class RuntimeEvent(BaseModel):
    type: str
    runtime_id: str
    task_id: str | None = None
    agent: str | None = None         # profile display_name
    payload: dict = {}
    timestamp: float
```

#### 1.3 `src/orchestrator/models.py` — 扩展（保留旧模型）

```python
# 新增
class ExecutionTask(BaseModel):
    id: str                           # "task-001"
    profile: str                      # "frontend-engineer"
    adapter: str                      # "claude-code"
    prompt: str
    depends_on: list[str] = []        # DAG ready

class ExecutionPlan(BaseModel):
    overview: str
    tasks: list[ExecutionTask]

class ConsultationRequest(BaseModel):
    profile: str
    question: str

class PlannerResult(BaseModel):
    plan: ExecutionPlan | None = None
    consultation_requests: list[ConsultationRequest] = []

class MergeResult(BaseModel):
    success: bool
    conflict_files: list[str] = []
```

#### 1.4 `src/orchestrator/runtime.py` — 运行时状态

```python
class OrchestratorRuntimeState(str, Enum):
    PLANNING = "planning"
    COORDINATING = "coordinating"
    EXECUTING = "executing"
    MERGING = "merging"
    COMPLETED = "completed"
    FAILED = "failed"

class TaskRuntime(BaseModel):
    id: str
    profile: str
    adapter: str
    state: TaskState                  # 复用 state.py
    result: str | None = None
    workspace_path: str = ""

class OrchestratorRuntime(BaseModel):
    runtime_id: str
    state: OrchestratorRuntimeState
    plan: ExecutionPlan | None = None
    task_states: dict[str, TaskRuntime] = {}
    shared_dir: str = ""
```

---

### Step 2: Registry 层 — Profile → Adapter 映射

#### 2.1 `src/runtime/registry.py`

```python
class AgentRegistry:
    def __init__(self, adapter_registry: AdapterRegistry): ...
    def register_profile(self, profile: AgentProfile): ...
    def get_profile(self, profile_id: str) -> AgentProfile: ...
    def get_adapter_cls(self, adapter_type: str) -> type[BaseAgentAdapter]: ...
    def spawn(self, profile_id: str, adapter_type: str, task_id: str) -> RuntimeAgent: ...
```

#### 2.2 App Wiring

- `src/app/dependencies.py`: 新增 `create_agent_registry()`
- `src/app/main.py`: 启动时实例化 `AgentRegistry`
- `src/api/dependencies.py`: 新增 `get_agent_registry` 依赖注入

---

### Step 3: Coordination Channel（核心新功能）

#### 3.1 `src/orchestrator/coordinator.py`

多轮协调循环（最多 10 轮）：

```python
class Coordinator:
    MAX_ROUNDS = 10

    async def plan_with_coordination(
        self, message: str, agents: list[dict]
    ) -> AsyncIterator[RuntimeEvent]:
        """
        循环：
        1. 调用 planner → 得到 PlannerResult
        2. 如果有 plan → yield plan_completed, return plan
        3. 如果有 consultation_requests:
           - yield coordination.started(round=N)
           - 对每个 request:
             - yield coordination.message(from=orchestrator, to=profile)
             - yield coordination.typing(profile)
             - 调用真实 adapter.chat() 获取回答
             - yield coordination.message(from=profile, to=orchestrator)
           - yield coordination.ended(round=N)
           - 追加到 coordination_history
        4. 下一轮
        """
```

**关键机制**：
- 咨询用 `adapter.chat()`（非流式，完整回答）
- 执行用 `adapter.stream_chat()`（流式，实时事件）
- 协调历史注入下一轮 planner prompt

#### 3.2 修改 `src/orchestrator/prompts.py`

新增 `COORDINATION_PLAN_PROMPT`：在 PLAN_PROMPT 基础上增加：
- `consultation_requests` 输出格式
- `coordination_history` 注入区
- 指示 LLM 在不确定时输出问题而非计划

新增 `build_coordination_planner_prompt(message, agents_desc, shared_dir, coordination_history)` 函数。

#### 3.3 修改 `src/orchestrator/graph.py`

新增 `coordination_plan_node`（使用新 prompt，返回 `PlannerResult`）。新增 `build_coordination_graph()`。

---

### Step 4: Execution Engine（真实 Agent 调用）

#### 4.1 `src/orchestrator/execution.py`

```python
class ExecutionEngine:
    async def execute(
        self, task: ExecutionTask, agent: RuntimeAgent
    ) -> AsyncIterator[RuntimeEvent]:
        adapter_cls = registry.get_adapter_cls(agent.adapter_type)
        adapter = adapter_cls()
        async for event in adapter.stream_chat(session_id, prompt, cwd=workspace):
            yield self._normalize(event, agent)
```

**Normalize 映射表**：

| StreamEvent | → RuntimeEvent |
|-------------|---------------|
| `EventType.TEXT` | `AGENT_DELTA` |
| `EventType.TOOL_CALL` | `AGENT_TOOL_CALL` |
| `EventType.TOOL_RESULT` | `AGENT_TOOL_RESULT` |
| `EventType.ERROR` | `TASK_FAILED` |
| `EventType.DONE` | _(跳过，Scheduler 处理)_ |
| `EventType.INIT` | _(捕获 cli_session_id，不转发)_ |

---

### Step 5: Scheduler（串行执行编排）

#### 5.1 `src/orchestrator/scheduler.py`

```python
class Scheduler:
    async def run(
        self, plan: ExecutionPlan, runtime_id: str,
        repo_path: str, task_id: str
    ) -> AsyncIterator[RuntimeEvent]:
        for task in plan.tasks:            # Phase 5: 串行
            # 1. Spawn agent
            agent = registry.spawn(task.profile, task.adapter, task_id)
            # 2. Create workspace worktree
            ws = workspace_mgr.create(repo_path, task_id, agent.session_agent_id, task.adapter)
            # 注：当前代码使用 worktrees/，Phase 5 迁移为 workspaces/
            # 3. Yield lifecycle events
            yield RUNTIME: agent.spawned + workspace.branch.created + task.started
            # 4. Execute (stream)
            async for event in engine.execute(task, agent):
                yield event
            # 5. Complete task
            yield task.completed
            # 6. Merge branch
            yield workspace.merge.started
            merged = workspace_mgr.merge(ws)
            yield workspace.merge.completed/conflict
            # 7. Cleanup
```

---

### Step 6: MergeManager

#### 6.1 `src/orchestrator/merge.py`

薄封装 `WorkspaceManager.merge()`，返回 `MergeResult(success, conflict_files)`。
Phase 5 不做自动冲突解决（生成新 Task 是 Phase 6）。

---

### Step 7: OrchestratorAdapter 重构

#### 7.1 修改 `src/adapters/orchestrator.py`

旧 5 阶段（Planning → Dispatch → MockCollect → Aggregate → Record）→ 新 pipeline：

```
stream_chat():
  1. yield RUNTIME_STARTED
  2. yield RUNTIME_PLANNING
  3. Coordinator.plan_with_coordination()
     → yield 所有协调事件 + 最终计划
  4. Scheduler.run(plan)
     → yield 所有执行事件 + 合并事件
  5. Aggregator.aggregate(results)
  6. EvolutionStore.record()
  7. yield RUNTIME_COMPLETED
```

旧的 `Dispatcher` 和 `RuntimeState` 保留但不再使用。

---

### Step 8: API 层变更

#### 8.1 `src/schemas/request.py` — 新增字段（向后兼容）

```python
class AgentRequest(_AgentRequest):
    # 现有
    rules: list[str] = Field(default_factory=list)
    config: dict | None = None
    # 新增：conversation 层
    conversation_id: str | None = None
    target_profile: str | None = None
    reply_to_message_id: str | None = None
    quote_message_ids: list[str] = Field(default_factory=list)
    regenerate_message_id: str | None = None
```

#### 8.2 `src/api/v1/agent.py`

- `_orchestrator_kwargs()` 扩展：传递 `repo_path`、`agent_registry`
- `_execute_stream()` 对 RuntimeEvent 直接 yield SSE

#### 8.3 `src/app/dependencies.py`

新增 `get_agent_registry` FastAPI 依赖。

---

### Step 9: Conversation + Routing（优先级较低）

#### 9.1 `src/conversation/` — 对话层

| 文件 | 职责 |
|------|------|
| `models.py` | Message, ExecutionRecord, Conversation |
| `store.py` | 内存 ConversationStore |
| `mentions.py` | MentionParser（从消息提取 @profile） |
| `context_builder.py` | ContextBuilder（注入 reply/quote 上下文） |
| `service.py` | ConversationService（串联以上） |

#### 9.2 `src/routing/` — 路由层

| 文件 | 职责 |
|------|------|
| `policies.py` | RoutingMode(DIRECT/ORCHESTRATOR), RoutingPolicy |
| `simple_router.py` | @mention 直接路由到指定 profile |
| `orchestrator_router.py` | 复杂任务走 Orchestrator 编排 |

---

## 三、文件清单

### 新增文件（19 个）

**P0（核心路径）：**

| 文件 | 职责 |
|------|------|
| `src/runtime/__init__.py` | 包 |
| `src/runtime/models.py` | AgentProfile, RuntimeAgent |
| `src/runtime/events.py` | RuntimeEventType, RuntimeEvent |
| `src/runtime/registry.py` | AgentRegistry（profile + adapter） |
| `src/orchestrator/coordinator.py` | Coordinator（多轮协调） |
| `src/orchestrator/scheduler.py` | Scheduler（串行执行） |
| `src/orchestrator/execution.py` | ExecutionEngine + normalize |
| `src/orchestrator/runtime.py` | OrchestratorRuntime 状态 |

**P1（扩展功能）：**

| 文件 | 职责 |
|------|------|
| `src/orchestrator/merge.py` | MergeManager |
| `src/conversation/__init__.py` | 包 |
| `src/conversation/models.py` | Message, ExecutionRecord, Conversation |
| `src/conversation/store.py` | ConversationStore |
| `src/conversation/mentions.py` | MentionParser |
| `src/conversation/context_builder.py` | ContextBuilder |
| `src/conversation/service.py` | ConversationService |
| `src/routing/__init__.py` | 包 |
| `src/routing/policies.py` | RoutingMode, RoutingPolicy |
| `src/routing/simple_router.py` | SimpleRoutingPolicy |
| `src/routing/orchestrator_router.py` | OrchestratorRouter |

### 修改文件（9 个）

| 文件 | 变更 |
|------|------|
| `src/orchestrator/models.py` | 新增 ExecutionTask, ExecutionPlan, ConsultationRequest, PlannerResult, MergeResult |
| `src/orchestrator/prompts.py` | 新增 COORDINATION_PLAN_PROMPT + build_coordination_planner_prompt |
| `src/orchestrator/graph.py` | 新增 coordination_plan_node + build_coordination_graph |
| `src/adapters/orchestrator.py` | 重构为 Coordinator + Scheduler pipeline |
| `src/schemas/request.py` | 新增 conversation_id, target_profile 等可选字段 |
| `src/api/v1/agent.py` | 扩展 orchestrator kwargs, 处理新 SSE 事件 |
| `src/api/dependencies.py` | 新增 get_agent_registry |
| `src/app/dependencies.py` | 新增 create_agent_registry |
| `src/app/main.py` | 启动时实例化 AgentRegistry |

---

## 四、SSE 事件流（Demo 对应）

```
用户: "实现登录系统"
  ↓
event: runtime.started
event: runtime.planning

  (Orchestrator 发现需要协调)
event: coordination.started                     round: 1
event: coordination.message                     项目经理 → 前端小助手
event: coordination.typing                      前端小助手 思考中...
event: coordination.message                     前端小助手 → 项目经理
event: coordination.message                     项目经理 → 代码审查员
event: coordination.typing                      代码审查员 思考中...
event: coordination.message                     代码审查员 → 项目经理
event: coordination.ended                       round: 1

event: coordination.started                     round: 2
  ... (同上) ...
event: coordination.ended                       round: 2

event: runtime.plan_completed                   计划: task-001(前端小助手) + task-002(代码审查员)

  (执行 task-001)
event: agent.spawned                            前端小助手
event: workspace.branch.created                 feature/task-001-ui
event: task.started                             task-001
event: agent.delta                              "正在实现登录系统..."
event: agent.tool_call                          npm install bcryptjs...
event: agent.tool_result                        ✓ 完成 (1.2s)
event: agent.tool_call                          write_file Login.tsx
event: task.completed                           task-001
event: workspace.merge.started
event: workspace.merge.completed                ✓ 无冲突

  (执行 task-002)
event: agent.spawned                            代码审查员
event: workspace.branch.created                 feature/task-002-review
event: task.started                             task-002
event: agent.delta                              "正在审查..."
event: agent.tool_call                          taskctl summary
event: agent.delta                              "审查结果：..."
event: task.completed                           task-002
event: workspace.merge.started
event: workspace.merge.completed

event: runtime.completed                        汇总 + evolution 记录
```

---

## 五、API 请求格式

`POST /v1/agent/stream` 端点签名不变。请求体新增可选字段：

```json
{
  "task_id": "task-001",
  "session_id": "orch-1",
  "message": "实现登录系统，支持邮箱和 OAuth",
  "agent_type": "orchestrator",
  "config": {
    "agents": [
      {
        "id": "claude-code",
        "profile": "frontend-engineer",
        "adapter": "claude-code",
        "name": "前端小助手",
        "session_id": "session-a"
      },
      {
        "id": "opencode",
        "profile": "reviewer",
        "adapter": "opencode",
        "name": "代码审查员",
        "session_id": "session-b"
      }
    ],
    "task_id": "task-001",
    "shared_dir": "/path/to/shared",
    "repo_path": "/path/to/repo"
  },
  "conversation_id": "conv-001",
  "target_profile": null
}
```

---

## 六、Go Backend 变更

### 设计原则

Go Backend 是 **SSE 透明网关**：
- `StreamWriter.Run()` 把 AgentEnd 的 **每一行 SSE** 都 `XADD` 到 Redis Stream
- `StreamHandler.serveStreaming()` 从 Redis 读取并原样 `Fprintf` 给前端
- **新事件类型（runtime.*, coordination.*）无需任何后端代码即可透传**

需要改的地方很少。

---

### 6.1 `internal/generated/events.go` — 新增事件类型常量

```go
// 在现有 EventType 基础上新增（用于 StreamWriter 状态检测）
const (
    // runtime.*
    EventTypeRuntimeStarted    EventType = "runtime.started"
    EventTypeRuntimePlanning   EventType = "runtime.planning"
    EventTypeRuntimePlanCompleted EventType = "runtime.plan_completed"
    EventTypeRuntimeCompleted  EventType = "runtime.completed"
    EventTypeRuntimeFailed     EventType = "runtime.failed"
    // coordination.*
    EventTypeCoordinationStarted EventType = "coordination.started"
    EventTypeCoordinationMessage EventType = "coordination.message"
    EventTypeCoordinationTyping  EventType = "coordination.typing"
    EventTypeCoordinationEnded   EventType = "coordination.ended"
    // task.*
    EventTypeTaskStarted    EventType = "task.started"
    EventTypeTaskCompleted  EventType = "task.completed"
    EventTypeTaskFailed     EventType = "task.failed"
    // agent.*
    EventTypeAgentSpawned    EventType = "agent.spawned"
    EventTypeAgentDelta      EventType = "agent.delta"
    EventTypeAgentToolCall   EventType = "agent.tool_call"
    EventTypeAgentToolResult EventType = "agent.tool_result"
    // workspace.*
    EventTypeWorkspaceBranchCreated  EventType = "workspace.branch.created"
    EventTypeWorkspaceMergeStarted   EventType = "workspace.merge.started"
    EventTypeWorkspaceMergeCompleted EventType = "workspace.merge.completed"
    EventTypeWorkspaceMergeConflict  EventType = "workspace.merge.conflict"
)
```

> 注意：这些常量仅用于 StreamWriter 中的 switch case 类型匹配。
> 实际 SSE 数据始终是字符串透传，即使不添加常量也能工作。
> 添加常量是为了类型安全和后续维护。

---

### 6.2 `internal/generated/request.go` — 扩展 AgentRequest

```go
type AgentRequest struct {
    // 现有字段
    TaskId      string    `json:"task_id"`
    SessionId   string    `json:"session_id"`
    Message     string    `json:"message"`
    AgentType   AgentType `json:"agent_type,omitempty"`
    Stream      bool      `json:"stream,omitempty"`
    SystemPrompt *string  `json:"system_prompt,omitempty"`
    Rules       []string  `json:"rules,omitempty"`
    WorkspacePath *string `json:"workspace_path,omitempty"`
    RepoPath    *string   `json:"repo_path,omitempty"`
    Config      *interface{} `json:"config,omitempty"`
    // 新增：群聊 / 对话层
    ConversationId      *string  `json:"conversation_id,omitempty"`
    TargetProfile       *string  `json:"target_profile,omitempty"`
    ReplyToMessageId    *string  `json:"reply_to_message_id,omitempty"`
    QuoteMessageIds     []string `json:"quote_message_ids,omitempty"`
    RegenerateMessageId *string  `json:"regenerate_message_id,omitempty"`
}
```

---

### 6.3 `internal/handler/task.go` — RunTask 支持群聊

#### 6.3.1 扩展 RunTaskReq

```go
type RunTaskReq struct {
    Message   string `json:"message" binding:"required"`
    AgentType string `json:"agent_type"`
    SessionID string `json:"session_id" binding:"required"`
    // 新增：群聊字段
    ConversationId      *string  `json:"conversation_id,omitempty"`
    TargetProfile       *string  `json:"target_profile,omitempty"`
    ReplyToMessageId    *string  `json:"reply_to_message_id,omitempty"`
    QuoteMessageIds     []string `json:"quote_message_ids,omitempty"`
    RegenerateMessageId *string  `json:"regenerate_message_id,omitempty"`
}
```

#### 6.3.2 修改 RunTask 构建 AgentRequest

关键变更：当 `agent_type == "orchestrator"` 时，从 Task 关联的 Sessions 构建 `config.agents` 列表传给 AgentEnd。

```go
func (h *TaskHandler) RunTask(c *gin.Context) {
    // ... 现有逻辑：查 task、保存 user message、创建 agent message ...

    go func() {
        agentReq := &generated.AgentRequest{
            TaskId:          taskID,
            SessionId:       req.SessionID,
            Message:         req.Message,
            AgentType:       generated.AgentType(agentType),
            Stream:          true,
            ConversationId:  req.ConversationId,
            TargetProfile:   req.TargetProfile,
            // ... 其他新字段 ...
        }
        if task.RepoPath != "" {
            agentReq.RepoPath = &task.RepoPath
        }

        // 新增：orchestrator 模式时，构建 config.agents
        if agentType == "orchestrator" {
            var sessions []model.Session
            db.GetDB().Where("task_id = ?", taskID).Find(&sessions)

            agents := make([]map[string]interface{}, 0, len(sessions))
            for _, s := range sessions {
                agents = append(agents, map[string]interface{}{
                    "id":         s.AgentType,
                    "profile":    s.AgentType,    // 或从 session_agent 取
                    "adapter":    s.AgentType,
                    "name":       s.AgentName,
                    "session_id": s.SessionID,
                })
            }
            config := map[string]interface{}{
                "agents":     agents,
                "task_id":    taskID,
                "shared_dir": fmt.Sprintf("worktrees/%s/shared/.agent", taskID),
            }
            if task.RepoPath != "" {
                config["repo_path"] = task.RepoPath
            }
            agentReq.Config = &config
        }

        // ... 现有逻辑：StreamAgent、StreamWriter.Run ...
    }()
}
```

---

### 6.4 `internal/stream/writer.go` — 处理新事件类型

#### 6.4.1 扩展 StreamWriter.Run 的 switch case

现有代码只处理 `EventTypeText`、`EventTypeDone`、`EventTypeError`。
新增对 `runtime.*` 和 `agent.*` 事件的处理：

```go
switch event.Type {
case generated.EventTypeText, generated.EventTypeAgentDelta:
    // 文本内容 → 批量刷写到 MySQL
    textKey := "text"
    if event.Type == generated.EventTypeAgentDelta {
        textKey = "text" // agent.delta payload 也是 {"text": "..."}
    }
    if text, ok := event.Content[textKey].(string); ok {
        sw.appendText(text)
    }
case generated.EventTypeDone:
    // 正常结束
case generated.EventTypeError, generated.EventTypeRuntimeFailed, generated.EventTypeTaskFailed:
    // 错误/失败
    sawError = true
    if errMsg, ok := event.Content["error"].(string); ok && errMsg != "" {
        sw.appendText("[Error] " + errMsg)
    }
case generated.EventTypeRuntimeCompleted:
    // Orchestrator 正常完成
case generated.EventTypeCoordinationMessage,
     generated.EventTypeCoordinationTyping,
     generated.EventTypeAgentToolCall,
     generated.EventTypeAgentToolResult:
    // 这些事件只转发到 Redis，不刷写到 MySQL（非文本内容）
}
```

**关键**：StreamWriter 的 `publishToRedis(line)` 已经把每一行都发到 Redis，所以新事件类型自动透传。上面只是让文本提取和状态检测更精确。

---

### 6.5 `internal/model/session.go` — 无需修改

Session 模型已有 `AgentType` + `AgentName`。CreateTask 时前端传入 agents 列表已经可以创建多个 Session。

---

### 6.6 `internal/model/message.go` — 可选扩展

群聊中一条用户消息可能触发多个 Agent 回复。当前设计是：
- 一个 RunTask 调用 → 一个 agent message（status=streaming）
- Orchestrator 的所有协调 + 执行事件都在这一个 message 的 SSE stream 里

这个设计可以工作，因为前端按 event type 区分渲染（coordination 渲染到协调面板，agent.delta 渲染到消息气泡）。

如果需要按 Agent 拆分消息（每个 Agent 一条 message），可选新增字段：

```go
type Message struct {
    // ... 现有字段 ...
    // 可选：群聊中的 agent 标识
    AgentProfile string `gorm:"size:64" json:"agent_profile,omitempty"` // "frontend-engineer"
}
```

Phase 5 暂不加，保持一个 message 包含全部事件。

---

### Go Backend 文件变更总结

| 文件 | 变更 | 优先级 |
|------|------|--------|
| `internal/generated/events.go` | 新增 20 个 RuntimeEvent 常量 | P0 |
| `internal/generated/request.go` | 新增 5 个群聊字段 | P0 |
| `internal/handler/task.go` | RunTask 构建 orchestrator config.agents | P0 |
| `internal/stream/writer.go` | switch case 新增事件类型处理 | P1 |

> StreamWriter 和 StreamHandler 的核心机制（Redis Stream 透传）完全不变。
> 新事件类型之所以能自动透传，是因为 `publishToRedis(line)` 不区分事件类型。

---

## 七、验证方式

1. **单元测试**（每个新模块）：
   - `coordinator.py`: mock adapter.chat()，验证多轮协调循环
   - `execution.py`: mock stream_chat()，验证 normalize 映射
   - `scheduler.py`: mock engine + workspace，验证串行流程
   - `registry.py`: 验证 profile → adapter 映射

2. **集成测试**：
   - `make run-agentend` 启动
   - curl 发 orchestrator stream 请求，观察 SSE 事件序列
   - 验证 workspace worktree 创建和合并

3. **前端联调**：
   - 前端监听新事件类型，渲染协调通道 + Agent 执行卡片
   - 对比 demo.html 视觉效果
