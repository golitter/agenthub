# Orchestrator 模块深度弊端分析

## 一、架构设计弊端

### 1.1 LangGraph 依赖

当前 graph 有 8 个节点（`skill_prepare → reason → human_review → dispatch → execute → review → evolve → save_mem`）并使用 conditional routing（`graph.compile()` 未启用 checkpointer，跨轮状态依赖外部 `ConversationMemoryStore` 持久化，而非 LangGraph 的状态回滚）。LangGraph 的价值（条件分支、状态在节点间显式流转）已得到部分利用，但 8 节点对于 call-L → dispatch → review 核心流程来说仍有冗余。

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
- **重规划**：`review_node` 检查失败任务，通过 conditional routing 触发 skill_prepare → reason 重规划（最多 3 次迭代）
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

### 2.1 JSON 提取仍然脆弱（部分缓解）

```python
def _extract_json(text: str) -> dict | None:
    try:
        match = re.search(r"```(?:json)?\s*\n?(.*?)\n?```", text, re.DOTALL)
        if match:
            return json.loads(match.group(1))
        return json.loads(text)
    except json.JSONDecodeError:
        return None
```

已修复：`reason_node` 改用 LangChain tool-calling（`llm.bind_tools()`），不再依赖 `_extract_json` 手动解析。工具调用结果由框架结构化处理，JSON 解析失败风险大幅降低。

但仍存在的问题：
- **LLM 可能不调用工具** — tool-calling 不保证 LLM 一定使用 `plan_and_dispatch` 工具，可能直接输出文本
- **工具参数可能不完整** — LLM 生成的 PlanOutput 可能缺少必要字段（如 agent 分配），需 Pydantic 校验兜底
- **plan 为 None 后仍继续** — `reason_node` 未产出 plan 时，OrchestratorAdapter 检查并 yield ERROR，但无重试

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

单次 LLM 调用，没有任何重试。如果 API 超时、返回 429/503、返回不完整 JSON，整个规划直接失败。（注：`_handle_ask_agent_call` 已添加 3 次 run_task 重试，但 reason_node 的 LLM 调用本身仍无重试）

---

## 三、数据一致性弊端

### 3.1 files_written 列表与实际文件名不一致（Bug）[已修复]

~~`_write_shared_plan` 已改为统一使用 LLM 生成的 `task.task_id` 构造文件名和 config.yaml 条目，不再使用 `idx` 索引。~~

### 3.2 task.md 中 agent 标注与 config.yaml 不一致

`task-*.md` 文件头部 `- agent: {agent_id}` 是 agent ID（如 `claude-code`），但 config.yaml 中同一个 task 的 `session_id` 已被替换为真实 session（如 `cc-orch-test`）。

Agent 读 `task-001.md` 看到 `- agent: claude-code`，但 `taskctl summary` 按 `cc-orch-test` 过滤——`.md` 文件头部的 agent 标注具有误导性。

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

执行与呈现层已有 pytest 覆盖：`tests/test_orchestrator_execution.py`（ExecutionEngine 波次失败取消兄弟任务、子 Run 预算继承收紧）与 `tests/test_orchestrator_presentation.py`（最终摘要块结构、reason 错误转 ERROR 事件、observability config）。但 `planning/graph.py` 的节点逻辑（review 路由、replan、skill_prepare）仍缺直接单测，LLM 输出不确定性带来的回归风险仍在。

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

现已在入口校验：`src/api/v1/agent.py` 要求传入的 `shared_dir` 与按 workspace 推导的期望路径（`{repo}/worktrees/{task_id}/shared/.agent`）完全一致，否则返回 HTTP 400；未传时直接采用推导路径。该白名单式校验阻断了指向系统目录的注入，但仍建议在写入路径处补充 `Path.resolve()` 边界检查作为纵深防御（对比 `ScopeRule` 对 `workspace_path` 的 `startswith("/")` 校验）。

### 6.2 LLM 输出直接写入文件系统

`plan.tasks[i].content` 由 LLM 生成，直接通过 `write_text()` 写入 `.md` 文件。如果 LLM 被诱导生成恶意内容，会原封不动地写入磁盘。

---

## 七、总览

```
┌─────────────┬──────────────────────────────────────────────────┐
│ 架构        │ • LangGraph 依赖（8 节点管道，部分冗余）          │
│             │ • OrchestratorAdapter 违反 LSP                   │
│             │ • 执行闭环主干已实现，但缺 durable job/checkpoint   │
│             │ • 与 Workspace 体系完全割裂                       │
├─────────────┼──────────────────────────────────────────────────┤
│ 可靠性      │ • JSON 提取脆弱（已有 fallback，但仍可能返回 None）│
│             │ • 每次调用新建 LLM 实例                           │
│             │ • 同步文件 I/O 阻塞事件循环                       │
│             │ • ~~assert 做控制流~~ [已修复]                    │
│             │ • 无重试机制                                      │
├─────────────┼──────────────────────────────────────────────────┤
│ 数据一致性  │ • ~~files_written 与实际文件名不一致 (Bug)~~ [已修复]│
│             │ • task.md agent 标注与 config.yaml 不一致         │
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
│ 安全        │ • shared_dir 路径注入漏洞                         │
│             │ • LLM 输出直接写入文件系统                        │
└─────────────┴──────────────────────────────────────────────────┘
```

## 八、优先级排序

| 优先级 | 问题 | 理由 |
|--------|------|------|
| ~~**P0**~~ | ~~shared_dir 路径注入~~ [已缓解] | 入口已加白名单校验阻断注入；纵深防御建议见 6.1 |
| **P1** | JSON 解析仍脆弱 | 解析失败返回 None，无重试机制 |
| **P1** | 每次调用新建 LLM 实例 | 高并发时连接开销大 |
| **P2** | 测试覆盖不均 | 执行/呈现层已有测试，`planning/graph.py` 节点逻辑改动仍可能引入回归 |
