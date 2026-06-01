# Orchestrator 规划 + 闭环编排实现

## 实现了什么

Orchestrator 作为任务编排器，通过 LangGraph 7 节点状态机实现 **skill_prepare → reason（含 ask_agent 工具调用）→ dispatch → execute → review → evolve → save_mem** 闭环编排。

核心功能：
1. **Skill Prepare** — L1→L2 技能发现与加载，构造 REASON_PROMPT
2. **Reason** — LLM tool-calling 循环：支持 read_file / list_dir / ask_agent / plan_and_dispatch 等工具
3. **Dispatch** — PlanOutput → DispatchResult 转换 + 拓扑排序为执行波次
4. **Execute** — ExecutionEngine 按波次执行（short-circuit CLI 或 HTTP fallback）
5. **Review** — 检查失败任务，触发 conditional re-plan（最多 3 次迭代）
6. **Evolve** — 记录编排经验到 EvolutionStore
7. **Save Mem** — 保存记忆，yield terminal DONE 事件

`ask_agent` 工具允许 Reason 阶段向特定 Agent 提问（通过 BackendClient → Go Backend → agentend 流式获取回答），结果用于 Planner 做决策。

## 整体架构

```
POST /v1/agent/stream (agent_type=orchestrator)
        │
        ▼
  OrchestratorAdapter.stream_chat()
        │
        ▼
  LangGraph StateGraph (7 nodes, conditional routing)
        │
   skill_prepare ──▶ reason ──▶ dispatch ──▶ execute ──▶ review
                        │                      ▲          │
                        │ (ask_agent)          │  (needs_replan=true)
                        │                      │          │
                        └── BackendClient ─────┘          │
                                     │               evolve ──▶ save_mem
                                     ▼
                              Go Backend ──▶ agentend
```

## 文件结构

```
src/
├── orchestrator/
│   ├── models.py            # TaskDef, PlanOutput, TaskResult, DispatchResult
│   ├── planning/
│   │   ├── graph.py         # LangGraph 7-node StateGraph（含 ask_agent 处理 + conditional routing）
│   │   ├── prompts.py       # REASON_PROMPT + build_reason_prompt()
│   │   ├── tools.py         # 规划工具（read_file, list_dir, ask_agent, plan_and_dispatch 等）
│   │   └── skill_loader.py  # L1→L2→L3 技能发现和加载
│   ├── execution/
│   │   ├── engine.py        # ExecutionEngine（short-circuit CLI 或 HTTP fallback）
│   │   ├── dispatcher.py    # Dispatcher (PlanOutput → DispatchResult) + topological_sort
│   │   ├── coordination.py  # CoordinationChannel（Agent 间 Q&A）
│   │   ├── state.py         # TaskState enum + RuntimeState
│   │   └── wave.py          # Wave 执行子图（占位）
│   ├── memory/
│   │   ├── pin_memory.py    # PinMemory (common/ + _pins.yaml)
│   │   └── evolution.py     # EvolutionStore (evolution.yaml)
│   ├── prompts/
│   │   └── group_chat.py    # 跨 Agent 对话上下文构建（build_group_chat_context）
│   └── reporting/
│       └── aggregator.py    # Aggregator (LLM 汇总)
├── adapters/
│   └── orchestrator.py      # OrchestratorAdapter（LangGraph stream + ask_event_queue）
├── clients/
│   └── backend_client.py    # BackendClient（与 Go Backend 通信）
└── api/v1/
    ├── agent.py             # _orchestrator_kwargs() + _resolve_workspace()
    └── pin.py               # /v1/pin/* 端点
```

## 怎么实现的

### 数据模型 (`src/orchestrator/models.py`)

```python
class TaskDef(BaseModel):
    task_id: str
    session_id: str     # agent id
    title: str
    content: str

class PlanOutput(BaseModel):
    overview: str
    tasks: list[TaskDef]

class TaskResult(BaseModel):          # 新增
    task_id: str
    agent: str
    success: bool
    content: str
    duration: float = 0.0

class DispatchResult(BaseModel):      # 新增
    task_id: str
    agent: str
    agent_type: str = ""              # 目标 agent 类型（如 claude-code, opencode）
    real_session_id: str = ""         # DB 分配的真实 session_id
    mention: str                      # "@claude-code"
    content: str
    depends_on: list[str] = []
    workspace_path: str = ""
```

### 闭环流程 (`src/adapters/orchestrator.py`)

`OrchestratorAdapter.stream_chat` 使用异步事件队列模式驱动 LangGraph 流：

1. **Graph 流式执行** — `self._graph.astream()` 产生 node update 频率
2. **Ask 事件队列** — `asyncio.Queue` 收集 ask_agent 的 ASK_CARD_START/ASK_CARD_DONE 事件，与 graph updates 并行消费
3. **Execute 节点** — `_handle_execute` 接管 Wave-by-Wave 执行，yield RUNTIME_EXECUTING/TEXT/COMPLETED 事件
4. **Aggregation** — 执行完毕后 `Aggregator.aggregate()` 汇总，yield terminal DONE

```python
async def stream_chat(self, session_id, message, **kwargs):
    # 构造 GraphState 初始状态
    initial_state = {
        "message": message, "agents": agents, "orchestrator": orchestrator,
        "task_id": task_id, "shared_dir": shared_dir, ...
    }

    # 设置 ContextVar：ask_event_queue, backend_client, cwd
    tokens = set_reason_runtime_context(
        ask_event_queue=ask_event_queue,
        backend_client=backend_client,
        cwd=cwd,
    )

    # async stream graph updates
    async for chunk in self._graph.astream(initial_state, stream_mode="updates"):
        node_name = next(iter(chunk))
        if node_name == "reason":
            yield from self._handle_reason(node_output)
        elif node_name == "execute":
            yield from self._handle_execute(...)
        elif node_name == "save_mem":
            yield self._build_done_event(current_state)
```

### Dispatcher (`src/orchestrator/execution/dispatcher.py`)

将 `PlanOutput` 转换为 `@agent` 调度指令。从 agents config 中查找 `workspace_path`。如果 agent 不在 config 中，`workspace_path` 为空字符串。

### Aggregator (`src/orchestrator/reporting/aggregator.py`)

LLM 调用汇总多 Agent 结果。输入 `list[TaskResult]` + overview，输出人类可读的汇总报告。如果无结果，返回空字符串。

### REASON Prompt (`src/orchestrator/planning/prompts.py`)

`build_reason_prompt()` 在 `REASON_PROMPT` 基础上注入可选上下文：

- **技能描述** — L2 skill 内容作为 "## 可用 Skills" 段落注入
- **Pin 约束** — `PinMemory.get_context()` 返回约束段落
- **历史经验** — `EvolutionStore.get_recent_experience()` 返回最近的编排经验

`graph.py` 的 `skill_prepare_node` 调用 `build_reason_prompt()` 构造系统 prompt，`reason_node` 使用该 prompt 进行 tool-calling 循环。Prompt 中定义了 `ask_agent` / `plan_and_dispatch` / `read_file` / `list_dir` / `current_time` 等工具的使用规则。

### Ask Agent (`src/orchestrator/planning/graph.py:_handle_ask_agent_call`)

Reason 阶段 LLM 可调用 `ask_agent(agent, question)` 向特定 Agent 提问，通过 BackendClient → Go Backend → agentend 流式获取回答。实现要点：

- 从 `state["agents"]` 中查找目标 agent 的 `session_id`
- 调用 `BackendClient.run_task()` 发送任务到 Go Backend
- 通过 `BackendClient.stream_result()` 订阅 SSE 流
- 向 `ask_event_queue` 推送 `ASK_CARD_START`/`ASK_CARD_DONE` 事件供前端渲染
- 设置 180 秒总超时 + 3 次 `run_task` 重试
- 返回值直接作为 ToolMessage 注入 REASON 的 tool-calling 循环

### Pin Memory (`src/orchestrator/memory/pin_memory.py`)

复用 `memory/common/` 目录，`_pins.yaml` 作为书签层：

- `pin(title, content)` — 写文件到 common/ + 加 _pins.yaml 条目 + AI 生成摘要
- `pin_existing(filename)` — 只加 _pins.yaml 书签（不写文件）
- `unpin(filename)` — 只删 _pins.yaml 条目，文件保留
- `get_context()` — 返回格式化摘要，注入 Planner prompt
- `get_full_content(filename)` — 返回文件完整内容

### Pin API (`src/api/v1/pin.py`)

```
POST /v1/pin/add      {shared_dir, content, title}
POST /v1/pin/remove   {shared_dir, filename}
GET  /v1/pin/list     ?shared_dir=...
```

### Evolution (`src/orchestrator/memory/evolution.py`)

`shared/.agent/evolution.yaml` 存储最近 20 条编排经验：

- `record(message, plan_summary, results_summary, success, agent_performance)` — 追加条目，超 20 条自动裁剪
- `get_recent_experience(n=5)` — 返回最近 N 条经验的格式化字符串（✅/❌ 指示器）

### RuntimeState (`src/orchestrator/execution/state.py`)

内存中的任务状态追踪：

```python
class TaskState(str, Enum):
    PENDING = "pending"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"

class RuntimeState:
    tasks: dict[str, TaskState]
    results: dict[str, str]
    running_agents: dict[str, str]   # agent_id → task_id
```

## 调用示例

```bash
curl -X POST http://localhost:8001/v1/agent/stream \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "orch-test",
    "session_id": "orch-planner",
    "message": "用 Claude Code 写登录页，用 OpenCode 审查代码",
    "agent_type": "orchestrator",
    "config": {
      "agents": [
        {"id": "claude-code", "session_id": "cc-orch-test", "name": "Claude Code",
         "capabilities": ["代码生成"], "workspace_path": "/ws/claude"},
        {"id": "opencode", "session_id": "oc-orch-test", "name": "OpenCode",
         "capabilities": ["代码审查"], "workspace_path": "/ws/opencode"}
      ],
      "shared_dir": "/path/to/shared/.agent"
    }
  }'
```

## Pin 操作示例

```bash
# 添加 Pin
curl -X POST http://localhost:8001/v1/pin/add \
  -H 'Content-Type: application/json' \
  -d '{"shared_dir": "/path/to/shared/.agent", "title": "API 规范", "content": "所有接口必须使用 RESTful 风格..."}'

# 列出 Pins
curl "http://localhost:8001/v1/pin/list?shared_dir=/path/to/shared/.agent"

# 移除 Pin
curl -X POST http://localhost:8001/v1/pin/remove \
  -H 'Content-Type: application/json' \
  -d '{"shared_dir": "/path/to/shared/.agent", "filename": "api-spec.md"}'
```
