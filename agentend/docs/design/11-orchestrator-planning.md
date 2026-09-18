# Orchestrator 规划 + 闭环编排实现

> **实现同步（2026-08-20）**：本文件描述的 Graph 是唯一生命周期所有者。历史段落中关于
> `OrchestratorAdapter._handle_execute`、placeholder execute 或在 `save_mem` 发送 DONE 的描述均已失效；
> 冲突恢复和唯一终态以 [根设计](../../../docs/design/14-orchestrator-conflict-recovery.md) 为准。

## 实现了什么

Orchestrator 作为任务编排器，通过 LangGraph 状态机实现 **skill_prepare → compact_context → reason（含 Agent 按需发现、
ask_agent 工具调用）→ human_review → dispatch → execute → review → final_aggregate → evolve → save_mem**
闭环编排；冲突恢复耗尽时走 `await_user` 并暂停，不生成根 `done`。

核心功能：
1. **Skill Prepare** — 扫描 L1 skill 元数据，构造只含身份/规则/工具/技能摘要的 REASON_PROMPT；L2/L3 内容由 `load_skill_detail` 按需加载
2. **Context Compaction** — `compact_context` 节点按 token 预算压缩 Reason 消息历史（会话摘要 + 近期轮次保留），详见 [26-orchestrator-context-compaction.md](26-orchestrator-context-compaction.md)
3. **Reason** — LLM tool-calling 循环：支持 current_time / list_available_agents / read_file / list_dir / write_file / run_skill / load_resource / load_skill_detail / ask_agent / plan_and_dispatch 工具；咨询或非空分派计划必须先完成一次独立 Agent 发现
4. **Dispatch** — PlanOutput → DispatchResult 转换 + 拓扑排序为执行波次
5. **Execute** — ExecutionEngine 按波次执行，统一通过 BackendClient HTTP 调度子 Agent 并订阅 SSE
6. **Review** — 检查失败任务，触发 conditional re-plan（最多 3 次迭代）
7. **Evolve** — 记录编排经验到 EvolutionStore
8. **Final Aggregate / Save Mem** — 仅在根 Graph 正常终止前生成最终摘要并保存记忆；`evolve` 与 `save_mem`
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
  LangGraph StateGraph (11 nodes, conditional routing)
        │
   skill_prepare ──▶ compact_context ──▶ reason ──▶ human_review ──▶ dispatch ──▶ execute ──▶ review
                        │                      ▲          │
                        │ (ask_agent)          │  (needs_replan=true)
                        │                      │          │
                        └── BackendClient ─────┘          │
                                     │            await_user（冲突恢复耗尽时暂停）
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
│   │   ├── graph.py         # LangGraph 11-node StateGraph（含 ask_agent 处理 + human_review + await_user + compact_context + conditional routing）
│   │   ├── prompts.py       # REASON_PROMPT + build_reason_prompt()
│   │   ├── tools.py         # 规划工具（current_time, list_available_agents, read_file, list_dir, write_file, run_skill,
│   │   │                    #   load_resource, load_skill_detail, ask_agent, plan_and_dispatch）
│   │   ├── context_builder.py  # Active Pin Snapshot 构建与 System/Human-reference 消息组装
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
│   │   ├── context_compactor.py    # 上下文压缩（token 预算 + 会话摘要，见 26-orchestrator-context-compaction.md）
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
    depends_on: list[str] = []                        # 依赖的任务 ID 列表
    requires_integrated_dependencies: bool = True     # 是否要求依赖产物已集成

class PlanOutput(BaseModel):
    overview: str
    tasks: list[TaskDef]
    merge_to_main: bool = False    # 任务成功后是否由 orchestrator 请求合并 task 分支到 main

class TaskResult(BaseModel):
    task_id: str
    root_task_id: str = ""            # 根任务 ID（恢复与审计）
    agent: str
    attempt: int = 0                  # 执行尝试次数
    execution_status: ExecutionStatus   # pending/running/completed/failed/timeout/cancelled/blocked
    integration_status: IntegrationStatus  # Git 集成状态
    success: bool | None = None       # 兼容字段，由 execution+integration 状态派生
    content: str
    message_id: str = ""              # Backend 持久化的 Agent 回复 message_id
    run_id: str = ""                  # 对应 child Run ID
    plan_task_id: str = ""            # 逻辑计划任务 ID（兼容时等于 task_id）
    integration_operation_id: str = ""   # 关联的集成操作 ID
    integration_scope_id: str = ""    # Git 集成范围 ID
    workspace_id: str = ""            # WorkspaceManager 记录 ID
    resolved_from_conflict: bool = False  # 是否由 Resolver 从冲突恢复
    duration: float = 0.0
    error_type: str = ""              # 失败类型，如 timeout / error
    error_code: str = ""              # 机器可读错误码
    error_message: str = ""           # 结构化失败原因
    conflict_files: list[str] = []    # merge 冲突文件列表
    source_branch: str = ""           # 产物源分支（及 source_commit/target_branch/target_commit/merge_base 快照）

class DispatchResult(BaseModel):
    task_id: str
    attempt: int = 0                  # 执行尝试次数
    agent: str
    agent_type: str = ""              # 目标 agent 类型（如 claude-code, opencode）
    real_session_id: str = ""         # DB 分配的真实 session_id
    mention: str                      # "@claude-code"
    content: str
    depends_on: list[str] = []
    requires_integrated_dependencies: bool = True
    workspace_path: str = ""
    plan_task_id: str = ""            # 逻辑计划任务 ID（与 task_id 兼容）
    integration_operation_id: str = ""   # 预登记的集成操作 ID
    workspace_handle: str = ""        # 不透明 Workspace 引用
    integration_scope_id: str = ""    # Git 集成范围 ID
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
- **Pin 约束** — Backend pinned announcements 经完整校验形成 Run 级 Active Pin Snapshot，再作为独立 `SystemMessage` 注入
- **历史经验** — `EvolutionStore.get_recent_experience()` 在 `skill_prepare_node` 中计算，进入 `state["evolution_context"]`
- **Agent 发现** — `state["agents"]` 保留服务端权威快照；每次 `reason_node` 仅在需要咨询或非空分派时调用发现工具，发现许可不跨 Reason 调用复用

`graph.py` 的 `skill_prepare_node` 调用 `build_reason_prompt()` 构造系统 prompt，`reason_node` 使用该 prompt 加上 Pin / Evolution / 群聊上下文 / memory messages 进行 tool-calling 循环。Prompt 中定义了 `list_available_agents` / `ask_agent` / `plan_and_dispatch` / `read_file` / `list_dir` / `write_file` / `run_skill` / `load_resource` / `load_skill_detail` / `current_time` 等工具的使用规则。

### Ask Agent (`src/orchestrator/planning/graph.py:_handle_ask_agent_call`)

Reason 阶段 LLM 可调用 `ask_agent(agent, question)` 向特定 Agent 提问，通过 BackendClient → Go Backend → agentend 流式获取回答。实现要点：

- 从 `state["agents"]` 中查找目标 agent 的 `session_id`
- 调用 `BackendClient.run_task()` 发送任务到 Go Backend
- 通过 `BackendClient.stream_result()` 订阅 SSE 流
- 向 `ask_event_queue` 推送 `ASK_CARD_START`/`ASK_CARD_DONE` 事件供前端渲染
- 设置 180 秒总超时（`orchestrator.ask_agent_timeout`）+ 最多 3 次 `run_task` 尝试（`for attempt in range(3)`）
- 返回值直接作为 ToolMessage 注入 REASON 的 tool-calling 循环

### Pin Memory (`src/orchestrator/memory/pin_memory.py`)

`PinMemory` 仍提供文件型 pin API，复用 `memory/common/` 目录和 `_pins.yaml` 书签层：

- `pin(title, content)` — 写文件到 common/ + 加 _pins.yaml 条目 + AI 生成摘要
- `pin_existing(filename)` — 只加 _pins.yaml 书签（不写文件）
- `unpin(filename)` — 只删 _pins.yaml 条目，文件保留
- `get_context()` — 返回格式化摘要，注入 Planner prompt
- `get_full_content(filename)` — 返回文件完整内容

当前 Orchestrator 主流程的固定约束来自 Backend pinned announcements：`agent.py` fail-closed 获取并校验完整集合，将快照固化到首次 Run runtime；`context_builder` 把它作为独立权威 `SystemMessage` 注入。文件型 `PinMemory` 只属于 Human/reference 资料。

### Pin API (`src/api/v1/pin.py`)

```
POST /v1/pin/add                {shared_dir, content, title}
POST /v1/pin/remove             {shared_dir, filename}
POST /v1/pin/announcement-unpin {shared_dir, content, sender_name}
GET  /v1/pin/list               ?shared_dir=...
```

`announcement-unpin` 是 deprecated 兼容端点，只记录日志，不再修改 ConversationMemory。

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

## Reason 循环消息序不变量（2026-09-18 修复）

**问题**：DeepSeek 偶发返回参数 JSON 非法的 tool call。langchain_openai 将其归入
`AIMessage.invalid_tool_calls`（`tool_calls` 为空），reason_node 据此判定"模型只回了
文字"并走 forced-retry；但 append 回历史的 AI 消息保留了 `invalid_tool_calls`，而
`_convert_message_to_dict` 会把它**原样序列化回下一次请求的 `tool_calls`**。代码只为
合法 `tool_calls` 生成 ToolMessage，于是请求中出现无人应答的 `tool_call_id`，DeepSeek
以 `400: insufficient tool messages following tool_calls message` 拒绝 → run 秒败
（批跑 bugfix-impl-004 复现，15s 失败、13.5 分）。

**修复**（`src/orchestrator/planning/graph.py` reason_node）：append AI 消息后，为每个
`invalid_tool_calls` 条目合成一条 `ToolMessage`（内容为参数解析失败说明，
`tool_call_id` 对应原 id）；缺 id 无法应答的非法调用则从消息上剥离（重建
AIMessage 丢弃该字段），维持"assistant 的每个 tool_call_id 都有紧邻 tool 消息应答"
的 OpenAI 协议不变量。回归测试
`tests/test_orchestrator_agent_discovery.py::test_reason_answers_invalid_tool_calls_to_keep_message_sequence_valid`
模拟 DeepSeek 服务端校验断言消息序合法；修复后 bugfix-impl-004 重跑通过（98.25）。
