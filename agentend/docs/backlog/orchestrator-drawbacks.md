# Orchestrator 模块深度弊端分析

## 一、架构设计弊端

### 1.1 LangGraph 依赖

当前 graph 有 11 个节点（`skill_prepare → compact_context → reason → human_review → dispatch → execute → review → final_aggregate → await_user → evolve → save_mem`）并使用 conditional routing（`graph.compile(checkpointer=MemorySaver())` 已启用进程内 checkpointer，thread_id 使用 Run id；跨进程/跨重启的会话持久化仍依赖外部 `ConversationMemoryStore`，而非 LangGraph 的 durable checkpoint）。LangGraph 的价值（条件分支、状态在节点间显式流转）已得到部分利用，但 11 节点对于 call-LLM → dispatch → review 核心流程来说仍有冗余。

### 1.2 OrchestratorAdapter 违反 Liskov 替换原则

`OrchestratorAdapter` 继承了 `BaseAgentAdapter`，但 5 个方法中有 3 个是 no-op：

```python
async def create_session(self, session_id: str) -> None:
    pass

async def interrupt(self, session_id: str) -> bool:
    return False

async def destroy_session(self, session_id: str) -> None:
    pass
```

Orchestrator **不是**一个 Agent 适配器——它是一个规划器，不应该塞进 `AdapterRegistry`。对比 `ClaudeCodeAdapter` 和 `OpenCodeAdapter` 都有真实的进程管理、会话生命周期。

### 1.3 执行闭环已实现主干 — "规划 + 调度 + 执行 + 聚合"

```
当前流程:
  用户需求 ──▶ Orchestrator ──▶ plan ──▶ write_shared ──▶ dispatch ──▶ collect ──▶ aggregate ──▶ evolution
```

已实现：
- **调度**：`Dispatcher` 将 `PlanOutput` 转为 `DispatchResult`，产出 `@agent` 调度 JSON
- **执行**：`ExecutionEngine` 按波次驱动 Agent，当前统一通过 `BackendClient.run_task()` / `stream_result()` 走 Backend HTTP 路径
- **聚合**：`Aggregator` 调用 LLM 汇总多 Agent 结果
- **重规划**：`review_node` 检查执行/集成双维度失败，通过 conditional routing 触发 skill_prepare → compact_context → reason 重规划（上限由 `orchestrator.replan_max_iterations` 控制，默认 3）
- **Ask Agent**：Reason 阶段可通过 `_handle_ask_agent_call` 向指定 Agent 提问
- **经验记录**：`EvolutionStore` 记录编排成败，注入下次 prompt
- **Pin 约束**：主流程从 Backend pinned announcements 注入；文件型 `PinMemory` 仍保留 `/v1/pin/*` 端点能力
- **状态追踪**：`RuntimeState` 内存跟踪 task 状态

仍存在的问题：
- **持久执行**：执行状态仍主要在本进程与消息流中推进，尚未接入 durable job queue / checkpointer

### 1.4 与 Workspace 系统割裂

> 状态（部分缓解）：Orchestrator 现已通过 `_resolve_workspace()` 调用 `WorkspaceManager.create_task_base()` 创建只读的 `task-base` worktree（用于 reason 阶段读取代码结构，见 `src/api/v1/agent.py` 与 [11-orchestrator-planning.md](../design/11-orchestrator-planning.md)）。但 `shared_dir` 仍由调用方传入、其下文件仍不经 git 追踪——下文批评对 shared_dir 部分仍然成立。

```
Claude/OpenCode:   request → _resolve_workspace → worktree → 安全隔离
Orchestrator:      request → 手动传 shared_dir → 直接写磁盘 → 无隔离、无追踪
```

ClaudeCodeAdapter 和 OpenCodeAdapter 都通过 `_resolve_workspace()` 自动获得隔离的 Git Worktree。但 Orchestrator 完全不参与 Workspace 体系：
- `shared_dir` 由调用方手动传入绝对路径
- 不创建、也不管理任何 Workspace
- 写入的文件没有经过 Workspace 的 git 追踪

---

## 二、可靠性弊端

### 2.1 JSON 提取脆弱 — [已基本修复]

历史上 `reason_node` 依赖 `_extract_json` 用正则从文本中抽取 JSON，解析失败返回 None。`_extract_json` 已从源码中删除：`reason_node` 改用 LangChain tool-calling（`llm.bind_tools()`），`plan_and_dispatch` 的参数由 `_plan_from_tool_call` 构造并 Pydantic 校验，解析失败风险大幅降低。

已有的护栏：
- **强制分派重试** — `_requires_dispatch_intent` 启发式判定请求需要分派而模型只输出文本时，注入重试消息；二次仍失败则由 `_fallback_plan_from_text` 生成单任务兜底计划
- **参数校验回喂** — `_plan_from_tool_call` 抛出的 ValueError 会作为错误 tool result 回给模型重试
- **Agent 发现前置** — `list_available_agents` 必须在上一轮 tool 批次完成后才能使用 `ask_agent` / `plan_and_dispatch`，未发现先分派会被拒绝并重试

仍存在的问题：
- **兜底计划质量** — `_fallback_plan_from_text` 只把原始请求塞给第一个可分派 Agent，不做拆解
- **reason 循环达到 `reason_max_iterations` 上限时只返回提示文本，无跨轮重试**

### 2.2 每次调用新建 LLM 实例

`reason_node`（原 `plan_node`）每次调用都创建新的 `ChatOpenAI` 实例：

```python
async def reason_node(state: GraphState) -> dict:
    llm = ChatOpenAI(
        model=settings.llm.model,
        base_url=settings.llm.base_url,
        api_key=settings.llm.api_key,
        timeout=settings.orchestrator.llm_request_timeout,
    )
```

导致：
- 每次都重新建立 HTTP 连接（无法复用连接池）
- 每次都重新初始化 HTTP 客户端
- 无法利用 langchain 的任何缓存或 rate-limiting 机制

### 2.3 同步文件 I/O 阻塞事件循环

`_write_shared_plan` 在 async graph (`ainvoke`) 中使用同步文件 I/O，会在 `asyncio` 事件循环中造成阻塞：

### 2.4 用 assert 做控制流 [已修复]

~~`assert plan is not None` 已改为 `if not plan: return`（见 `_write_shared_plan` 和 `dispatch_node`）。~~

### 2.5 没有重试机制

单次 LLM 调用，没有任何重试。如果 API 超时、返回 429/503、返回不完整 JSON，整个规划直接失败。（注：`_handle_ask_agent_call` 已添加 3 次 run_task 重试，reason_node 内部对"未调用工具/参数错误"有循环重试，但对 `ainvoke` 的 LLM 调用本身仍无网络级重试）

---

## 三、数据一致性弊端

### 3.1 files_written 列表与实际文件名不一致（Bug）[已修复]

~~`_write_shared_plan` 已改为统一使用 LLM 生成的 `task.task_id` 构造文件名和 config.yaml 条目，不再使用 `idx` 索引。~~

### 3.2 task.md 中 agent 标注与 config.yaml 不一致 [已修复]

~~`task-*.md` 文件头部只写 `- agent: {agent_id}`，而 config.yaml 中同一 task 的 `session_id` 是真实 session，两处语义混用。~~

`_write_shared_plan` 现在在 `task-*.md` 头部同时写入 `- task_id` / `- agent` / `- agent_type` / `- session_id` 四个字段，config.yaml 条目也携带相同的 `agent` 与 `session_id` 键，两处标注一致，`.md` 的 agent 字段不再与 `taskctl summary` 的 session 过滤条件冲突。

### 3.3 GraphState 不是 Pydantic Model — 与全局 schema 体系不一致

整个项目的数据模型都用 Pydantic，唯独 `GraphState` 用了 `TypedDict`，没有：
- 运行时类型校验
- 字段默认值
- 序列化/反序列化能力

---

## 四、性能弊端

### 4.1 同步 LLM 调用在 Async Graph 中

~~`plan_node` 是同步函数（`def` 非 `async def`），`llm.invoke()` 是同步阻塞调用。~~

已改为 `reason_node`（`async def`），使用 `llm.ainvoke()` 异步调用。但每次仍创建新实例，无法复用连接池。
- 所有其他请求排队等待
- SSE 心跳超时
- 健康检查失败

### 4.2 无连接池复用

每次创建 `ChatOpenAI` 实例都创建新的 HTTP 连接。

---

## 五、可维护性弊端

### 5.1 测试覆盖不均 [部分缓解]

执行与呈现层已有 pytest 覆盖：`tests/test_orchestrator_execution.py`（ExecutionEngine 波次失败取消兄弟任务、子 Run 预算继承收紧、review_node 重规划路由与成功结果复用）、`tests/test_orchestrator_presentation.py`（最终摘要块结构、reason 错误转 ERROR 事件、observability config）、`tests/test_orchestrator_agent_discovery.py`（reason 工具循环的发现前置约束、provider 失败部分转录、checkpointer 集成）与 `tests/test_context_compaction.py`（上下文压缩与 CAS 修订冲突）。但 `skill_prepare`、`human_review` 等节点的独立单测仍缺，LLM 输出不确定性带来的回归风险仍在。

### 5.2 零日志（orchestrator/ 内部）— [部分缓解]

`planning/graph.py` 已补充关键日志：重规划达到上限（`logger.warning("Review: max_iterations=%d reached ...")`）、reason 节点达到上限、`save_mem_node` 异常（`logger.exception`）。但 `execution/dispatcher.py`、`reporting/aggregator.py` 等子模块的日志覆盖仍较稀疏，规划失败时的细粒度可观测性不足。

### 5.3 硬编码的 Prompt — 无法动态调整

`REASON_PROMPT` 是 Python 字符串常量。要修改 prompt 必须修改源代码并重启服务。不能按 task 类型使用不同 prompt、通过 config 配置、A/B 测试。

### 5.4 5 个任务上限是 Prompt 约束而非代码约束

Prompt 中说 "任务数量不超过 5 个"，但 `PlanOutput` model 没有对 `tasks` 列表长度的校验。LLM 完全可能忽略，返回更多任务。

---

## 六、安全弊端

### 6.1 shared_dir 路径注入 — [已缓解]

`shared_dir` 来自用户请求的 `config` 字段。历史版本无任何校验，攻击者可传入 `{"shared_dir": "/etc"}` 让 `write_shared_node` 执行 `Path("/etc/plans").mkdir(...)` 造成任意目录写入。

现已在入口校验：`src/api/v1/agent.py` 要求传入的 `shared_dir` 与按 workspace 推导的期望路径（`{repo 上一层目录}/worktrees/{task_id}/shared/.agent`）完全一致，否则返回 HTTP 400；未传时直接采用推导路径。该白名单式校验阻断了指向系统目录的注入，但仍建议在写入路径处补充 `Path.resolve()` 边界检查作为纵深防御（对比 `ScopeRule` 对 `workspace_path` 的 `startswith("/")` 校验）。

### 6.2 LLM 输出直接写入文件系统

`plan.tasks[i].content` 由 LLM 生成，直接通过 `write_text()` 写入 `.md` 文件。如果 LLM 被诱导生成恶意内容，会原封不动地写入磁盘。

---

## 七、总览

```
┌─────────────┬──────────────────────────────────────────────────┐
│ 架构        │ • LangGraph 依赖（11 节点管道，部分冗余）         │
│             │ • OrchestratorAdapter 违反 LSP                   │
│             │ • 执行闭环主干已实现，但缺 durable job/checkpoint   │
│             │ • 与 Workspace 体系部分割裂（见 1.4，shared_dir 仍无 git 追踪）│
├─────────────┼──────────────────────────────────────────────────┤
│ 可靠性      │ • ~~JSON 提取脆弱~~ [已基本修复，见 2.1]          │
│             │ • 每次调用新建 LLM 实例                           │
│             │ • 同步文件 I/O 阻塞事件循环                       │
│             │ • ~~assert 做控制流~~ [已修复]                    │
│             │ • 无网络级重试机制                                │
├─────────────┼──────────────────────────────────────────────────┤
│ 数据一致性  │ • ~~files_written 与实际文件名不一致 (Bug)~~ [已修复]│
│             │ • ~~task.md agent 标注与 config.yaml 不一致~~ [已修复]│
│             │ • GraphState 用 TypedDict 非 Pydantic             │
├─────────────┼──────────────────────────────────────────────────┤
│ 性能        │ • 每次调用新建 LLM 实例（无法复用连接池）          │
│             │ • 同步文件 I/O 阻塞事件循环                       │
├─────────────┼──────────────────────────────────────────────────┤
│ 可维护性    │ • 测试覆盖不均（执行/呈现层有，planning 节点缺）  │
│             │ • orchestrator/ 内部日志稀疏（部分缓解）          │
│             │ • Prompt 硬编码                                   │
│             │ • 5 任务上限仅在 Prompt 中                        │
├─────────────┼──────────────────────────────────────────────────┤
│ 安全        │ • shared_dir 路径注入 [已缓解，见 6.1]            │
│             │ • LLM 输出直接写入文件系统                        │
└─────────────┴──────────────────────────────────────────────────┘
```

## 八、优先级排序

| 优先级 | 问题 | 理由 |
|--------|------|------|
| ~~**P0**~~ | ~~shared_dir 路径注入~~ [已缓解] | 入口已加白名单校验阻断注入；纵深防御建议见 6.1 |
| ~~**P1**~~ | ~~JSON 解析脆弱~~ [已基本修复] | 已切换 tool-calling + 兜底计划 + 参数校验回喂，见 2.1 |
| **P1** | 每次调用新建 LLM 实例 | 高并发时连接开销大 |
| **P2** | 测试覆盖不均 | 执行/呈现层已有测试，`planning/graph.py` 节点逻辑改动仍可能引入回归 |
