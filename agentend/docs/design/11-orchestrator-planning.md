# Orchestrator 规划 + 闭环编排实现

> **实现同步（2026-08-20）**：本文件描述的 Graph 是唯一生命周期所有者。历史段落中关于
> `OrchestratorAdapter._handle_execute`、placeholder execute 或在 `save_mem` 发送 DONE 的描述均已失效；
> 冲突恢复和唯一终态以 [根设计](../../../docs/design/14-orchestrator-conflict-recovery.md) 为准。

## 实现了什么

Orchestrator 作为任务编排器，通过 LangGraph 状态机实现 **skill_prepare → reason（含 Agent 按需发现、
ask_agent 工具调用）→ human_review → dispatch → execute → review → final_aggregate → evolve → save_mem**
闭环编排；冲突恢复耗尽时走 `await_user` 并暂停，不生成根 `done`。

核心功能：
1. **Skill Prepare** — 扫描 L1 skill 元数据，构造只含身份/规则/工具/技能摘要的 REASON_PROMPT；L2/L3 内容由 `load_skill_detail` 按需加载
2. **Reason** — LLM tool-calling 循环：支持 current_time / list_available_agents / read_file / list_dir / write_file / run_skill / load_resource / load_skill_detail / ask_agent / plan_and_dispatch 工具；咨询或非空分派计划必须先完成一次独立 Agent 发现
3. **Dispatch** — PlanOutput → DispatchResult 转换 + 拓扑排序为执行波次
4. **Execute** — ExecutionEngine 按波次执行，统一通过 BackendClient HTTP 调度子 Agent 并订阅 SSE
5. **Review** — 检查失败任务，触发 conditional re-plan（最多 3 次迭代）
6. **Evolve** — 记录编排经验到 EvolutionStore
7. **Final Aggregate / Save Mem** — 仅在根 Graph 正常终止前生成最终摘要并保存记忆；`evolve` 与 `save_mem`
   不发送 DONE

`list_available_agents()` 工具按需返回本轮 Agent 快照（只含 id/name）；`ask_agent` 工具允许 Reason 阶段向特定 Agent 提问（通过 BackendClient → Go Backend → agentend 流式获取回答），结果用于 Planner 做决策。Agent id 仍由服务端 `state["agents"]` 校验。

## 整体架构

```
POST /v1/agent/stream (agent_type=orchestrator)
        │
        ▼
  OrchestratorAdapter.stream_chat()
        │
        ▼
  LangGraph StateGraph (8 nodes, conditional routing)
        │
   skill_prepare ──▶ reason ──▶ human_review ──▶ dispatch ──▶ execute ──▶ review
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
│   │   ├── graph.py         # LangGraph 8-node StateGraph（含 ask_agent 处理 + human_review + conditional routing）
│   │   ├── prompts.py       # REASON_PROMPT + build_reason_prompt()
│   │   ├── tools.py         # 规划工具（current_time, list_available_agents, read_file, list_dir, write_file, run_skill,
│   │   │                    #   load_resource, load_skill_detail, ask_agent, plan_and_dispatch）
│   │   └── skill_loader.py  # L1→L2→L3 技能发现和加载
│   ├── execution/
│   │   ├── engine.py        # ExecutionEngine（BackendClient HTTP 调度 + SSE 聚合）
│   │   ├── dispatcher.py    # Dispatcher (PlanOutput → DispatchResult) + topological_sort
│   │   ├── coordination.py  # CoordinationChannel（Agent 间 Q&A）
│   │   ├── state.py         # TaskState enum + RuntimeState
│   │   └── wave.py          # 兼容的 Wave 执行辅助（主流程由 graph.execute_node 统一拥有）
│   ├── memory/
│   │   ├── pin_memory.py    # PinMemory (common/ + _pins.yaml)
│   │   ├── conversation_memory.py  # ConversationMemoryStore (conversation_memory.json)
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
    merge_to_main: bool = False    # 任务成功后是否由 orchestrator 请求合并 task 分支到 main

class TaskResult(BaseModel):          # 新增
    task_id: str
    agent: str
    success: bool
    content: str
    message_id: str = ""              # Backend 持久化的 Agent 回复 message_id
    duration: float = 0.0
    error_type: str = ""              # 失败类型，如 timeout / error
    error_message: str = ""           # 结构化失败原因
    conflict_files: list[str] = []    # merge 冲突文件列表

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
3. **Execute 节点** — `graph.execute_node` 接管 Wave-by-Wave 执行，返回权威 TaskResult，并通过 runtime queue
   产出 RUNTIME/INTEGRATION/RESOLUTION 事件
4. **Aggregation** — `final_aggregate` 只生成根任务最终摘要；Graph END 后 Adapter 发送唯一 DONE

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
            # graph.execute_node 已经执行真实 child Runs 并返回权威 TaskResult
            pass
        elif node_name == "final_aggregate":
            yield summary_event(...)
        # 根 Graph END 后由 Adapter 发送唯一 DONE；await_user 不发送 DONE
```

### Dispatcher (`src/orchestrator/execution/dispatcher.py`)

将 `PlanOutput` 转换为 `@agent` 调度指令。只接受非 Orchestrator Agent 的精确 id，并从 agents config 中查找 `workspace_path` 和真实 `session_id`；未知 id 或缺少真实 session 时明确失败，不静默改派。

### ExecutionEngine (`src/orchestrator/execution/engine.py`)

执行引擎不再走 short-circuit CLI。当前统一通过 `BackendClient.run_task()` 把子任务交给 Go Backend，由 Backend 再调用对应 AgentEnd adapter；随后通过 `BackendClient.stream_result()` 订阅该子任务的 SSE 输出。执行前会按 `real_session_id` 为子 Agent 创建独立 worktree，任务消息末尾注入 `taskctl merge` 集成要求。

### Aggregator (`src/orchestrator/reporting/aggregator.py`)

LLM 调用汇总多 Agent 结果。输入 `list[TaskResult]` + overview，输出人类可读的汇总报告。如果无结果，返回空字符串。

### REASON Prompt (`src/orchestrator/planning/prompts.py`)

`build_reason_prompt()` 在 `REASON_PROMPT` 基础上注入静态身份、工具说明和 L1 skill 摘要；动态上下文由 `reason_node` 以消息列表方式追加，不再拼进 prompt 字符串。当前 Agent 列表也不注入系统提示词，而由 `list_available_agents()` 按需返回：

- **技能描述** — `skill_prepare_node` 只把 L1 name + description 写入 "## 可用 Skills"；需要完整 `SKILL.md` 或资源文件时，LLM 调用 `load_skill_detail(skill_name, level, resource_path)`
- **Pin 约束** — Backend pinned announcements 先经 `PinRule` 转成 `system_prompt_append`，再进入 `state["pin_context"]`
- **历史经验** — `EvolutionStore.get_recent_experience()` 在 `skill_prepare_node` 中计算，进入 `state["evolution_context"]`
- **Agent 发现** — `state["agents"]` 保留服务端权威快照；每次 `reason_node` 仅在需要咨询或非空分派时调用发现工具，发现许可不跨 Reason 调用复用

`graph.py` 的 `skill_prepare_node` 调用 `build_reason_prompt()` 构造系统 prompt，`reason_node` 使用该 prompt 加上 Pin / Evolution / 群聊上下文 / memory messages 进行 tool-calling 循环。Prompt 中定义了 `list_available_agents` / `ask_agent` / `plan_and_dispatch` / `read_file` / `list_dir` / `write_file` / `run_skill` / `load_resource` / `load_skill_detail` / `current_time` 等工具的使用规则。

### Ask Agent (`src/orchestrator/planning/graph.py:_handle_ask_agent_call`)

Reason 阶段 LLM 可调用 `ask_agent(agent, question)` 向特定 Agent 提问，通过 BackendClient → Go Backend → agentend 流式获取回答。实现要点：

- 从 `state["agents"]` 中查找目标 agent 的 `session_id`
- 调用 `BackendClient.run_task()` 发送任务到 Go Backend
- 通过 `BackendClient.stream_result()` 订阅 SSE 流
- 向 `ask_event_queue` 推送 `ASK_CARD_START`/`ASK_CARD_DONE` 事件供前端渲染
- 设置 180 秒总超时 + 3 次 `run_task` 重试
- 返回值直接作为 ToolMessage 注入 REASON 的 tool-calling 循环

### Pin Memory (`src/orchestrator/memory/pin_memory.py`)

`PinMemory` 仍提供文件型 pin API，复用 `memory/common/` 目录和 `_pins.yaml` 书签层：

- `pin(title, content)` — 写文件到 common/ + 加 _pins.yaml 条目 + AI 生成摘要
- `pin_existing(filename)` — 只加 _pins.yaml 书签（不写文件）
- `unpin(filename)` — 只删 _pins.yaml 条目，文件保留
- `get_context()` — 返回格式化摘要，注入 Planner prompt
- `get_full_content(filename)` — 返回文件完整内容

当前 Orchestrator 主流程的固定约束优先来自 Backend pinned announcements：`agent.py` 预取置顶公告，`PinRule` 写入 `system_prompt_append`，`OrchestratorAdapter.stream_chat` 再把它放入 `state["pin_context"]`，由 `reason_node` 以 `SystemMessage` 注入。

### Pin API (`src/api/v1/pin.py`)

```
POST /v1/pin/add                {shared_dir, content, title}
POST /v1/pin/remove             {shared_dir, filename}
POST /v1/pin/announcement-unpin {shared_dir, content, sender_name}
GET  /v1/pin/list               ?shared_dir=...
```

### Evolution (`src/orchestrator/memory/evolution.py`)

`{shared_dir}/evolution.yaml`（通常是 `shared/.agent/evolution.yaml`）存储最近 20 条编排经验：

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

## 执行阶段的群聊消息

每个可执行 wave 开始前，规划图先发布一条属于 Orchestrator 的 `text` 调度通知，并为该
wave 生成稳定的 source `message_id`。`ExecutionEngine` 随后把子 Backend 流中的文本转为
普通 `text` 事件，携带 `agent`、`agent_type`、`session_id`、子运行 `message_id`、`run_id`
和 `attempt`。`runtime_text` 只保留给旧版卡片日志兼容，子 Agent 的自然语言回复不得再转成
该事件，否则前端只能把回复嵌在 Orchestrator 卡片中。

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
