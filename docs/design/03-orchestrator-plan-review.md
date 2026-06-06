# Orchestrator 规划审查机制

> 设计文档 v1 — 规划完成后、执行前强制用户审查

## Context

当前 Orchestrator 的 LangGraph 流程中，`reason_node` 产出 plan 后**直接进入** `dispatch → execute`，中间没有任何人工介入点。用户无法在执行前审查、修改或讨论规划。

**目标**：规划完成后暂停，等待用户审查。用户可以批准执行、提供修改意见、或与 Orchestrator 继续讨论。每次产出 plan 都必须审查。

## 方案选型

| 方案 | 描述 | 优点 | 缺点 |
|------|------|------|------|
| A: LangGraph interrupt | `interrupt_before=["dispatch"]` | 原生 checkpoint | 灵活性差，难以支持修改循环 |
| B: 新节点 + AskEventQueue | 复用已有 ask card 模式 | 实现简单 | 与 ask card 职责混淆 |
| **C: 会话状态 + 后端 API 网关** | **新增 awaiting_review 状态 + review API** | **解耦彻底，三端各司其职** | **三端都需改动** |

**选定方案 C**：通过会话状态机管理审查生命周期，后端作为 API 网关转发审查请求，职责清晰。

## 改造后的 Graph

```
┌───────────────────────────────────────────────────────────────────────────┐
│                        改造后的 LangGraph                                   │
│                                                                           │
│  ┌──────────────┐    ┌────────┐    ┌──────────────┐    ┌───────────┐      │
│  │skill_prepare │───▶│ reason │───▶│ human_review │───▶│  dispatch │      │
│  └──────────────┘    └────────┘    └──────┬───────┘    └─────┬─────┘      │
│                           ▲               │                   │           │
│                           │        ⏸ 等待用户审查           │           │
│                           │               │                   │           │
│                           │         route_by_review_decision  │           │
│                           │         ┌───────┴────────┐        │           │
│                           │         ▼                ▼        ▼           │
│                           │      "approve"     "discuss"/"modify"        │
│                           │         │                │                    │
│                           │         ▼                │                    │
│                           │      dispatch            │                    │
│                           │         │                                     │
│                           │         ▼                                     │
│                           │      execute ──▶ review ──▶ evolve ──▶ save_mem│
│                           │                │                              │
│                           │           needs_replan=true                    │
│                           │                │                              │
│                           └────────────────┘                              │
│                      （replan 也经过 human_review — 每次必审）             │
└───────────────────────────────────────────────────────────────────────────┘
```

注意：replan 路径 `review → skill_prepare → reason → human_review` 也会触发审查。「每次必审」由 graph 拓扑天然保证。

## 三端数据流

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                        完整数据流时序                                         │
│                                                                              │
│  ① 正常审查流程                                                               │
│  ─────────────                                                               │
│                                                                              │
│  AgentEnd                    Backend                   Frontend              │
│  ────────                    ───────                   ────────              │
│                                                                              │
│  reason_node 产生 plan                                                       │
│       │                                                                      │
│       ▼                                                                      │
│  human_review_node                                                           │
│       │                                                                      │
│       ├── session: running → awaiting_review                                 │
│       │                                                      │               │
│       ├── SSE: plan_review { plan, waves, ... } ──────────────────▶│       │
│       │                                    (StreamWriter 写入Redis)  │       │
│       │                                                      ▼               │
│       │                                              ┌─────────────────┐    │
│       │                                              │ PlanReviewCard  │    │
│       │                                              │                 │    │
│       │                                              │ Step 1: xxx     │    │
│       │                                              │ Step 2: xxx     │    │
│       │                                              │ Step 3: xxx     │    │
│       │                                              │                 │    │
│       │                                              │ [批准] [讨论]    │    │
│       │                                              │ [输入框...]      │    │
│       │                                              └────────┬────────┘    │
│       │                                                       │              │
│       ⏸ await event.wait()                                   │              │
│       .                                                       │              │
│       .        ② 用户提交审查 ◀───────────────────────────────┘              │
│       .                       │                                              │
│       .                       ▼                                              │
│       .              POST /tasks/:id/review                                 │
│       .              { action: "approve" }                                  │
│       .                       │                                              │
│       .                       ▼                                              │
│       .              POST /v1/agent/review ──────▶                          │
│       .                                             │                        │
│       ◀─────────────────────────────────────────────┘                        │
│       │  event.set({ decision: "approve" })                                  │
│       ▼                                                                      │
│  route_by_review_decision → "dispatch"                                      │
│       │                                                                      │
│       ├── session: awaiting_review → running                                 │
│       ▼                                                                      │
│  dispatch → execute → review → evolve → save_mem → DONE                    │
│                                                                              │
│                                                                              │
│  ③ 讨论/修改流程（循环）                                                      │
│  ────────────────────                                                        │
│                                                                              │
│  用户在 PlanReviewCard 输入框写:                                              │
│    "第三步不需要了，加一步先跑 lint"                                          │
│    action: "discuss"                                                         │
│       │                                                                      │
│       ▼                                                                      │
│  POST /tasks/:id/review { action: "discuss", content: "..." }               │
│       │                                                                      │
│       ▼                                                                      │
│  AgentEnd: event.set({ decision: "discuss", message: "..." })               │
│       │                                                                      │
│       ▼                                                                      │
│  route_by_review_decision → "reason"                                        │
│       │                                                                      │
│       ▼                                                                      │
│  reason_node (带用户反馈上下文) → 产生新 plan                                │
│       │                                                                      │
│       ▼                                                                      │
│  human_review_node → SSE: plan_review → ⏸ 等待                              │
│       │                                                                      │
│      ... 循环，直到用户 approve ...                                           │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## 各端改动清单

### 1. 契约层（contracts/schemas/）

#### session-state.yaml

新增 `awaiting_review` 状态及合法转换：

```yaml
SessionState:
  enum:
    - idle
    - running
    - awaiting_review   # ← 新增：等待用户审查规划
    - completed
    - interrupted
    - error
    - inactive

SessionStateTransitions:
  properties:
    idle:
      const: [running]
    running:
      const: [completed, interrupted, error, awaiting_review]  # ← 新增
    awaiting_review:         # ← 新增整个 block
      const: [running]       # 审查后恢复执行（approve 或 discuss 都回到 running）
```

#### event-types.yaml

新增 `plan_review` 事件类型：

```yaml
EventType:
  enum:
    # ... 现有事件 ...
    - plan_review       # ← 新增：规划审查请求，携带完整 plan 供前端展示
```

### 2. AgentEnd 改动

#### orchestrator/planning/graph.py

**GraphState 新增字段**：

```python
class GraphState(TypedDict):
    # ... 现有字段 ...
    review_decision: str       # "approve" | "discuss" | "modify"
    review_message: str        # 用户的反馈内容
```

**新增 human_review_node**：

```python
# 全局注册表：session_id → (Event, result)
_pending_reviews: dict[str, asyncio.Event] = {}
_review_results: dict[str, dict] = {}

async def human_review_node(state: GraphState) -> dict:
    session_id = state["task_id"]

    # 1. 注册等待事件
    event = asyncio.Event()
    _pending_reviews[session_id] = event

    # 2. 阻塞等待用户响应（通过外部 API 唤醒）
    await event.wait()

    # 3. 读取审查结果
    result = _review_results.pop(session_id, {})
    _pending_reviews.pop(session_id, None)

    return {
        "review_decision": result.get("action", "approve"),
        "review_message": result.get("content", ""),
    }
```

**新增路由函数**：

```python
def route_by_review_decision(state: GraphState) -> str:
    decision = state.get("review_decision", "approve")
    if decision == "approve":
        return "dispatch"
    return "reason"  # discuss / modify 都回到 reason
```

**修改现有路由**：

```python
def route_by_output_type(state: GraphState) -> str:
    output_type = state.get("output_type", "error")
    if output_type == "text":
        return "save_mem"
    elif output_type == "plan":
        return "human_review"   # ← 改为 human_review（原为 dispatch）
    else:
        return END
```

**build_graph() 改动**：

```python
def build_graph() -> StateGraph:
    graph = StateGraph(GraphState)

    graph.add_node("skill_prepare", skill_prepare_node)
    graph.add_node("reason", reason_node)
    graph.add_node("human_review", human_review_node)  # ← 新增
    graph.add_node("dispatch", dispatch_node)
    graph.add_node("execute", _execute_placeholder)
    graph.add_node("review", review_node)
    graph.add_node("evolve", evolve_node)
    graph.add_node("save_mem", save_mem_node)

    graph.set_entry_point("skill_prepare")

    graph.add_edge("skill_prepare", "reason")
    graph.add_conditional_edges("reason", route_by_output_type)
    graph.add_conditional_edges("human_review", route_by_review_decision)  # ← 新增
    graph.add_edge("dispatch", "execute")
    graph.add_edge("execute", "review")
    graph.add_conditional_edges("review", route_by_review)
    graph.add_edge("evolve", "save_mem")
    graph.set_finish_point("save_mem")

    memory = MemorySaver()
    return graph.compile(checkpointer=memory)
```

#### adapters/orchestrator.py

在 `stream_chat()` 的节点分发逻辑中新增 `human_review` 分支：

```python
elif node_name == "human_review":
    plan = current_state.get("plan")
    yield StreamEvent.create(
        EventType.PLAN_REVIEW,
        plan=plan.model_dump() if plan else {},
        waves=current_state.get("execution_waves", []),
    )
```

`reason_node` 产出 plan 时的 SSE 事件保持不变（`PLANNING` + `TEXT`），这些事件在 `human_review` 之前已经发送，前端可以先展示规划思考过程。

#### api/v1/agent.py

新增 `/review` 端点：

```python
@router.post("/review")
async def submit_review(request: ReviewRequest):
    session_id = request.session_id
    if session_id not in _pending_reviews:
        raise HTTPException(404, "No pending review for this session")

    _review_results[session_id] = {
        "action": request.action,    # approve / discuss / modify
        "content": request.content,  # 用户反馈
    }
    _pending_reviews[session_id].set()
    return {"status": "ok"}
```

#### session/models.py

新增 `AWAITING_REVIEW` 状态，更新合法转换表。

### 3. Backend 改动

#### controller/impl/task_controller.go

新增 `ReviewTask` 方法：

```go
func (h *Handler) ReviewTask(c *gin.Context) {
    // 1. 解析请求（session_id, action, content）
    // 2. 验证 session 状态为 awaiting_review
    // 3. 转发到 agentend POST /v1/agent/review
    // 4. 返回 agentend 响应
}
```

#### router/

注册新路由：

```go
tasks.POST("/:taskId/review", h.ReviewTask)
```

#### pkg/agentend_client/client.go

新增 `ReviewAgent` 方法，发送 POST `/v1/agent/review` 到 agentend。

> 注意：当前 Backend 已演进为 Controller → Service → DAO 三层架构，实际改动文件位于 `controller/impl/task_controller.go` 和 `service/impl/task_service.go`。

### 4. Frontend 改动

#### hooks/use-chat-stream.ts

在 SSE 事件 switch 中新增 `plan_review` 分支，触发 PlanReviewCard 渲染。

#### components/PlanReviewCard.tsx（新增）

规划审查 UI 组件，包含：

- 规划步骤列表（只读展示）
- 执行波次可视化
- 「批准执行」按钮 → `POST /api/tasks/:id/review { action: "approve" }`
- 讨论输入框 → `POST /api/tasks/:id/review { action: "discuss", content: "..." }`
- 修改输入框 → `POST /api/tasks/:id/review { action: "modify", content: "..." }`

#### 消息发送路由

当 session 状态为 `awaiting_review` 时，聊天输入框的消息走 `/review` API 而非 `/run` API。

## discuss 流程中 reason 产出 text 的处理

当用户 discuss 后 reason_node 可能返回 `text`（直接文本回答）而非新的 `plan`。此时 graph 走 `save_mem → END`，审查流程结束。

这意味着 LLM 判断当前讨论不需要生成新的 plan，直接回答用户即可。这是合理行为——用户可能只是问了一个关于规划的澄清问题，LLM 回答后前端应回到正常聊天状态。

如果需要强制 reason 在审查模式下始终产出 plan，可以在 reason_node 注入 system prompt 约束。初始版本建议不强制，观察实际使用体验后再决定。

## 边界情况

### 超时机制

`human_review_node` 如果用户一直不响应，graph 永远挂着。需要超时保护：

```python
async def human_review_node(state: GraphState) -> dict:
    event = asyncio.Event()
    _pending_reviews[session_id] = event

    timeout = get_settings().review_timeout  # 可配置，默认 600s（10 分钟）
    try:
        await asyncio.wait_for(event.wait(), timeout=timeout)
    except asyncio.TimeoutError:
        return {"review_decision": "approve", "review_message": "审查超时，自动继续"}
    finally:
        _pending_reviews.pop(session_id, None)
        _review_results.pop(session_id, None)
```

超时值通过 `config.py` Settings 暴露为 `review_timeout`，默认 10 分钟（600 秒）。生产环境中可根据使用场景调整——短任务可设为 3 分钟，复杂审查可设为 30 分钟。

### 进程崩溃恢复

`_pending_reviews` 和 `_review_results` 是纯内存字典。如果 agentend 进程崩溃：

1. 内存中的等待状态丢失
2. 但 LangGraph 的 MemorySaver 已将 graph checkpoint 持久化（包含 `human_review` 节点的入口状态）
3. 进程重启后，graph 可以从 checkpoint 恢复

**恢复策略**：重启后从 checkpoint 恢复 graph 时，`human_review_node` 会重新执行，重新注册 `_pending_reviews` 中的 Event 并等待。此时前端如果还在等待，用户可以正常提交审查。

**如果重启后前端已断开**：session 状态会停留在 `awaiting_review`。需要两种恢复路径：

- **前端重连**：用户重新打开页面，前端拉取消息历史，看到 `plan_review` 事件后重新渲染 PlanReviewCard，用户继续审查
- **超时兜底**：重启后的 `human_review_node` 有独立的超时计时，超时后自动 approve 并结束

不需要额外的持久化层——MemorySaver checkpoint + 超时兜底足以覆盖崩溃场景。

### 连接断开

用户关闭浏览器，SSE 断开，但 graph 还在等。MemorySaver 已保存 checkpoint，重连后可恢复到 `human_review` 状态继续审查。

### SSE Keepalive

`human_review_node` 通过 `event.wait()` 阻塞时，graph 的 `astream()` 不会产出新的 chunk。此时 SSE 流进入空闲状态，可能被反向代理（Nginx 等）超时断开。

**解决方案**：利用现有的 SSE heartbeat 机制。当前 Backend 的 StreamHandler 已有 15 秒心跳间隔（`stream.go` 中的 heartbeat 机制）。在 `human_review` 等待期间，OrchestratorAdapter 需要持续发送 SSE heartbeat 注释（`: ping\n\n`），保持连接活跃。

具体实现：在 `stream_chat()` 的 `while not graph_finished` 循环中，当收到 `human_review` 节点输出后，启动一个后台任务定期向 update_queue 投递 heartbeat signal，直到 graph 恢复或超时。

### 并发审查

`_pending_reviews` 字典以 session_id 为 key，天然保证同一 session 只有一个 pending review。

### discuss 时的 SSE 流

用户 discuss 后 graph 回到 reason_node 重新规划，planning/text 事件通过已有 SSE 通道自然流到前端。前端需正确处理「审查中」状态下的这些事件——展示为讨论对话，而非独立的新规划。

## 改动规模估算

| 模块 | 改动量 | 说明 |
|------|--------|------|
| contracts/schemas/ | ~20 行 | 2 个 yaml 文件 |
| agentend/ | ~120 行 | graph + adapter + api + heartbeat |
| backend/ | ~50 行 | handler + router |
| frontend/ | ~150 行 | 组件 + hook |
| **总计** | **~340 行** | 核心机制简洁，复杂度在前端组件 |
