# Bug Fix: 子 Agent 消息持久化 — SSE 挂起 + 回复错乱 + 消息不分离

> 日期: 2026-05-29
> 关联 Change: `openspec/changes/sub-agent-message-persistence/`

## 现象

实现子 Agent 消息持久化（ExecutionEngine 改为回调后端 RunTask API）后，出现六个问题：

1. **SSE 永远「正在回复」** — 前端订阅的 orchestrator SSE 流永远不结束
2. **「管理者」消息显示内部规划** — orchestrator 回复内容是 LLM 的原始规划推理，而非汇总结果
3. **子 Agent 的 dispatch content 被当作用户消息显示** — "执行 pwd 命令..." 等内部指令出现在聊天中
4. **多 Agent 消息不分离** — orchestrator 规划、子 Agent 回复、最终总结全部混为一条消息，无法区分哪个 Agent 说了什么
5. **SSE 事件被拆分为多个** — 一个 agentend SSE 事件被拆为 `event:` 和 `data:` 两个独立事件推给前端，导致流式输出为空或碎片化
6. **数据库保存内容为片段** — MySQL 中只存了最后 ~500 字符的片段，而非完整文本

---

## 根因分析

### Bug 1: SSE 永远挂起

**根因**: `BackendClient.stream_result()` 使用 `httpx.aiter_lines()` 消费后端 SSE 长连接。httpx 的 `aiter_lines()` 对 SSE 协议的 `\n\n` 事件分隔处理不可靠，导致 async generator 永远挂起。

**调用链**:

```
OrchestratorAdapter.stream_chat()
  → ExecutionEngine._execute_task()
    → BackendClient.stream_result()   ← 卡在这里
      → httpx.aiter_lines()           ← 永远不返回
```

`stream_result()` 挂起 → `_execute_task()` 无法 yield `RUNTIME_COMPLETED` → `stream_chat()` 卡在 Phase 3 → 前端永远收不到 DONE 事件。

**修复** (`agentend/src/clients/backend_client.py`):

- 改用独立 `httpx.AsyncClient`（长 read timeout）+ `aiter_text()` + 手动缓冲区拆行
- `_execute_task()` 中用 `asyncio.wait_for(_collect_stream(), timeout=timeout)` 加超时保底
- 循环结束后 drain 剩余缓冲区，防止最后一个事件丢失

```python
# Before (不可靠)
async with self._client.stream("GET", url, params=params) as resp:
    async for line in resp.aiter_lines():  # ← 可能永远挂起
        ...

# After (可靠)
sse_client = httpx.AsyncClient(timeout=httpx.Timeout(connect=10, read=600, write=10, pool=10))
async with sse_client.stream("GET", url, params=params) as resp:
    buf = ""
    async for text_chunk in resp.aiter_text():  # ← 逐块接收
        buf += text_chunk
        while "\n" in buf:
            line, buf = buf.split("\n", 1)
            # ... 解析 SSE data 行
    # drain 剩余缓冲区
    if buf.strip():
        ...
```

### Bug 2: Orchestrator 回复显示内部规划

**根因**: `OrchestratorAdapter` Phase 4 的 TEXT 事件使用了 `overview`（LLM 的原始规划推理），而非 `aggregated`（汇总后的干净结果）。

```python
# Before
yield StreamEvent.create(EventType.TEXT, text=overview)
# overview 内容类似: "用户询问所有可用 agent，因此规划一个任务让该 agent 执行..."

# After
yield StreamEvent.create(EventType.TEXT, text=aggregated or overview)
# aggregated 是 Aggregator 合并子 Agent 结果后的干净摘要
```

**文件**: `agentend/src/adapters/orchestrator.py` Phase 4

### Bug 3: 子 Agent 的 dispatch content 被当作用户消息

**根因**: ExecutionEngine 调用 `POST /api/tasks/:taskId/run` 时，后端 RunTask 总是为子 Agent session 创建 user message。前端 `getTaskMessages` 返回所有 session 的所有消息，包括这条内部指令。

**修复**:

1. **后端** (`backend/internal/handler/task.go`): `RunTaskReq` 新增 `SkipUserMessage bool` 字段，当为 `true` 时跳过创建 user message

```go
type RunTaskReq struct {
    Message         string `json:"message" binding:"required"`
    AgentType       string `json:"agent_type"`
    SessionID       string `json:"session_id" binding:"required"`
    Cwd             string `json:"cwd"`
    SkipUserMessage bool   `json:"skip_user_message"`
}

// RunTask handler 中:
if !req.SkipUserMessage {
    // 创建 user message
}
```

2. **AgentEnd** (`agentend/src/clients/backend_client.py`): `run_task()` 请求体中传 `"skip_user_message": True`

### Bug 4: 多 Agent 消息不分离

**根因**: `OrchestratorAdapter.stream_chat()` 在整个流程中只 yield 了一个 TEXT 事件（Phase 4 的汇总）。子 Agent 的响应在 `ExecutionEngine._execute_task()` 内部通过 `collected` 收集，存入 `TaskResult.content`，但从未作为 TEXT 事件发给前端。规划内容（overview）也仅作为 PLANNING 事件发送（前端渲染为 runtime card，非聊天消息）。

**期望效果**:

```
Orchestrator：我来规划。一号做这个，二号做这个。
一号：我xxx
二号：我也xxx
Orchestrator：进行一个简要的总结
```

**实际效果**: 仅显示一条汇总消息，规划、子 Agent 回复全部丢失。

**前端 agent 切换机制**: 前端 `streamAgentUpdate` 已支持 SSE 流内 agent 切换 — 当 TEXT 事件携带 `agent` + `agent_type` 字段且与当前 streaming agent 不同时，自动将当前内容终结为一条独立消息并开始新消息。问题在于 orchestrator 端从未发送带 agent 元数据的 TEXT 事件。

**修复** (`agentend/src/adapters/orchestrator.py`):

1. **Phase 1（规划）**: 规划完成后 yield TEXT 事件，携带 orchestrator agent 信息

```python
yield StreamEvent.create(
    EventType.TEXT, text=overview, agent="Orchestrator", agent_type="orchestrator"
)
```

2. **Phase 3（执行）**: 每个子 Agent 完成后 yield TEXT 事件，携带子 agent 信息

```python
dispatch_map = {dr.task_id: dr for dr in dispatch_results}
async for event, result in engine.execute(dispatch_results):
    yield event
    if result is not None:
        # ...
        dr = dispatch_map.get(result.task_id)
        yield StreamEvent.create(
            EventType.TEXT,
            text=result.content or "(no output)",
            agent=result.agent,
            agent_type=dr.agent_type if dr else "unknown",
        )
```

3. **Phase 4（汇总）**: TEXT 事件携带 orchestrator agent 信息，触发从子 agent 切回 orchestrator

```python
yield StreamEvent.create(
    EventType.TEXT,
    text=aggregated or overview,
    agent="Orchestrator",
    agent_type="orchestrator",
)
```

**前端消息分离效果**: 每个 TEXT 事件带不同的 `agent`/`agent_type` → `streamAgentUpdate` 检测 agent 变化 → 自动终结当前内容为独立消息 → 最终产生 4 条分离的聊天消息。

### Bug 5: SSE 事件被拆分为多个（流式输出为空）

**根因**: agentend 使用 `sse_starlette` 序列化 SSE 事件，每个事件产生两行：

```
event: text
data: {"type":"text","content":{...}}

```

后端 goroutine (`task.go`) 使用 `bufio.Scanner` 逐行读取，**每行独立发布到 Redis Stream**。`serveStreaming` 从 Redis 读取时，每条记录追加 `\n\n` 发送给前端，导致一个 agentend 事件变成两个独立 SSE 事件：

```
event: text\n\n       ← 命名事件，前端 onmessage 无法捕获
data: {...}\n\n       ← 默认消息，前端 onmessage 捕获
```

后果：
- `event:` 行被前端忽略（无 `data:` 字段），浪费带宽
- 子 Agent SSE 流经 `BackendClient.stream_result()` 消费时，`event:` 和 `data:` 行在 Redis 中解耦，极端情况下 `data:` 行可能丢失，导致收集到的内容为空
- 前端看到"一条消息被拆成多条"或"输出为空"

**修复** (`backend/internal/handler/task.go`):

后端 goroutine 跳过 `event:` 行和空行，仅将 `data:` 行发布到 Redis：

```go
sw.Run(func(fn func(string)) {
    for scanner.Scan() {
        line := scanner.Text()
        // Skip SSE event type lines and blank separators
        if line == "" || strings.HasPrefix(line, "event:") {
            continue
        }
        fn(line)
    }
    // ...
})
```

### Bug 6: 数据库保存内容为片段

**根因**: `StreamWriter.doFlush()` 每次刷写后执行 `sw.buf.Reset()` + `sw.bufLen = 0`，然后仅将当前 buffer（~500 字符）写入 MySQL 的 `content` 字段，**覆盖**了之前已写入的内容。

**调用链**:

```
TEXT 事件到达 → appendText("陈曦感到...")   → buf 累积到 500 字符
  → doFlush() → buf.Reset() → MySQL content = "前 500 字"（覆盖！）
TEXT 事件到达 → appendText("眼泪不受控制...") → buf 重新累积到 500 字符
  → doFlush() → buf.Reset() → MySQL content = "第二个 500 字"（覆盖！）
...
最终 → MySQL 只剩最后一段 ~500 字符的片段
```

**修复** (`backend/internal/stream/writer.go`):

- 不再 `Reset()` buffer，保留完整累积文本
- 新增 `flushedLen` 字段跟踪已刷写长度，阈值判断用 `bufLen - flushedLen >= 500`
- 每次 `doFlush` 写入 **全量** buffer 内容到 MySQL

```go
// Before (覆盖)
sw.buf.Reset()
sw.bufLen = 0
// MySQL: content = 仅当前 buffer（~500 字符）

// After (全量追加)
sw.flushedLen = sw.bufLen  // 标记已刷写到哪
// 不 Reset buffer
// MySQL: content = buf.String()（完整累积文本）
```

---

## 修改文件清单

| 文件 | 变更 |
|------|------|
| `agentend/src/clients/backend_client.py` | SSE 改用 `aiter_text()` + 手动拆行 + 缓冲区 drain；传 `skip_user_message` |
| `agentend/src/orchestrator/execution/engine.py` | SSE 消费加 `asyncio.wait_for` 超时保底 |
| `agentend/src/adapters/orchestrator.py` | Phase 1/3/4 yield 带 agent 元数据的 TEXT 事件；TEXT 从 `overview` 改为 `aggregated or overview` |
| `backend/internal/handler/task.go` | RunTaskReq 新增 `SkipUserMessage` + `Cwd` 字段；goroutine 跳过 `event:` 和空行，仅发布 `data:` 行到 Redis（**注：后端已重构为 controller + service 分层，对应文件现为 `controller/impl/task_controller.go` + `service/impl/task_service.go`**） |
| `backend/internal/stream/writer.go` | `doFlush` 不再 Reset buffer，改为全量写入 MySQL；新增 `flushedLen` 跟踪已刷写长度 |

---

## 教训

1. **httpx 不适合直接消费 SSE**: `aiter_lines()` 对长连接 SSE 的处理不可靠，应使用 `aiter_text()` 或 `aiter_bytes()` 手动解析
2. **SSE 消费必须加超时**: 任何 async generator 消费外部 SSE 的地方都应用 `asyncio.wait_for` 包裹，防止无限挂起
3. **内部 API 调用应与前端调用区分**: orchestrator 内部回调 RunTask 的语义不同于前端发起的 RunTask，需要通过参数（如 `skip_user_message`）区分行为
4. **多 Agent 流必须 yield 带 agent 元数据的 TEXT 事件**: 仅 yield RUNTIME/PLANNING 事件无法产生聊天消息。前端 `streamAgentUpdate` 已支持 agent 切换，但前提是 TEXT 事件必须同时携带 `agent` + `agent_type` 字段
5. **SSE 缓冲区必须 drain**: `aiter_text()` 循环结束后，缓冲区可能残留最后一个未以 `\n` 结尾的事件（如连接异常断开），必须处理后才能关闭
6. **SSE 中转必须保持事件原子性**: `sse_starlette` 等库将一个 SSE 事件拆为 `event:` + `data:` 多行输出。中转层（如 Redis Stream）若逐行存储，会导致一个事件被拆为多个独立 SSE 事件推给下游。中转时应仅存储 `data:` 行，跳过 `event:` 和空行
7. **增量刷写不能用 Reset+覆盖模式**: 分批将流式内容持久化到数据库时，每次刷写必须写入**全量累积内容**（或用 SQL CONCAT 追加），绝不能 Reset buffer 后仅写当前片段——否则历史内容会被覆盖，最终只剩最后一段
