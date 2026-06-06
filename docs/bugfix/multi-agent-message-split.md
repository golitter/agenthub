# Bug Fix: 多 Agent 消息拆分 — 页面刷新后消息混淆 + 重复消息

> 日期: 2026-05-29
> 关联 Change: `openspec/changes/fix-multi-agent-message-split/`

## 现象

Orchestrator 群聊场景下，实时流中前端通过 `streamAgentUpdate` 能正确按 Agent 切换显示消息气泡。但存在两个持久化层问题：

1. **页面刷新后所有 Agent 输出混为一条消息** — orchestrator、claude-code、orchestrator 的全部输出合并为一条 MySQL Message，`agent_type` 固定为初始值（orchestrator），刷新后所有内容显示在一个气泡下
2. **ExecutionEngine 双写导致重复消息** — `ExecutionEngine` 为子 Agent 单独调用 `backend_client.run_task()`，在子 Agent 的 session 下产生重复消息，`ListMessages` 按 `task_id` 查询不分 session，导致重复内容混入
3. **SSE 重放缺少 agent 元数据** — `serveCompleted` 和 `serveStreaming` Phase 1 使用 `FormatSSE`（不含 agent 信息），重连或加载已完成消息时前端无法正确归属消息到对应 Agent

---

## 根因分析

### Bug 1: 多 Agent 输出合并为一条 Message

**根因**: `StreamWriter` 为每次 RunTask 创建一条 Message，整个 SSE 流的所有内容追加到这一条记录中。虽然 SSE TEXT 事件已携带 `agent_type`/`agent` 字段，但 StreamWriter 从未解析这些字段，也没有在 Agent 切换时创建新 Message。

**调用链**:

```
RunTask handler → NewStreamWriter(taskID, sessionID, messageID, agentType)
  → sw.Run(scanner)
    → scanFunc → appendText(text)  ← 所有 Agent 的文本追加到同一个 buf
      → doFlush()                  ← 全部写入同一个 messageID
```

**修复** (`backend/internal/stream/writer.go`):

StreamWriter 新增 Agent 切换检测，在 `agent_type` 变化时自动创建新 Message：

1. 新增字段 `originalMessageID`（初始 messageID，不变）、`currentAgentType`、`currentAgentName`
2. `Run()` 中解析 TEXT 事件的 `agent_type`/`agent`，变化时调用 `switchAgent()`
3. `switchAgent()` 流程：

```go
func (sw *StreamWriter) switchAgent(newAgentType, newAgentName string) {
    // 1. Flush 当前 buffer 到当前 Message
    sw.doFlush()
    // 2. 将当前 Message 标记 completed（不是原始 Message）
    sw.updateMessageStatus(sw.messageID, "completed")
    // 3. 创建新 Message（同 session，新 agent_type，status=streaming）
    newMsg := model.Message{
        MessageID: uuid.New().String(),
        TaskID:    sw.taskID,
        SessionID: sw.sessionID,
        Role:      "agent",
        AgentType: newAgentType,
        AgentName: newAgentName,
        Status:    "streaming",
    }
    db.GetDB().Create(&newMsg)
    // 4. 切换内部 messageID，重置 buffer
    sw.messageID = newMsgID
    sw.buf.Reset()
}
```

4. `finish()` 同时 finalize 最后一条子 Message 和原始 Message
5. `registry.Delete` 使用 `originalMessageID`（保证 `IsActive` 对前端连接始终正确）
6. buffer 为空时跳过 finalize，仅更新 agent 信息

**关键设计决策 — 原始 Message 保持 streaming 直到整轮结束**:

`ServeStream` 通过 `IsActive(originalMessageID)` 判断流是否活跃。若原始 Message 提前 completed，前端重连时 `serveCompleted` 会立即发送 done，导致丢失后续事件。因此原始 Message 仅在 `finish()` 时标记 completed/failed。

**Redis stream key 不变**: 所有事件继续写入原始 Message 的 Redis stream，不创建独立 stream。前端通过同一个 SSE 连接接收所有 Agent 的事件，`streamAgentUpdate` 已能正确处理 agent 切换。

### Bug 2: ExecutionEngine 双写导致重复消息

**根因**: `ListMessages` 按 `task_id` 查询，返回所有 session 的消息。ExecutionEngine 为子 Agent 调用 `run_task()` 创建的 Message 属于子 Agent 的 session，与 orchestrator session 的消息内容重复。

**修复** (`backend/internal/handler/message.go` + 前端):

`ListMessages` 新增可选 `session_id` query param：

```go
sessionID := c.Query("session_id")
query := db.GetDB().Where("task_id = ?", taskID)
if sessionID != "" {
    query = query.Where("session_id = ?", sessionID)
}
```

前端 `getTaskMessages` 和 `use-chat-stream.ts` 传入当前 `sessionId`，只加载当前 session 的消息。不传 `session_id` 时行为不变（向后兼容）。

### Bug 3: SSE 重放缺少 agent 元数据

**根因**: `FormatSSE(text)` 仅生成 `{type:"text", content:{text}}`，不包含 `agent_type`/`agent` 字段。`serveCompleted` 和 `serveStreaming` Phase 1 使用此函数重放 MySQL 历史内容，前端收到的事件缺少 agent 信息。

**修复** (`backend/internal/stream/writer.go` + `backend/internal/handler/stream.go`):

1. 新增 `FormatSSEWithMeta(text, agentType, agentName)`:

```go
func FormatSSEWithMeta(text, agentType, agentName string) string {
    content := map[string]string{"text": text}
    if agentType != "" {
        content["agent_type"] = agentType
    }
    if agentName != "" {
        content["agent"] = agentName
    }
    event := map[string]interface{}{"type": "text", "content": content}
    data, _ := json.Marshal(event)
    return fmt.Sprintf("data: %s", string(data))
}
```

2. `serveCompleted`、`serveStreaming` Phase 1、`serveFailed` 均改用 `FormatSSEWithMeta`，传入消息自身的 `AgentType`/`AgentName`
3. `agent_type` 为空时（旧数据或单 Agent）不包含 agent 字段，向后兼容

---

## 改后效果

### 一次性 orchestrator 运行，MySQL 产生 3 条独立 Message：

| Message | agent_type    | agent_name | status    | 内容             |
|---------|---------------|------------|-----------|------------------|
| 1（原始）| orchestrator  | 项目经理    | completed | "我来分析规划..."  |
| 2（子消息）| claude-code   | 代码助手    | completed | "执行 pwd..."    |
| 3（子消息）| orchestrator  | 项目经理    | completed | "总结如下..."     |

### 前端显示效果：

- **实时流**: 不变 — `streamAgentUpdate` 已支持 agent 切换
- **页面刷新/历史加载**: 3 条消息各自显示为独立气泡，`agent_type` 正确，头像和名称正确
- **SSE 重连**: Phase 1 发送的历史内容带 agent 元数据，前端正确归属
- **单 Agent 会话**: 不受影响 — `agent_type` 不变，不触发 switchAgent

---

## 修改文件清单

| 文件 | 变更 |
|------|------|
| `backend/internal/stream/writer.go` | 新增 `originalMessageID`/`currentAgentType`/`currentAgentName` 字段；新增 `switchAgent()` 方法；`Run()` 解析 agent_type 触发切换；`finish()` 同时 finalize 原始+最后一条 Message；`updateStatus` → `updateMessageStatus(messageID, status)`；新增 `FormatSSEWithMeta()` |
| `backend/internal/handler/stream.go` | `serveCompleted`/`serveStreaming` Phase 1/`serveFailed` 改用 `FormatSSEWithMeta`，传入消息的 `AgentType`/`AgentName`（**注：后端已重构为 controller + service 分层，对应文件现为 `controller/impl/stream_controller.go` + `service/impl/stream_service.go`**） |
| `backend/internal/handler/message.go` | `ListMessages` 新增可选 `session_id` query param 过滤（**注：后端已重构为 controller + service 分层，对应文件现为 `controller/impl/message_controller.go` + `service/impl/message_service.go`**） |
| `frontend/src/lib/api.ts` | `getTaskMessages` 参数新增 `sessionId` 可选字段 |
| `frontend/src/hooks/use-chat-stream.ts` | `useEffect` 历史加载传入 `sessionId` |
| `frontend/src/components/chat/ChatArea.tsx` | `loadMoreMessages` 传入 `sessionId` |
