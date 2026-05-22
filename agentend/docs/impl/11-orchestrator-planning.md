# Orchestrator 规划 + 闭环编排实现

## 实现了什么

Orchestrator 作为 "AI 项目经理"，从"写文件就结束"的无状态脚本升级为 **plan → dispatch → collect → aggregate** 闭环编排器。

核心闭环：
1. **Planner** — LLM 拆解用户需求为子任务（注入 Pin 约束 + 历史经验）
2. **Dispatcher** — 产出 `@agent` 调度指令（结构化 JSON，由前端/Go Backend 消费）
3. **Collect** — 收集 Agent 执行结果（通过 `results_callback` 参数）
4. **Aggregator** — LLM 汇总多 Agent 结果为人类可读报告
5. **Evolution** — 每次编排后记录成败经验，注入下次 Planner prompt

## 整体架构

```
POST /v1/agent/execute (agent_type=orchestrator)
        │
        ▼
  OrchestratorAdapter
        │
        ▼
  LangGraph StateGraph
   ┌────┴────┐
   │  plan   │ ← LLM (Pin + Evolution 注入 prompt)
   │  node   │
   └────┬────┘
   ┌────▼────┐
   │ write_  │ ← 文件 IO
   │ shared  │
   └────┬────┘
        │
        ▼ plan → dispatch → collect → aggregate
        │
   ┌────▼────────┐
   │ Dispatcher  │ → 产出 @agent 调度 JSON
   └────┬────────┘
        │ (results_callback)
   ┌────▼────────┐
   │ Aggregator  │ ← LLM 汇总 Agent 结果
   └────┬────────┘
   ┌────▼────────┐
   │ Evolution   │ ← 记录编排经验
   └─────────────┘
```

## 文件结构

```
src/
├── orchestrator/
│   ├── __init__.py
│   ├── models.py       # TaskDef, PlanOutput, TaskResult, DispatchResult
│   ├── prompts.py      # PLAN_PROMPT + build_planner_prompt()
│   ├── graph.py        # LangGraph (plan → write_shared)
│   ├── state.py        # TaskState enum + RuntimeState
│   ├── dispatcher.py   # Dispatcher (PlanOutput → DispatchResult)
│   ├── aggregator.py   # Aggregator (LLM 汇总)
│   ├── pin_memory.py   # PinMemory (common/ + _pins.yaml)
│   └── evolution.py    # EvolutionStore (evolution.yaml)
├── adapters/
│   └── orchestrator.py # OrchestratorAdapter (闭环逻辑)
└── api/v1/
    ├── agent.py        # _orchestrator_kwargs()
    └── pin.py          # /v1/pin/* 端点
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
    mention: str                      # "@claude-code"
    content: str
    depends_on: list[str] = []
    workspace_path: str = ""
```

### 闭环流程 (`src/adapters/orchestrator.py`)

`OrchestratorAdapter.stream_chat` 内部串联五个阶段：

1. **Planning** — 调用 LangGraph graph 执行 plan + write_shared
2. **Dispatch** — `Dispatcher.dispatch(plan)` 产出 `list[DispatchResult]`，逐个 yield dispatch 事件
3. **Collect** — 如果调用方传入 `results_callback`，用它获取 Agent 执行结果；否则使用 mock
4. **Aggregate** — `Aggregator.aggregate(results, overview)` 调用 LLM 汇总，yield DONE 事件
5. **Evolution** — `EvolutionStore.record()` 记录本次编排成败经验

```python
async def stream_chat(self, session_id, message, **kwargs):
    # Phase 1: Planning
    yield StreamEvent.create(EventType.PLANNING, status="started")
    result = await self._graph.ainvoke({...})

    # Phase 2: Dispatch
    dispatcher = Dispatcher(agents)
    dispatch_results = dispatcher.dispatch(plan)
    for dr in dispatch_results:
        yield StreamEvent.create(EventType.PLANNING, node="dispatch", dispatch=dr.model_dump())

    # Phase 3: Collect
    task_results = await results_callback(dispatch_results) if results_callback else [...]

    # Phase 4: Aggregate
    aggregated = Aggregator().aggregate(task_results, overview)

    # Phase 5: Record experience
    EvolutionStore(shared_dir).record(...)
    yield StreamEvent.create(EventType.DONE, text=aggregated)
```

### Dispatcher (`src/orchestrator/dispatcher.py`)

将 `PlanOutput` 转换为 `@agent` 调度指令。从 agents config 中查找 `workspace_path`。如果 agent 不在 config 中，`workspace_path` 为空字符串。

### Aggregator (`src/orchestrator/aggregator.py`)

LLM 调用汇总多 Agent 结果。输入 `list[TaskResult]` + overview，输出人类可读的汇总报告。如果无结果，返回空字符串。

### Planner Prompt 升级 (`src/orchestrator/prompts.py`)

`build_planner_prompt()` 在 `PLAN_PROMPT` 基础上注入两个可选上下文：

- **Pin 约束** — `PinMemory.get_context()` 返回 "## 必须遵守的约束（Pin）" 段落
- **历史经验** — `EvolutionStore.get_recent_experience()` 返回 "## 最近编排经验" 段落

`graph.py` 的 `plan_node` 已改为调用 `build_planner_prompt()` 而非 `PLAN_PROMPT.format()`。

### Pin Memory (`src/orchestrator/pin_memory.py`)

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

### Evolution (`src/orchestrator/evolution.py`)

`shared/.agent/evolution.yaml` 存储最近 20 条编排经验：

- `record(message, plan_summary, results_summary, success, agent_performance)` — 追加条目，超 20 条自动裁剪
- `get_recent_experience(n=5)` — 返回最近 N 条经验的格式化字符串（✅/❌ 指示器）

### RuntimeState (`src/orchestrator/state.py`)

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
curl -X POST http://localhost:8001/v1/agent/execute \
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
