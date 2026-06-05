# SSE 流式输出架构

本文档详细描述多 Agent 聊天系统中 SSE（Server-Sent Events）流式输出的完整链路，覆盖 Agentend（生产者）、Backend（中继站 + 持久化）、Frontend（消费者 + 渲染器）三端。

## 全局数据流

```
┌─────────────────────────────────────────────────────────────────────┐
│  用户在输入框发送消息                                                  │
└──────────────┬──────────────────────────────────────────────────────┘
               ▼
┌──────────────────────────┐     POST /api/tasks/:id/run     ┌──────────────────────┐
│  Frontend (useChatStream) │ ──────────────────────────────▶ │  Backend (handler)   │
│  sendMessage()            │                                 │  创建 Message 记录     │
└──────────────┬───────────┘                                 │  启动 StreamWriter    │
               │                                             └──────┬───────────────┘
               │                                                    │ POST /v1/agent/stream
               │                                                    ▼
               │                                          ┌──────────────────────────┐
               │                                          │  Agentend (FastAPI)       │
               │                                          │  _execute_stream()        │
               │                                          │  adapter.stream_chat()    │
               │                                          │  yield SSE events         │
               │                                          └──────────┬───────────────┘
               │                                                     │
               │      ◀───── SSE (data: JSON) ────────────────────────┘
               │              StreamWriter.Run() 消费 agentend SSE
               │              双写: Hub (内存) + Redis Stream + MySQL
               │
               │      GET /api/tasks/:id/stream?session_id=&message_id=
               │      ◀───── SSE (data: JSON) ────────────────────────────
               │              StreamService.ServeStream()
               │              从 Hub + Redis Stream 实时推送
               ▼
┌──────────────────────────┐
│  Frontend (connectSSE)   │
│  EventSource 持久连接     │
│  onEvent → store actions │
│  rAF 批量刷到 Zustand     │
│  React re-render          │
└──────────────────────────┘
```

---

## 一、Agentend（Python）— SSE 事件生产者

### 1.1 入口：`POST /v1/agent/stream`

文件：`agentend/src/api/v1/agent.py`

```python
@router.post("/stream")
async def agent_stream(request: AgentRequest, ...) -> EventSourceResponse:
    # 1. 解析 workspace（git worktree 隔离）
    workspace_path = await _resolve_workspace(request, workspace_mgr)
    # 2. 规则引擎评估（安全/灵魂/群聊等）
    passed, rule_result = rule_engine.evaluate(rule_ctx)
    # 3. 解析会话（新建 or resume）
    session_id, cli_session_id, is_resume = await _resolve_session(...)
    # 4. 返回 SSE 流
    return EventSourceResponse(_execute_stream(...))
```

### 1.2 流式生成器：`_execute_stream()`

文件：`agentend/src/api/v1/agent.py:138-209`

核心逻辑是调用适配器的 `stream_chat()` 异步生成器，逐个 `yield` SSE event：

```python
async def _execute_stream(...):
    raw_events = adapter.stream_chat(session_id, request.message, **stream_kwargs)
    async for event in event_stream:
        yield {
            "event": event.type,               # SSE event 字段（如 "text", "tool_call"）
            "data": event.model_dump_json(),   # SSE data 字段（JSON 字符串）
        }
```

每个 yield 出去的 dict 被 `sse_starlette` 的 `EventSourceResponse` 自动格式化为标准 SSE 格式：

```
event: text
data: {"type":"text","content":{"text":"Hello"},"timestamp":1234.56}
```

### 1.3 适配器层（以 ClaudeCode 为例）

文件：`agentend/src/adapters/claude.py`

```
ClaudeCodeAdapter.stream_chat()
  ├── 启动子进程: claude -p "用户消息" --output-format stream-json --include-partial-messages
  ├── 逐行读取 stdout
  ├── _parse_stream_line() 将 CLI 输出转为 StreamEvent
  │     ├── "stream_event" + "content_block_delta" → EventType.TEXT（逐 token）
  │     ├── "tool_use"                                → EventType.TOOL_CALL
  │     ├── "tool_result"                             → EventType.TOOL_RESULT
  │     ├── "result"                                  → EventType.DONE
  │     └── "system"                                  → EventType.INIT（包含 cli_session_id）
  └── yield StreamEvent
```

**关键点**：`--include-partial-messages` 使 Claude CLI 输出 token 级别的 `stream_event`，从而实现逐字流式。

### 1.4 统一事件模型

文件：`agentend/src/schemas/events.py`

```python
class StreamEvent(_StreamEvent):
    type: str           # EventType 枚举值
    content: dict       # 事件负载
    timestamp: float

    @staticmethod
    def create(event_type, agent_type=None, **kwargs):
        return StreamEvent(type=event_type.value, content=kwargs)
```

事件类型由契约层 `contracts/schemas/*.yaml` 定义，包括：

| EventType | 含义 | content 关键字段 |
|-----------|------|-----------------|
| `init` | 会话初始化 | `cli_session_id` |
| `text` | 文本 token | `text`, `agent_type`, `agent`, `message_id` |
| `tool_call` | 工具调用开始 | `tool`, `args` |
| `tool_result` | 工具调用结束 | `tool`, `result` |
| `done` | 流式结束 | `text`, `usage` |
| `error` | 错误 | `error`, `message` |
| `heartbeat` | 心跳 | — |
| `runtime_executing` | 子任务开始执行 | `task_id`, `agent`, `title` |
| `runtime_completed` | 子任务执行完成 | `task_id`, `agent`, `success` |
| `runtime_text` | 子任务文本输出 | `task_id`, `agent`, `text` |
| `planning` | 规划阶段（任务分发） | `node`, `dispatch` |
| `plan_review` | 规划评审 | `plan`, `waves`, `review_key` |
| `coordination_start` | 多 Agent 协商开始 | — |
| `coordination_message` | 多 Agent 协商消息 | `from`, `to`, `text`, `round` |
| `coordination_done` | 多 Agent 协商结束 | `decisions` |
| `ask_card_start` | Agent 间提问开始 | `question_id`, `source_*`, `target_*`, `question` |
| `ask_card_done` | Agent 间提问结束 | `question_id`, `status`, `summary` |

---

## 二、Backend（Go）— SSE 事件中继站 + 持久化

后端同时承担两个角色：

1. **消费 agentend 的 SSE 流** → 写入 MySQL + Redis + 内存 Hub
2. **向前端提供 SSE 流** → 从 Hub + Redis Stream 读取并推送

### 2.1 消费端：`StreamWriter.Run()`

文件：`backend/internal/stream/writer.go:113-227`

```go
func (sw *StreamWriter) Run(scanFunc func(func(line string)) error) RunOutcome {
    // 启动后台 flush goroutine（每 500ms 批量刷到 MySQL）
    go sw.flushLoop()

    scanFunc(func(line string) {
        if strings.HasPrefix(line, "data: ") {
            var event generated.StreamEvent
            json.Unmarshal([]byte(data), &event)

            switch event.Type {
            case "text":
                // 1. 检测 agent 切换 → switchAgent()（关闭旧 Message，创建新 Message）
                // 2. 追加文本到内存 buffer
                sw.appendText(text)
                // 3. 双通道推送：
                //    热路径: Hub.Publish()（立即推送给前端，低延迟）
                //    冷路径: bufferTextLine()（批量合并后写 Redis Stream）
                sw.bufferTextLine(text)

            case "done":
                sw.flushTextBuffer()
            case "error":
                sawError = true
            case "ask_card_start" / "ask_card_done":
                sw.persistAskCardEvent()
            case "plan_review":
                sw.persistPlanReviewEvent()
            }
        }
        sw.publishToRedis(line)  // 非文本事件直接写 Redis
    })

    // 最终刷写，更新 Message 状态为 completed/failed
    sw.updateMessageStatus(sw.messageID, outcome)
}
```

### 2.2 双通道推送架构

| 通道 | 目标 | 延迟 | 用途 |
|------|------|------|------|
| **Hub（内存 Pub/Sub）** | 前端实时 SSE | ~0ms | 低延迟实时推送 |
| **Redis Stream** | 断线重连 / 历史回放 | ~1ms | 持久化 + 重连补漏 |
| **MySQL** | 长期存储 | ~500ms | 历史消息查询 |

`bufferTextLine()` 的批处理策略（`writer.go:356-407`）：

- **Hub**：每条 TEXT **立即**推送（`Hub.Publish`）
- **Redis**：累积到 **2KB** 或 **500ms** 后合并为一条写入（减少 Redis 写入频率）

### 2.3 RuntimeHub（内存 Pub/Sub）

文件：`backend/internal/stream/hub.go`

```go
// 全局单例
var Hub = &RuntimeHub{...}

// 发布：非阻塞，buffer 满时丢弃旧事件
func (h *RuntimeHub) Publish(key, data string)

// 订阅：返回带 1024 buffer 的 channel
func (h *RuntimeHub) Subscribe(key string) (<-chan HubEvent, uint64)

// 关闭：发送 Done sentinel，关闭所有 subscriber channel
func (h *RuntimeHub) Close(key string)
```

key 格式：`sessionID:messageID`（与 Redis Stream key 相同）。

### 2.4 推送端：`StreamService.ServeStream()`

文件：`backend/internal/service/impl/stream_service.go:27-145`

前端通过 `GET /api/tasks/:id/stream?session_id=&message_id=` 建立 SSE 连接：

```go
func (svc *StreamService) ServeStream(ctx, sessionID, messageID, writer, flusher) error {
    message := svc.messageDao.FindByMessageID(messageID)

    switch message.Status {
    case "streaming":  return svc.serveStreaming(...)
    case "failed":     svc.serveFailed(...)
    default:           svc.serveCompleted(...)
    }
}
```

**`serveStreaming()` 核心逻辑**（`stream_service.go:51-145`）：

```
Step 1: 写入已有的历史 Content（MySQL 中已有的部分）
         → 分 500 字符分片，FormatSSEWithMeta() 格式化后写 SSE

Step 2: 从 Redis Stream XREAD 补漏（上次断线到现在的 gap）
         → lastSeq 开始读取，逐条写 SSE

Step 3: 进入主循环，同时监听 4 个 channel：
         ├── Hub event channel  → 实时 SSE 事件（主要路径）
         ├── heartbeat ticker   → 每 15s 发心跳
         ├── stale timer        → 10s 检查流是否已结束
         └── ctx.Done()         → 客户端断开

当收到 HubEvent{Done: true}：
         → 写 data: {"type":"done"}，结束流
```

**三种消息状态的处理**：

| 状态 | 处理方式 |
|------|---------|
| `streaming` | 先回放历史 → Redis 补漏 → Hub 实时订阅 |
| `completed` | 分片回放全部 Content → 发 done |
| `failed` | 分片回放 Content → 发 error |

### 2.5 Agent 切换机制

当 Orchestrator 将任务分发给子 Agent 时，SSE 流中的 `agent_type` / `message_id` 会发生变化。`StreamWriter` 通过以下机制处理：

1. **`switchAgent()`**：检测到 agent_type 或 source_messageID 变化时
   - 刷出当前 buffer 到 MySQL
   - 将当前 sub-Message 状态更新为 `completed`
   - 创建新的 Message 记录（`status=streaming`）
   - 原始 Message 始终保持 `streaming` 直到整个 round 结束
2. **`shouldForwardTextWithoutPersist()`**：判断文本是否来自其他 session 的消息（跨 session 转发时不重复写 MySQL）
3. **`shouldSplitAfterForward()`**：转发文本后，下一条消息需要创建新的 sub-Message

---

## 三、Frontend（React）— SSE 事件消费者 + 渲染器

前端渲染链路涉及 6 层，自上而下为：

```
SSE EventSource
  → useChatStream（事件路由）
    → useMessageStore（rAF 批量刷写 + Zustand）
      → MessageList（虚拟列表 + 合成 streaming 消息）
        → MessageRenderer → MessageBubble → BlockRenderer
          → MarkdownRenderer / 各种卡片组件
```

### 3.1 SSE 连接层：`connectSSE()`

文件：`frontend/src/lib/sse.ts`

```typescript
export function connectSSE({ url, params, onEvent, ... }): AbortController {
    const es = new EventSource(fullUrl)  // 浏览器原生 EventSource API

    es.onmessage = (e) => {
        const event: StreamEvent = JSON.parse(e.data)
        onEvent(event)  // 回调给 useChatStream
    }
    // 开发环境绕过 Vite 代理（Vite 会 buffer SSE）
    // 5 分钟无事件视为 stale，自动关闭
}
```

**注意**：开发模式下直连 `http://localhost:8080` 绕过 Vite dev proxy，因为 Vite 会 buffer SSE 响应。

### 3.2 Hook 层：`useChatStream()`

文件：`frontend/src/hooks/use-chat-stream.ts`

这是前端 SSE 的**中枢调度器**，负责：

- **发送消息**：`sendMessage()` → POST `/api/tasks/:id/run` → 拿到 `message_id` → 建立 SSE 连接
- **事件路由**：根据 `event.type` 分发到不同的 store action

```typescript
switch (event.type) {
    case "text":
        store.streamAgentUpdate(sessionId, agentType, agentName, messageId)
        store.streamText(sessionId, text, messageId)
        break
    case "tool_call":         store.streamToolCall(sessionId, toolName); break
    case "tool_result":       store.streamToolResult(sessionId); break
    case "done":              store.streamDone(sessionId); abort(); break
    case "error":             store.streamError(sessionId, error); abort(); break
    case "runtime_executing": store.streamRuntimeEvent(...); break
    case "runtime_completed": store.streamRuntimeEvent(...); break
    case "runtime_text":      store.streamRuntimeText(...); break
    case "planning":          store.streamPlanEvent(...); break
    case "plan_review":       store.streamPlanReviewEvent(...); break
    case "coordination_message": store.streamCoordinationEvent(...); break
    case "coordination_done":    store.streamCoordinationDone(...); break
    case "ask_card_start":       store.streamAskCardStart(...); break
    case "ask_card_done":        store.streamAskCardDone(...); break
}
```

**断线重连**：`useEffect` 在 mount 时加载历史消息，如果发现 `status === 'streaming'` 的消息，自动重新建立 SSE 连接继续消费。

### 3.3 状态管理层：`useMessageStore`（Zustand）

文件：`frontend/src/stores/message-store.ts`

#### rAF 批量刷写机制

这是前端渲染性能的关键设计。核心问题：每个 SSE token（可能 10ms 一个）都触发 `Zustand.set()` → React re-render + scroll，开销巨大。

解决方案：token 先缓冲到 `_textBufs`，通过 `requestAnimationFrame` 每帧只 flush 一次。

```typescript
let _textBufs: Map<string, string[]> | null = null
let _flushRafId: number | null = null

function _scheduleFlush(set) {
    if (_flushRafId !== null) return  // 已有 pending flush
    _flushRafId = requestAnimationFrame(() => {
        _flushRafId = null
        // 将缓冲的所有 token 拼接后一次性更新到 streamingContent
        set(s => ({
            sessions: { ...s.sessions, [sid]: {
                ...session,
                streamingContent: session.streamingContent + pieces.join(''),
            }}
        }))
    })
}
```

#### 关键 Store Actions

| Action | 作用 |
|--------|------|
| `streamStart()` | 初始化流式状态：`status='streaming'`，清空 `streamingContent` 和 `runtimeBlocks` |
| `streamText()` | token 缓冲 → rAF 批量拼接到 `streamingContent` |
| `streamAgentUpdate()` | 检测 agent 切换，将当前内容固化为一条 ChatMessage，重置流式状态 |
| `streamToolCall()` | `status='tool_running'`，UI 展示工具调用状态 |
| `streamToolResult()` | `status='streaming'`，恢复流式输出 |
| `streamDone()` | 最终 flush，将 `streamingContent` 固化为 ChatMessage，`status='done'` |
| `streamError()` | 类似 `streamDone` 但标记 `status='error'` |
| `streamRuntimeEvent()` | 维护 `runtimeBlocks[]` 数组（运行时任务状态卡片） |
| `streamRuntimeText()` | 向匹配的 `runtime_status` block 追加实时文本 |
| `streamPlanEvent()` | 向 `runtimeBlocks` 插入/更新 plan block |
| `streamPlanReviewEvent()` | 向 `runtimeBlocks` 插入 plan_review block |
| `streamCoordinationEvent()` | 向 `runtimeBlocks` 追加 coordination 消息 |
| `streamCoordinationDone()` | 标记 coordination block 为已关闭 |
| `streamAskCardStart()` | 向 `runtimeBlocks` 插入 ask_agent block |
| `streamAskCardDone()` | 更新 ask_agent block 状态为 answered/failed |

#### 流式 Replay 去重

`streamText()` 中内嵌了 replay 去重逻辑（`message-store.ts:437-479`）：

当断线重连后，Backend 先回放历史 Content（可能包含之前已经接收的文本），前端通过 `streamingReplay` 记录的 `messageId + offset` 来跳过重复部分，只追加真正的新文本。

### 3.4 会话状态模型：`SessionChatState`

文件：`frontend/src/stores/session-store.ts`

每个会话在 Zustand 中维护一份独立的 `SessionChatState`：

```typescript
interface SessionChatState {
  messages: ChatMessage[]           // 已完成的历史消息（固化后的）
  streamingContent: string          // 当前正在流式写入的原始文本
  streamingReplay?: {               // 断线重连去重游标
    messageId: string
    offset: number                  // 已确认收到的字符偏移量
  }
  streamingAgentType?: AgentType    // 当前流式输出的 agent 类型
  streamingAgentName?: string       // 当前流式输出的 agent 名称
  streamingMessageId?: string       // 当前流式消息的 message_id
  status: ChatStatus                // 'idle' | 'loading' | 'streaming' | 'tool_running' | 'done' | 'error'
  runtimeBlocks: MessageBlock[]     // 流式过程中的结构化 block（plan、ask_card 等）
  activePlanReviewKey?: string      // 当前活跃的 plan review 标识
  toolName?: string                 // 正在调用的工具名
  activeStream: ActiveStream | null // 当前活跃的流信息
  hasMore: boolean                  // 是否有更多历史消息可加载
  isLoadingMore: boolean            // 是否正在加载更多
}
```

**核心状态转换**：

```
idle → loading      sendMessage() 发出 POST 请求
loading → streaming  streamStart() 收到 SSE 连接确认
streaming → tool_running  streamToolCall() 收到 tool_call 事件
tool_running → streaming  streamToolResult() 收到 tool_result 事件
streaming → done    streamDone() 收到 done 事件
streaming → error   streamError() 收到 error 事件 / 连接断开
```

### 3.5 消息合成层：`MessageList`

文件：`frontend/src/components/chat/MessageList.tsx`

这是流式渲染的关键合成点。它将「已完成消息」和「正在流式输出的内容」合并为统一的 `displayItems` 数组：

```typescript
const displayItems = useMemo<DisplayItem[]>(() => {
  // 1. 将 runtimeBlocks + streamingContent 合成 streaming 消息的 blocks
  const streamingBlocks = coalesceMessageBlocks([
    ...runtimeBlocks,
    ...(streamingContent ? reduceEventToBlocks(streamingContent) : []),
  ])

  // 2. 如果正在流式输出，追加一条虚拟的 "streaming" 消息
  const allMsgs = isStreaming && (streamingContent || runtimeBlocks.length > 0)
    ? [
        ...messages,                       // 已完成的历史消息
        {
          id: 'streaming',                 // 虚拟 ID 标识
          role: 'agent',
          content: streamingContent,        // 原始文本
          blocks: streamingBlocks,          // 解析后的结构化 blocks
          agentType: streamingAgentType,
          timestamp: Date.now(),
        },
      ]
    : messages

  // 3. 过滤空消息 + 插入时间分隔线
  const visibleMsgs = allMsgs.filter(shouldRenderMessage)
  // ... 插入 time-divider ...
}, [messages, isStreaming, streamingContent, runtimeBlocks])
```

**虚拟列表优化**：当消息数 > 50 时启用 `@tanstack/react-virtual` 虚拟滚动，避免大量消息时的 DOM 节点过多。通过 `estimateSize` 估算每条消息高度，`overscan: 5` 预渲染上下 5 条。

**自动滚到底部**：`useMessageScroll` hook 监听 `streamingContent` 变化，通过 rAF 节流滚到底部，确保流式输出时始终看到最新内容。用户主动向上滚动时停止自动滚动，出现"回到底部"按钮。

### 3.6 消息渲染层：`MessageRenderer` → `MessageBubble`

文件：`frontend/src/components/chat/MessageRenderer.tsx`、`MessageBubble.tsx`

#### MessageRenderer

负责将 `ChatMessage` 转为 `MessageBubble`，并计算消息属性：

```typescript
// 长消息判定：> 1600 字符 或 > 28 行
function isLongMessage(msg, isStreaming) { ... }

// 结构化消息：包含非 text 类型的 block
function isStructuredMessage(msg) {
  return msg.blocks?.some(block => block.type !== 'text')
}
```

#### MessageBubble

根据消息角色渲染不同的布局：

- **User**：右对齐，primary-soft 背景，管理员头像
- **Agent**：左对齐，Agent 头像 + 颜色标识条 + 卡片背景
- **System**：居中，灰色小字

Agent 气泡的关键特性：

```tsx
// 宽度自适应：纯文本 vs 结构化内容
const bubbleWidth = isStructured || isLong
  ? 'w-full max-w-[min(68vw,46rem)]'   // 结构化内容（plan、diff 等）更宽
  : 'max-w-[min(68vw,38rem)]'          // 纯文本较窄

// 左侧颜色条 — 不同 Agent 不同颜色
<div style={{ backgroundColor: AGENT_COLORS[agentType] }} />

// 流式光标 — 流式输出时显示闪烁的 ▌
{isStreaming && <span className="animate-pulse">▌</span>}

// 长消息折叠 — 超长消息预览 22rem 高度，点击放大 Dialog
{isLong && <Dialog>...</Dialog>}
```

### 3.7 Block 渲染层：`BlockRenderer`

文件：`frontend/src/components/chat/MessageBubble.tsx:31-139`

`BlockRenderer` 是 block → UI 组件的路由器：

```tsx
function BlockRenderer({ block, taskId, sessionId, ... }) {
  switch (block.type) {
    case 'text':          return <MarkdownRenderer content={block.content} />
    case 'html-render':   return <HtmlCard content={block.content} expanded={...} />
    case 'image':         return <ImageCard path={block.path} />
    case 'diff':          return <DiffCard snapshotId={block.snapshotId} />
    case 'plan':          return <PlanCard overview={block.overview} tasks={block.tasks} />
    case 'plan_review':   return <PlanReviewCard ... interactive={interactive} />
    case 'runtime_status':return <RuntimeStatus ... streamingText={block.streamingText} />
    case 'coordination':  return <CoordChannel messages={...} />
    case 'ask_agent':     return <AskAgentCard ... status={block.status} />
    case 'task_failure':  return <TaskFailureCard ... />
    case 'final_summary': return <FinalSummaryCard ... />
    case 'tool_call':     return <ToolCard name={block.name} input={block.input} />
    case 'tool_result':   return <ToolCard output={block.output} />
    // ...
  }
}
```

每个卡片组件的具体职责：

| 组件 | 文件 | 职责 |
|------|------|------|
| `MarkdownRenderer` | `markdown/MarkdownRenderer.tsx` | ReactMarkdown + remarkGfm 渲染，自动识别树形结构并包裹为代码块 |
| `CodeBlock` | `markdown/CodeBlock.tsx` | Shiki 代码高亮（tokyo-night 主题），异步渲染 + 行号显示 |
| `PlanCard` | `cards/PlanCard.tsx` | 任务拆解列表，显示各子任务 agent + 标题 + 状态 |
| `PlanReviewCard` | `cards/PlanReviewCard.tsx` | 规划评审卡片，支持 approve/discuss 交互，Diff 预览 |
| `RuntimeStatus` | `cards/RuntimeStatus.tsx` | 子任务执行状态指示器，含实时文本流 `streamingText` |
| `CoordChannel` | `cards/CoordChannel.tsx` | 多 Agent 协商面板，显示消息列表 + 结论 |
| `AskAgentCard` | `chat/AskAgentCard.tsx` | Agent 间通信卡片，支持折叠/展开 |
| `FinalSummaryCard` | `cards/FinalSummaryCard.tsx` | 执行总结卡片（成功/失败/部分完成） |
| `TaskFailureCard` | `cards/TaskFailureCard.tsx` | 任务失败卡片，显示原因和类型（超时/错误） |
| `DiffCard` | `cards/DiffCard.tsx` | Diff 查看器，多文件 tab + CodeMirror 编辑 |
| `HtmlCard` | `cards/HtmlCard.tsx` | HTML 内容预览（沙箱 iframe） |
| `ImageCard` | `cards/ImageCard.tsx` | 图片展示 |
| `PreviewCard` | `cards/PreviewCard.tsx` | 外部 URL 预览 |
| `ToolCard` | `cards/ToolCard.tsx` | 工具调用/结果展示 |

### 3.8 Markdown 渲染细节

文件：`frontend/src/components/markdown/MarkdownRenderer.tsx`、`CodeBlock.tsx`

`MarkdownRenderer` 基于 `react-markdown` + `remark-gfm`（GitHub Flavored Markdown），自定义了以下组件：

- **代码块**：有语言标注时使用 Shiki 异步高亮（tokyo-night 主题），无标注但含换行时显示行号的纯文本代码块，单行时用 inline `<code>` 样式
- **树形结构自动识别**：`fenceTreeBlocks()` 预处理函数检测 `│├└┬┼─` 等 ASCII 树形字符，自动包裹为 ` ```text ``` ` 代码块
- **表格**：自定义 `table`/`th`/`td` 组件，带边框和固定布局
- **文本样式**：`prose prose-invert`，`whitespace-pre-wrap` 保留换行

### 3.9 文本 → Block 解析：`block-reducer.ts`

文件：`frontend/src/lib/block-reducer.ts`

`streamingContent`（原始文本）通过 `reduceEventToBlocks()` 被解析为 `MessageBlock[]`：

```
原始文本流
  ├── 普通文本                   → { type: 'text', content: '...' }
  ├── ```aka_yhy ... ``` 围栏块  → 解析为 html-render / image / diff 等
  └── 内联 type: + json: 标记行  → 解析为 plan / plan_review / runtime_status / ask_agent 等
```

`coalesceMessageBlocks()` 负责合并相邻的同类 block：

- 多个连续 text block 合并为一个
- `runtime_status` 同 task_id 的更新合并
- `plan` / `plan_review` / `ask_agent` / `coordination` 同 ID 的去重合并

### 3.10 流式过程中的 Block 增量更新

前端有两种并行的 block 来源，它们在 `MessageList` 中合并：

1. **`runtimeBlocks[]`**：由 store actions 直接维护的结构化 block（`streamRuntimeEvent`、`streamPlanReviewEvent`、`streamAskCardStart` 等），这些是独立于文本流的 SSE 事件（如 `runtime_executing`、`plan_review`、`ask_card_start`）触发的
2. **`reduceEventToBlocks(streamingContent)`**：从流式文本中解析出的 block（如文本中内嵌的 `type: plan\njson: {...}` 标记）

合并公式：
```typescript
const streamingBlocks = coalesceMessageBlocks([
  ...runtimeBlocks,                                            // 先放事件驱动的 block
  ...(streamingContent ? reduceEventToBlocks(streamingContent) : []),  // 再放文本解析的 block
])
```

`coalesceMessageBlocks` 会自动去重：如果 `runtimeBlocks` 已有某个 `plan` block，文本解析出的同名 block 会合并（更新字段）而非重复添加。

### 3.11 前端流式渲染完整时序

```
SSE token 到达 (每 10-50ms)
    │
    ▼
connectSSE → onEvent({ type: "text", content: { text: "Hello" } })
    │
    ▼
useChatStream: store.streamText(sessionId, "Hello", messageId)
    │
    ▼
message-store: _ensureBuf(sessionId).push("Hello")
               _scheduleFlush() → requestAnimationFrame
    │  ← 同一帧内可能推入多个 token，只触发一次 rAF
    ▼
rAF callback: Zustand.set(state => ({
    streamingContent: state.streamingContent + pieces.join('')
}))
    │  ← 一次 set() 触发一次 React re-render
    ▼
MessageList useMemo 重算:
  streamingBlocks = coalesceMessageBlocks([
    ...runtimeBlocks,
    ...reduceEventToBlocks(streamingContent)    ← 每帧全量重新解析
  ])
  displayItems = [...messages, { id: 'streaming', blocks: streamingBlocks }]
    │
    ▼
React re-render:
  MessageBubble(variant="agent", blocks=[...], isStreaming=true)
    ├── BlockRenderer(block) → 各卡片组件
    │     └── type: 'text' → MarkdownRenderer(content)
    │           └── ReactMarkdown → <p>Hello World...</p>
    └── <span className="animate-pulse">▌</span>     ← 流式光标
    │
    ▼
useMessageScroll: scheduleScrollToBottom() → rAF 滚到底部
```

---

## 四、三端协作时序（端到端）

```
用户输入 "Hello"
    │
    ▼
[Frontend] sendMessage() → POST /api/tasks/:id/run
    │                                          │
    │                                          ▼
    │                               [Backend] 创建 Message (status=streaming)
    │                                          │ POST /v1/agent/stream → Agentend
    │                                          │
    │                                          ▼
    │                               [Agentend] Claude CLI subprocess
    │                               stdout 逐行 → _parse_stream_line() → StreamEvent
    │                               yield {"event":"text","data":"..."}
    │                                          │
    │                               [Backend] StreamWriter.Run() 消费 SSE
    │                               ├── appendText() → 内存 buffer
    │                               ├── bufferTextLine() → Hub.Publish() [热路径]
    │                               └── flushLoop() → MySQL 批量刷写 [500ms]
    │
    ▼
[Frontend] GET /api/tasks/:id/stream (EventSource)
    │
    │  [Backend] StreamService.serveStreaming()
    │  ├── Redis XREAD 补漏
    │  └── Hub.Subscribe() channel → 写 SSE → Flush
    │
    ▼
[Frontend] es.onmessage → JSON.parse → StreamEvent
    │
    ▼
[useChatStream] onEvent → switch(event.type)
    │  "text" → store.streamText()
    ▼
[message-store] _ensureBuf(sid).push(text) + _scheduleFlush()
    │  rAF 合并 token
    ▼
[Zustand set] streamingContent += "all buffered tokens"
    │
    ▼
[React re-render] reduceEventToBlocks(streamingContent) → MessageBlock[]
    │
    ▼
[MessageBubble] 根据 block.type 渲染对应组件
```

---

## 五、关键设计决策

### 5.1 为什么不直连 Agentend？

前端不直连 Agentend，而是通过 Backend 中转，原因：

1. **安全性**：Agentend 没有认证，不应暴露给前端
2. **持久化**：Backend 负责将消息写入 MySQL，断线后可恢复
3. **多路复用**：Orchestrator 场景下，一个前端连接对应多个 Agent，Backend 负责合并
4. **断线重连**：Backend 通过 Redis Stream 支持从断点续传

### 5.2 为什么用 Hub + Redis 双通道？

1. **Hub（内存）**：延迟极低（Go channel 直通），适合实时推送
2. **Redis Stream**：支持持久化、断线重连、多实例部署时跨进程通信
3. **MySQL**：长期存储，前端首次加载时读取历史消息

### 5.3 为什么前端用 rAF 批量刷写？

- SSE token 可能每 10-50ms 到达一个
- 每次都触发 `Zustand.set()` → React re-render → Markdown 解析 → scroll，开销极大
- rAF 将同一帧内到达的所有 token 合并为一次更新，将渲染频率降到 60fps
- 用户体验不受影响（人眼无法区分 16ms 内的文字增量）

### 5.4 为什么 Backend 要批量刷 MySQL？

- 如果每个 token 都 `UPDATE messages SET content = ...`，MySQL 写入压力极大
- StreamWriter 的 `flushLoop` 每 500ms 或 buffer 达到 2KB 时才刷一次
- 前端不会感知延迟，因为它从 Hub/Redis 读取，不依赖 MySQL

---

## 六、相关文件索引

### Agentend

| 文件 | 职责 |
|------|------|
| `agentend/src/api/v1/agent.py` | SSE 端点，`_execute_stream()` 生成器 |
| `agentend/src/adapters/base.py` | 适配器抽象基类，定义 `stream_chat()` 接口 |
| `agentend/src/adapters/claude.py` | Claude CLI 适配器，子进程 + 逐行解析 |
| `agentend/src/adapters/opencode.py` | OpenCode CLI 适配器 |
| `agentend/src/adapters/codex.py` | Codex CLI 适配器 |
| `agentend/src/adapters/orchestrator.py` | Orchestrator 多 Agent 适配器 |
| `agentend/src/schemas/events.py` | StreamEvent 模型 |
| `agentend/src/generated/events.py` | 契约生成的 EventType 枚举 |

### Backend

| 文件 | 职责 |
|------|------|
| `backend/internal/stream/writer.go` | StreamWriter — 消费 agentend SSE，双写 Hub/Redis/MySQL |
| `backend/internal/stream/hub.go` | RuntimeHub — 内存 Pub/Sub（Go channel） |
| `backend/internal/service/impl/stream_service.go` | StreamService — 向前端推送 SSE |
| `backend/internal/handler/stream.go` | SSE HTTP handler |
| `backend/internal/generated/events.go` | 契约生成的 StreamEvent/EventType |

### Frontend

| 文件 | 职责 |
|------|------|
| `frontend/src/lib/sse.ts` | `connectSSE()` — EventSource 封装 |
| `frontend/src/hooks/use-chat-stream.ts` | `useChatStream()` — SSE 事件路由中枢 |
| `frontend/src/stores/message-store.ts` | Zustand store — rAF 批量刷写 + 状态管理 |
| `frontend/src/stores/session-store.ts` | 会话级状态（streamingContent, runtimeBlocks 等） |
| `frontend/src/lib/block-reducer.ts` | 文本 → MessageBlock[] 解析器 |
| `frontend/src/lib/block-types.ts` | MessageBlock 类型定义 |
| `frontend/src/components/chat/ChatArea.tsx` | 聊天区域组件，调用 useChatStream |
| `frontend/src/components/chat/MessageBubble.tsx` | 消息气泡，根据 block.type 渲染 |
| `frontend/src/generated/events.ts` | 契约生成的 StreamEvent/EventType TypeScript 类型 |
