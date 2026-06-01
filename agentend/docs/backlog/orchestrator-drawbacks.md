# Orchestrator 模块深度弊端分析

## 一、架构设计弊端

### 1.1 LangGraph 依赖

当前 graph 有 7 个节点（`skill_prepare → reason → dispatch → execute → review → evolve → save_mem`）并使用 conditional routing 和 `MemorySaver` checkpointer。LangGraph 的价值（条件分支、状态回滚、checkpoint）已得到部分利用，但 7 节点对于 call-L → dispatch → review 核心流程来说仍有冗余。

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

### 1.3 执行闭环已部分实现 — "规划 + 调度 + 聚合"

```
当前流程:
  用户需求 ──▶ Orchestrator ──▶ plan ──▶ write_shared ──▶ dispatch ──▶ collect ──▶ aggregate ──▶ evolution
```

已实现：
- **调度**：`Dispatcher` 将 `PlanOutput` 转为 `DispatchResult`，产出 `@agent` 调度 JSON
- **执行**：`ExecutionEngine` 按波次驱动 Agent（short-circuit CLI 或 HTTP fallback）
- **聚合**：`Aggregator` 调用 LLM 汇总多 Agent 结果
- **重规划**：`review_node` 检查失败任务，通过 conditional routing 触发 skill_prepare → reason 重规划（最多 3 次迭代）
- **Ask Agent**：Reason 阶段可通过 `_handle_ask_agent_call` 向指定 Agent 提问
- **经验记录**：`EvolutionStore` 记录编排成败，注入下次 prompt
- **Pin 约束**：`PinMemory` 持久化共享约束
- **状态追踪**：`RuntimeState` 内存跟踪 task 状态

仍存在的问题：
- **分布式执行**：dispatch 产出 JSON 但不直接驱动 Agent 执行，需外部消费

### 1.4 与 Workspace 系统割裂

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

已修复：`_extract_json` 现在捕获 `json.JSONDecodeError`，返回 `None` 而非崩溃。`plan_node` 也包裹了 `try/except Exception`，失败时返回 `{"plan": None}` 而非冒泡 500。

但仍存在的问题：
- **LLM 可能输出多个代码块** — `re.search` 只取第一个，可能是示例而非最终答案
- **LLM 可能在 JSON 前后加解释文字** — 如果没加代码块包裹，直接 `json.loads(text)` 会返回 None
- **LLM 可能在 JSON 内部加注释** — 标准 JSON 不支持注释，但 LLM 经常加
- **plan 为 None 后仍继续** — `plan_node` 返回 None 后 OrchestratorAdapter 检查并 yield ERROR，但无重试

### 2.2 每次调用新建 LLM 实例

`reason_node`（原 `plan_node`）每次调用都创建新的 `ChatOpenAI` 实例：

```python
async def reason_node(state: GraphState) -> dict:
    llm = ChatOpenAI(
        model=settings.llm.model,
        base_url=settings.llm.base_url,
        api_key=settings.llm.api_key,
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

`.md` 文件中 `> agent: {task.session_id}` 是 LLM 输出的 agent ID（如 `claude-code`），但 config.yaml 中同一个 task 的 `session_id` 已被替换为真实 session（如 `cc-orch-test`）。

Agent 读 `task-001.md` 看到 `> agent: claude-code`，但 `taskctl summary` 按 `cc-orch-test` 过滤——`.md` 文件头部的 agent 标注具有误导性。

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

### 5.1 零测试覆盖

Orchestrator 模块完全没有任何单元测试。考虑到 LLM 输出的不确定性，这是最大的风险点。

### 5.2 零日志（orchestrator/ 内部）

`orchestrator/` 目录内部的 `planning/graph.py`、`execution/dispatcher.py`、`reporting/aggregator.py` 等模块没有 `logger` 调用。但外层的 `opencode.py`、`workspace/manager.py` 等已有日志。生产环境中 orchestrator 规划失败时没有可观测性。

### 5.3 硬编码的 Prompt — 无法动态调整

`PLAN_PROMPT` 是 Python 字符串常量。要修改 prompt 必须修改源代码并重启服务。不能按 task 类型使用不同 prompt、通过 config 配置、A/B 测试。

### 5.4 5 个任务上限是 Prompt 约束而非代码约束

Prompt 中说 "任务数量不超过 5 个"，但 `PlanOutput` model 没有对 `tasks` 列表长度的校验。LLM 完全可能忽略，返回更多任务。

---

## 六、安全弊端

### 6.1 shared_dir 路径注入

`shared_dir` 来自用户请求的 `config` 字段，没有做任何路径校验。攻击者可以传入：

```json
{"shared_dir": "/etc"}
```

`write_shared_node` 会直接 `Path("/etc/plans").mkdir(parents=True, exist_ok=True)` 并写入文件。**任意目录文件写入漏洞**。

对比 `ScopeRule` 对 `workspace_path` 做了 `startswith("/")` 校验，但 `shared_dir` 完全绕过了规则引擎。

### 6.2 LLM 输出直接写入文件系统

`plan.tasks[i].content` 由 LLM 生成，直接通过 `write_text()` 写入 `.md` 文件。如果 LLM 被诱导生成恶意内容，会原封不动地写入磁盘。

---

## 七、总览

```
┌─────────────┬──────────────────────────────────────────────────┐
│ 架构        │ • LangGraph 过度依赖（两节点线性管道）            │
│             │ • OrchestratorAdapter 违反 LSP                   │
│             │ • 执行闭环部分实现（callback 需外部实现）          │
│             │ • 与 Workspace 体系完全割裂                       │
├─────────────┼──────────────────────────────────────────────────┤
│ 可靠性      │ • JSON 提取脆弱（已有 fallback，但仍可能返回 None）│
│             │ • 每次调用新建 LLM 实例                           │
│             │ • 同步文件 I/O 阻塞事件循环                       │
│             │ • assert 做控制流                                 │
│             │ • 无重试机制                                      │
├─────────────┼──────────────────────────────────────────────────┤
│ 数据一致性  │ • files_written 与实际文件名不一致 (Bug)          │
│             │ • task.md agent 标注与 config.yaml 不一致         │
│             │ • GraphState 用 TypedDict 非 Pydantic             │
├─────────────┼──────────────────────────────────────────────────┤
│ 性能        │ • 每次调用新建 LLM 实例（无法复用连接池）          │
│             │ • 同步文件 I/O 阻塞事件循环                       │
├─────────────┼──────────────────────────────────────────────────┤
│ 可维护性    │ • 零测试覆盖                                      │
│             │ • orchestrator/ 内部零日志                        │
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
| **P0** | shared_dir 路径注入 | 安全漏洞，可被外部利用 |
| **P1** | JSON 解析仍脆弱 | 解析失败返回 None，无重试机制 |
| **P1** | 每次调用新建 LLM 实例 | 高并发时连接开销大 |
| **P2** | 零测试覆盖 | 任何改动都可能引入回归 |
