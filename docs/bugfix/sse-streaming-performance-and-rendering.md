# SSE 流式输出性能优化 + 渲染修复

## Context

三端 SSE 流从 agentend → backend → frontend 的热路径（Hub 推送）已经是即时推送，**用户感知的首 token / token 推送延迟不主要卡在后端 flush 间隔**。性能问题集中在**前端渲染管线**：

1. 每次流式 token 都重新解析全部 `streamingContent`（O(n²)）
2. 每次都全量跑 ReactMarkdown（含 remarkGfm AST 解析）
3. 已完成的 block/消息也会跟着重渲染
4. `syncComposedState()` 在非流式场景（sendMessage/streamStart 等）级联调用 2-3 次

## 审查结论（2026-06-04）

本文原先偏重前端渲染性能，但复查三端 SSE 链路后确认：在做渲染优化前，必须先处理几个**正确性 / 状态一致性**问题，否则性能优化会掩盖重复内容、失败状态被覆盖、断线重连错乱等问题。

### 必须先修的正确性问题

1. **EventSource 原生重连会重复拼接历史内容**
   - 前端 `connectSSE({ reconnect: true })` 依赖浏览器自动重连。
   - 后端 `serveStreaming()` 每次新连接都会先把 MySQL 当前 `msg.Content` 作为 history text events 发送。
   - 后端 Redis replay 从 `msg.LastSeq` 继续读，但这个值是 MySQL 已 flush 位置，不是当前 EventSource 客户端已消费位置。
   - `Hub.Subscribe()` 返回的 `currentSeq` 当前未使用，代码注释里的 "events between last_seq and hub's currentSeq" 语义没有真正落地。
   - 前端当前只 append TEXT，没有按 `message_id` + offset/seq 去重；断线重连后会把 MySQL history 和 Redis gap 都再拼一次。
   - 解决方向：
     - 后端为 SSE 发送标准 `id:`，并支持 `Last-Event-ID` / query `last_seq` 精确续接；或
     - 前端在重连前清空当前 streaming buffer，并用服务端 replay 作为权威状态；或
     - 前端按 `message_id` + 已接收字符长度对 history replay 做 prefix 去重。

2. **后端 scanner error 可能先标 failed 又被 `StreamWriter.Run()` 收尾覆盖成 completed**
   - `task.go` 中 `scanner.Err()` 分支调用 `sw.Fail()`。
   - 但 `StreamWriter.Run()` 的 scanFunc 返回后仍继续执行最终 flush，并在 `sawError == false` 时 `updateMessageStatus(..., "completed")`。
   - 同类状态问题还存在于正常 `ERROR` event：`StreamWriter` 会把 Message 标 failed，但 `task.go` scan 正常结束后仍无条件把 Session 标 completed。
   - 解决方向：`Run()` 需要从 scanFunc 获得错误结果，或 `StreamWriter` 内部记录 terminal outcome；Message 和 Session 都必须使用同一个 outcome 收尾。

3. **后端 `bufio.Scanner` 单行上限与 agentend 子进程行上限不一致**
   - 后端 scanner buffer 上限是 1MB。
   - agentend 各 CLI adapter subprocess `limit` 是 10MB，且某些 tool/result JSON 可能形成长 `data:` 单行。
   - 解决方向：用 `bufio.Reader.ReadString('\n')` / `ReadBytes('\n')` 解析 SSE 行，或把后端 scanner 上限提高到与 agentend 一致，并保留错误测试。

4. **agentend `_execute_stream` 无论异常都把 session 标 completed**
   - `_execute_stream()` 的 `finally` 固定 `session_mgr.update_state(..., COMPLETED)`。
   - 这会让 agentend 内部 session 状态掩盖 adapter 异常 / ERROR event；后端 message 可能是 failed，但 agentend session 是 completed。
   - 解决方向：捕获异常时标 error/interrupted；收到 `EventType.ERROR` 后标 error；正常完整结束才标 completed。

5. **前端 `connectToStream` callback 依赖漏 `agentType`**
   - `useChatStream()` 中 `connectToStream` 内部使用 `agentType` 调 `streamStart()`，但依赖数组未包含 `agentType`。
   - 切换 agent 类型后自动重连可能用旧 agent type 初始化 streaming state。

6. **后端 heartbeat 事件未进入契约**
   - backend 会发送 `{"type":"heartbeat"}`。
   - `contracts/schemas/event-types.yaml` 未声明 `heartbeat`。
   - 当前前端 default ignore 不会崩，但三端契约不一致。
   - 注意：不能无条件改成 SSE 注释心跳 `: heartbeat\n\n`。EventSource comment 不触发 `onmessage`，前端 `staleTimeoutMs` 只在 `onmessage` 更新 `lastEventTime`，改成 comment 后长时间无业务事件时仍会被前端误判超时。
   - 解决方向：
     - **方案 A（推荐）**：把 `heartbeat` 加入 `contracts/schemas/event-types.yaml` 并运行 `make generate`，前端显式 case ignore 但更新活跃时间。
     - **方案 B**：改成 SSE comment，同时删除或重写前端 stale watchdog，让它不依赖 message event。

7. **后端历史回放按 byte 切分可能破坏 UTF-8**
   - `splitContent(text, 500)` 用 `len(text)` 和 `text[:end]` 按 byte 切。
   - 如果 500 byte 边界落在中文 / emoji 等多字节字符中间，历史回放、completed replay、failed replay 可能产生替换字符或内容损坏。
   - 解决方向：按 rune 切，或用 `strings.Builder` / `utf8.DecodeRuneInString` 保证 chunk 边界是合法 UTF-8。

8. **前端 error 会清空当前已显示的部分内容**
   - `streamError()` 直接清空 `streamingContent` 和 `runtimeBlocks`。
   - 后端可能已经持久化部分内容，刷新后能恢复，但当前视图会把失败前已经流出来的内容瞬间抹掉。
   - 解决方向：error 收尾时把已有 streaming 内容转成一条 failed agent message，或保留当前 streaming bubble 并显示错误状态。

9. **SSE 字段名存在轻微契约漂移**
   - 前端 `tool_call` 分支读取 `content.name`。
   - agentend adapters 发送的是 `content.tool`。
   - 当前表现通常是工具名显示 `unknown`，不一定阻断主链路，但属于 SSE 事件字段不一致，应在 Phase 0 或同批小修中统一。

### 实施顺序修正

新增 **Phase 0** 先修正确性问题；Phase 1-4 再做前端性能优化；Phase 6 作为防御性增强保留但不应排在正确性问题前。

### Phase 0 实施记录（2026-06-04）

Phase 0 已按保守修复策略落地，目标是先消除 SSE 正确性 / 状态一致性问题，不在同一批里引入完整 `id:` / `Last-Event-ID` 协议重构。

已完成：

- **后端状态 outcome 统一**：`StreamWriter.Run()` 返回 terminal outcome，`task.go` 按 outcome 更新 Session，reader error / `ERROR` event 不再被最终 completed 覆盖。
- **后端长行读取加固**：`task.go` 从 `bufio.Scanner` 改为 `bufio.Reader.ReadString('\n')`，避免 1MB scanner token 上限；单行超过 10MB 时按 reader error 失败收尾。
- **后端 UTF-8 replay 修复**：`splitContent()` 改为按 rune 切分，history / completed / failed replay 不再按 byte 边界切坏中文或 emoji。
- **agentend outcome 修复**：`_execute_stream()` 用本地 outcome 收尾，adapter `ERROR` 标 `ERROR`，客户端取消标 `INTERRUPTED`，正常结束才标 `COMPLETED`。
- **heartbeat 入契约**：`heartbeat` 加入 `contracts/schemas/event-types.yaml`，并重新生成三端类型；前端显式处理 `EventTypeValues.Heartbeat`。
- **前端 replay 去重**：TEXT event 按 `message_id` 做前缀 replay 去重，避免 EventSource 原生重连后重复拼接历史内容；同时保留断线前本地已显示、可能尚未持久化的 token。
- **前端 error 内容保留**：`streamError()` 会把已有 `streamingContent + runtimeBlocks` 收敛成 failed agent message，不再清空失败前已显示内容。
- **tool_call 字段兼容**：前端兼容 `content.tool || content.name`，避免 adapters 发 `tool` 时 UI 显示 `unknown`。

验证结果：

- `go test ./...` 通过
- `uv run pytest` 通过
- `uv run ruff check src/api/v1/agent.py` 通过
- `uv run python -m py_compile src/api/v1/agent.py src/adapters/claude.py src/adapters/opencode.py src/adapters/codex.py src/clients/backend_client.py` 通过
- `pnpm exec vitest run src/stores/__tests__/chat.test.ts` 通过
- `git diff --check` 通过

未纳入本批：

- 标准 SSE `id:` / `Last-Event-ID` 精确续接仍未实现。本批采用前端 `message_id` 前缀去重作为保守修复。
- Phase 1-4 前端渲染性能优化仍待后续实施。
- `pnpm exec tsc -b` 当前仍受既有无关类型问题阻塞（`ACTIVE_STATUSES.has(ChatStatus)`、`HistorySearch` 旧 block type、`ContactsPage` 未使用变量等），不是本批 SSE 改动引入。

---

## 修改计划

### Phase 0: SSE 正确性与状态一致性修复（BLOCKER）

> **注意（2026-06-06）**: 后端代码已重构为 Controller + Service 分层。`handler/task.go` → `controller/impl/task_controller.go` + `service/impl/task_service.go`；`handler/stream.go` → `controller/impl/stream_controller.go` + `service/impl/stream_service.go`；`handler/message.go` → `controller/impl/message_controller.go` + `service/impl/message_service.go`。

#### 文件: `frontend/src/lib/sse.ts` / `frontend/src/hooks/use-chat-stream.ts`
- 明确重连策略：不能让 EventSource 自动重连后无条件 append replay 内容。
- 推荐实现二选一：
  - **方案 A（协议正确）**：后端输出 `id: <redis-seq>`，前端记录 `Last-Event-ID`，重连只接收缺口。
  - **方案 B（前端兜底）**：重连触发后清空当前 streaming buffer，以后端 history replay 重建当前消息。
- 若采用前缀去重，必须以 `message_id` 为边界，不能跨子 Agent / 子 Message 去重。
- `connectToStream` 的依赖数组必须补 `agentType`；否则切换 agent 后重连会用旧 agent type 初始化 streaming state。
- 若采用方案 B，需要区分"新 stream start"和"同一 message reconnect replay"：
  - 新 stream：清空 `streamingContent` / `runtimeBlocks` / `streamingMessageId`
  - reconnect replay：清空当前 streaming buffer 后等待服务端 replay 重建
  - 普通 `streamStart()` 当前不会清空 `streamingContent`，不能假设已经完成 reset
- `streamError()` 不应直接丢弃已显示部分内容。应把现有 `streamingContent + runtimeBlocks` 收敛成 failed message，或保留在当前 bubble 中并展示错误状态。
- `tool_call` 字段统一：前端兼容读取 `content.tool || content.name`，或 agentend/backend 统一改成 `name` 并更新契约说明。

#### 文件: `backend/internal/handler/stream.go`
- `serveStreaming()` 的 history replay 需要与前端去重策略配套。
- 如实现标准 SSE `id:`，Redis replay / Hub event / completed replay 都应携带一致的 seq 语义。
- `Hub.Subscribe()` 当前返回 `currentSeq` 但 handler 未使用。若保留三阶段握手，应真正用 currentSeq 定义 Redis gap replay 边界；否则删除误导性注释并采用更明确的 replay 策略。
- 如果继续发送 `heartbeat` event，需先改 `contracts/schemas/event-types.yaml` 并运行 `make generate`。
- 如果改成 SSE comment heartbeat，必须同步修改 `frontend/src/lib/sse.ts` 的 stale watchdog；否则 comment 不会刷新 `lastEventTime`。
- `splitContent()` 必须按 UTF-8 rune 边界切分，不得按 byte 切分。

#### 文件: `backend/internal/handler/task.go`
- 替换或加固 `bufio.Scanner`：
  - 避免 1MB 单行上限导致长 SSE event 失败。
  - scanner/reader 错误必须传回 `StreamWriter.Run()`，避免最终状态被覆盖成 completed。
- scan 正常结束后不应无条件把 Session 标 completed。Session 状态必须跟 `StreamWriter` terminal outcome 一致：
  - DONE / 无 ERROR：completed
  - ERROR event / reader error / panic：failed
  - plan_review awaiting：awaiting_review，不应被后续 completed 覆盖

#### 文件: `backend/internal/stream/writer.go`
- `StreamWriter` 增加 terminal outcome 标记，或让 `Run()` 接收 scan 错误并返回 outcome。
- `Fail()` / ERROR event 后 finalizer 不得再把当前 / original message 更新为 completed。
- outcome 需要暴露给 `task.go`，确保 Message 和 Session 状态一致。
- `PublishErrorAndFail()` 与 `Fail()` 都应确保 hub 关闭、Redis TTL、registry cleanup 的行为一致且幂等。

#### 文件: `agentend/src/api/v1/agent.py`
- `_execute_stream()` 不应在 `finally` 无条件标 completed。
- 正常结束：`COMPLETED`
- adapter 抛异常 / ERROR event：`ERROR`
- 客户端取消 / interrupt：`INTERRUPTED`（若状态模型支持）
- 实现时用本地 `outcome` 变量统一收尾；不要在 `async for` 内先标 ERROR 再让 `finally` 标 COMPLETED。当前状态机不允许 `ERROR -> COMPLETED`。

**验证**:
- 断线重连：流式到一半断开再恢复，最终文本不重复、不丢字。
- scanner 错误：构造超长 `data:` 行，Message / Session 最终为 failed，不被 completed 覆盖。
- agentend adapter 抛异常或 yield ERROR：agentend session、backend message、backend session 三者状态一致。
- heartbeat：前端无 unknown contract warning；长时间只有 heartbeat 时不会触发 stale timeout。
- UTF-8 replay：构造 500 byte 附近的中文内容，completed / failed / streaming history replay 不乱码。
- error 收尾：失败前已经显示的文本仍留在界面中，并带失败状态。

---

### Phase 1: 增量 Block 解析（HIGH IMPACT）

**问题**: `MessageList.tsx:78-82` 的 `useMemo` 每帧调用 `reduceEventToBlocks(streamingContent)` 从头解析整个字符串（`block-reducer.ts:10-41` O(n) 全文扫描）。10KB 响应 × 60fps = 每秒解析 600KB。

**方案**: 在 store 层缓存已解析并合并的 blocks，只解析新增的 delta。

#### 文件: `frontend/src/stores/session-store.ts`
- `SessionChatState` 新增两个字段：
  ```ts
  streamingBlocks: MessageBlock[]      // 已解析且已与 runtimeBlocks 合并的完整 blocks
  streamingParsedLength: number         // 已解析到的字符位置
  ```
- `initialSessionState` 初始化 `streamingBlocks: []`, `streamingParsedLength: 0`

#### 文件: `frontend/src/lib/block-reducer.ts`
- 新增 `reduceEventToBlocksDelta(fullText, prevParsedLength, prevBlocks)` 函数
  - 取 delta = `fullText.slice(prevParsedLength)`
  - 只解析 delta，保留 prevBlocks 中确定已完成的部分
  - 处理跨 chunk 边界的 `aka_yhy` fence：**不要使用固定 200 字符回溯作为唯一机制**。结构化 block 可能远超 200 字，必须记录最后一个未闭合 fence 的起点或维护增量 parser 状态，从未闭合位置开始重解析
  - **引用稳定性**：对已完成（不在回溯区间内的）block 返回原对象引用（`prevBlocks[i] === nextBlocks[i]`），确保 Phase 2 的 React.memo 有效
  - **block ID 副作用**：`_blockIdCounter`（`block-reducer.ts:6-8`）是模块级可变计数器。回溯重解析时会产生新 ID，但因为是替换尾部 blocks，不影响已完成 blocks 的 ID 稳定性。这是可接受的 — 只有尾部（正在增长的）block 的 ID 会变化
  - 返回 `{ blocks, parsedLength }`
- 新增或改造 `coalesceMessageBlocksStable(prevBlocks, nextRawBlocks)`：
  - 当前 `coalesceMessageBlocks()` 会对每个 block 执行 `{ ...block }` 克隆，直接破坏引用稳定性
  - Phase 2 依赖 `prev.block === next.block`，因此合并函数必须对未变化 block 返回原对象
  - runtime block merge（plan / runtime_status / coordination / ask_agent）只能替换实际变化的 block

#### 文件: `frontend/src/stores/message-store.ts`
- `_scheduleFlush` 的 rAF 回调中，在拼接新 text 到 `streamingContent` 后：
  1. 调用 `reduceEventToBlocksDelta` 增量更新 `streamingBlocks`（仅文本 blocks）
  2. 再执行稳定引用版本的 coalesce 合并 `runtimeBlocks + newStreamingBlocks`
  - 这样 `streamingBlocks` 始终是合并后的最终结果，`runtimeBlocks` 变化时也需要重新合并
- `streamDone` / `streamError` / `streamAgentUpdate` 等清除 streaming 状态时，同时重置 `streamingBlocks: []`, `streamingParsedLength: 0`
- `streamStart` 同样重置这两个字段

#### 文件: `frontend/src/components/chat/MessageList.tsx`
- 将 `displayItems` useMemo 中：
  ```tsx
  // Before: 每帧全量解析
  const streamingBlocks = coalesceMessageBlocks([
    ...runtimeBlocks,
    ...(streamingContent ? reduceEventToBlocks(streamingContent) : []),
  ])
  // After: 直接使用 store 层缓存的已合并 blocks
  // streamingBlocks 已在 store flush 时完成 coalesce，无需再处理
  ```
- 依赖数组增加 `streamingBlocks`，移除 `streamingContent` 和 `runtimeBlocks`
- 新增 `streamingBlocks` prop 从 ChatArea 传入
- 移除对 `reduceEventToBlocks` / `coalesceMessageBlocks` 的直接 import（如果不再使用）

#### 文件: `frontend/src/components/chat/ChatArea.tsx`
- 从 store 读取 `streamingBlocks` 传给 `MessageList`

**验证**:
- 增加断言：增量解析 + stable coalesce 后 `prevBlocks[i] === nextBlocks[i]`（i < 已完成区域长度）引用稳定性
- 增加用例：`aka_yhy` fence opening 和 closing 分布在多个 chunk，且 block 内容超过 200 字，最终仍解析为结构化 block
- `console.time` 对比增量 vs 全量解析耗时

---

### Phase 2: React.memo 隔离重渲染（HIGH IMPACT，依赖 Phase 1 引用稳定性）

**问题**: `BlockRenderer`（`MessageBubble.tsx:31`）、`MarkdownRenderer`（`MarkdownRenderer.tsx:128`）都是普通函数组件，每次流式更新所有 block 都重渲染。

**前提**: Phase 1 的 `reduceEventToBlocksDelta` 和 stable coalesce 都必须对已完成 block 返回原对象引用。如果任一环节克隆 block，Phase 2 的 React.memo 形同虚设。

#### 文件: `frontend/src/components/markdown/MarkdownRenderer.tsx`
- 用 `React.memo` 包裹 `MarkdownRenderer`，props 浅比较 `content`
- 当 content 不变时完全跳过 `fenceTreeBlocks` + ReactMarkdown 解析

#### 文件: `frontend/src/components/chat/MessageBubble.tsx`
- `BlockRenderer` 用 `React.memo` 包裹，自定义比较器：
  ```ts
  React.memo(BlockRenderer, (prev, next) =>
    prev.block === next.block &&
    prev.taskId === next.taskId &&
    prev.sessionId === next.sessionId &&
    prev.interactive === next.interactive &&
    prev.expandedPreview === next.expandedPreview &&
    prev.agentSessionLookup === next.agentSessionLookup
  )
  ```
- 注意 comparator 必须覆盖所有会影响渲染的 props；尤其 `agentSessionLookup` 会影响 AskAgentCard 的 avatar / session 信息，`expandedPreview` 会影响 preview card 展开渲染。漏掉这些字段会造成 memo 后 UI stale。

**验证**:
- React DevTools Profiler 确认流式输出时非活跃 block 0 次重渲染

---

### Phase 3: 流式阶段跳过 Markdown 解析（MEDIUM IMPACT）

**问题**: 即使有 React.memo，正在流式输出的最后一个 text block 每帧都触发 ReactMarkdown 全量解析。

**关键定位逻辑**: 流式输出时可能有多个 text block（runtimeBlocks 里的 + streaming 解析出的），**只有最后一个 text block**（即尚未遇到闭合 `aka_yhy` fence 的增长中 block）才应该跳过 Markdown。已完成的 block 必须始终完整渲染。

#### 文件: `frontend/src/components/markdown/MarkdownRenderer.tsx`
- 新增 `isStreaming?: boolean` prop
- 当 `isStreaming === true` 时，用轻量渲染替代：
  ```tsx
  <div className="prose prose-invert ... whitespace-pre-wrap">
    {content}
  </div>
  ```
- 不跑 `fenceTreeBlocks`、不跑 ReactMarkdown、不跑 shiki 高亮
- **未闭合 fence 处理**: 如果纯文本内容包含 ` ``` ` fence 开头但未闭合，会原样显示。这是可接受的短暂中间态，流结束时会自动触发完整渲染
- 流结束时 `isStreaming` 变 false，自动触发一次完整 Markdown 渲染（"格式化弹出"效果，ChatGPT 同款体验）

#### 文件: `frontend/src/components/chat/MessageBubble.tsx`
- `BlockRenderer` 中 text block 渲染时传入 `isStreaming`，仅最后一个 block 为 true：
  ```tsx
  case 'text': {
    const isLastBlock = block === blocks[blocks.length - 1]
    return (
      <MarkdownRenderer
        content={block.content}
        isStreaming={isStreaming && isLastBlock}
      />
    )
  }
  ```
- `AgentMessageContent` 需将 `blocks` 数组和 `isStreaming` prop 透传给 `BlockRenderer`
- 注意：最后一个 block 不一定是 text。若最后一个 block 是 runtime / ask_agent / plan_review，不能把前面的已完成 text 误判为 streaming text。

**验证**:
- 肉眼确认流式输出为纯文本，完成后"格式化弹出"效果正常
- 确认已完成 block 不受影响，始终完整渲染

---

### Phase 4: 优化 Store 级联同步（LOW IMPACT）

**问题修正**: 审查后确认 — 流式热路径上 `_scheduleFlush` 只更新 message-store → session-store（通过 `useSessionStore.setState`），每帧只触发 **1 次** `syncComposedState()`。只有 `sendMessage` / `streamStart` 等同时修改多个 store 的操作才出现 2-3 次同步。

Impact 从 MEDIUM 下调为 **LOW**。但 rAF batching 方案本身仍值得做，防止非流式场景多余同步，且实现成本低。

#### 文件: `frontend/src/stores/chat.ts`
- 将三个 subscribe 合并为 rAF 批量同步：
  ```ts
  let syncRafId: number | null = null
  function scheduleSync() {
    if (syncRafId !== null) return
    syncRafId = requestAnimationFrame(() => {
      syncRafId = null
      useChatStore.setState(syncComposedState())
    })
  }
  useNavigationStore.subscribe(scheduleSync)
  useSessionStore.subscribe(scheduleSync)
  useMessageStore.subscribe(scheduleSync)
  ```
- 确保每帧最多一次 `syncComposedState()`

---

### Phase 5: SKIP — 后端 Flush 间隔优化

审查结论：`flushInterval`/`textBatchAge` 只影响 Redis/MySQL 持久化时效性，**不影响用户感知**（Hub 热路径已经即时推送）。收益可忽略，不做。

---

### Phase 6: Hub 终端事件保护（LOW IMPACT，防御性编程）

#### 文件: `backend/internal/stream/hub.go`
- `Publish` 方法中检测终端事件，确保不被 drop
- **检测方式**: 用轻量 JSON 解析替代 `strings.Contains`，避免 text 内容误判：
  ```go
  var peek struct{ Type string `json:"type"` }
  payload := strings.TrimPrefix(data, "data: ")
  isTerminal := json.Unmarshal([]byte(payload), &peek) == nil &&
      (peek.Type == "done" || peek.Type == "error")
  ```
- **drain 上限**: 加 cap 限制防止极端情况卡顿：
  ```go
  for sub := range s.subscribers {
      select {
      case sub.ch <- evt:
      default:
          if isTerminal {
              for i := 0; i < cap(sub.ch) && len(sub.ch) > 0; i++ {
                  <-sub.ch
              }
              sub.ch <- evt
          } else {
              select { case <-sub.ch: default: }
              select { case sub.ch <- evt: default: }
          }
      }
  }
  ```
- subscriber channel buffer 是 1024（`hub.go:113`），正常情况几乎不可能填满。此改动为纯防御性编程。
- 同时补充 `Close()` 分支测试：当前 `Hub.Close()` 在 channel 满时可能投递不了 `HubEvent{Done:true}`，但 handler 读到 closed channel 会补发 done。测试要覆盖 closed channel 兜底路径，避免未来代码依赖 Done sentinel 时退化。

---

## 不改的部分

| 层 | 结论 |
|---|---|
| agentend `_execute_stream` | async yield 直接推送，无性能缓冲问题；但存在异常时无条件标 completed 的状态问题，归入 Phase 0 |
| agentend `ClaudeCodeAdapter` | 使用 `--include-partial-messages` 逐 token 推送，已最优 |
| backend Hub 热路径 | `bufferTextLine` 中 Hub.Publish 每个文本 token 都即时推送 |
| backend stream handler | 每个事件后立即 `Flush()`，SSE header 正确；但重连 replay 去重 / heartbeat 契约 / UTF-8 replay 需修 |
| backend flush 间隔 | 只影响持久化，不影响用户感知，收益可忽略 |
| frontend sse.ts | 使用 EventSource 合理；但 `reconnect: true` 必须配套 replay 去重或权威重建 |
| frontend `_scheduleFlush` | rAF 批量策略本身正确，Phase 1 仅在此基础上增加增量解析 |

---

## 优先级总结

| Phase | 优先级 | 说明 |
|-------|--------|------|
| 0 | **BLOCKER** | 先修重连重复、失败状态覆盖、长 SSE 行、agentend outcome、heartbeat/stale、UTF-8 replay、error 内容保留等正确性问题 |
| 1 | **HIGH** | 核心性能瓶颈。必须实现增量 parser 状态和 stable coalesce |
| 2 | **HIGH** | 但必须等 Phase 1 验证引用稳定性后再做 |
| 3 | **MEDIUM** | 需"最后一个未闭合 block"定位逻辑 + 未闭合 fence 处理 |
| 4 | **LOW** | 流式热路径实际只触发 1 次 sync，非流式场景才多次 |
| 5 | **SKIP** | 不影响用户感知，收益可忽略 |
| 6 | **LOW** | 防御性编程，1024 buffer 几乎不会满 |

---

## 验证方案

1. **Phase 0**: 断线重连不重复；scanner error / ERROR event 不被 completed 覆盖；超长 SSE 行可处理；agentend/backend 状态一致；heartbeat 契约与 stale watchdog 一致；UTF-8 replay 不乱码；error 不清空已显示内容
2. **Phase 1**: 引用稳定性断言 `prevBlocks[i] === nextBlocks[i]`；超过 200 字的跨 chunk `aka_yhy` fence 能正确解析；`console.time` 增量 vs 全量对比
3. **Phase 2**: React DevTools Profiler 确认非活跃 block 0 次重渲染
4. **Phase 3**: 肉眼验证流式纯文本 → 完成"格式化弹出"；已完成 block 不受影响
5. **Phase 4**: `syncComposedState` 非流式场景调用次数从 2-3 降为 1
6. **Phase 6**: 单元测试：channel 满时 done/error 事件仍能投递；`Close()` 后 closed channel 兜底仍发 done
7. **端到端**: `make run` 启动三端，发送消息观察流式输出流畅度；中途断网/刷新/切 Tab 后内容不重复、不丢失
