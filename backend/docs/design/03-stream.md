# Stream — SSE 流式中转

## 实现了什么

`StreamWriter` 消费 AgentEnd 的 SSE 响应流，通过双层通道（内存 Hub + Redis Stream）实时推送事件，同时将文本内容定时批量刷写到 MySQL Message 表。支持 Agent 类型切换（Orchestrator 场景下自动拆分子消息）、跨 Session 转发不持久化、AskCard/PlanReview 运行时块事件持久化、Redis Stream key 管理、全局 goroutine 注册表、启动时残留消息清理等机制。

## 怎么实现的

### StreamWriter 结构体 (`internal/stream/writer.go`)

```go
type StreamWriter struct {
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	messageID string // current (latest) message ID — updated on agent switch
	sessionID string
	taskID    string
	streamKey string

	originalMessageID string // first message ID — never changes, used for registry and Redis stream
	currentAgentType  string // tracks the current agent type from SSE events
	currentAgentName  string // tracks the current agent name from SSE events
	currentSourceID   string // upstream logical message boundary hint from agentend
	sourcePersistSkip map[string]bool
	splitAfterForward bool
	askCardMessageIDs map[string]string

	buf        strings.Builder
	bufLen     int
	flushedLen int
	lastSeq    string
	lastFlush  time.Time
	mu         sync.Mutex

	textBuf      []string // buffered text snippets for TEXT events awaiting merge
	textBufSize  int      // total byte size of textBuf
	textBufStart time.Time
}
```

常量配置：

```go
const (
	flushInterval    = 500 * time.Millisecond // 定时刷写间隔
	flushThreshold   = 2048                   // 缓冲区字节数阈值
	maxStreamLen     = 10000                  // Redis Stream 最大长度
	streamExpireTTL  = 600 * time.Second      // 流结束后 Redis key TTL
	goroutineTimeout = 30 * time.Minute       // 单次 goroutine 超时
	textBatchSize    = 2048                   // TEXT 事件批量合并大小
	textBatchAge     = 500 * time.Millisecond // TEXT 事件批量合并等待时间
)
```

### 双层通道：RuntimeHub + Redis Stream

`RuntimeHub`（`internal/stream/hub.go`）是一个内存发布/订阅中心，用于低延迟 SSE 推送：

```go
// Hub is the global RuntimeHub instance.
var Hub = &RuntimeHub{streams: make(map[string]*RuntimeStream)}

// Publish sends an event to all subscribers of the given stream key.
func (h *RuntimeHub) Publish(key, data string)

// Subscribe returns a channel for consuming events and the current sequence number.
func (h *RuntimeHub) Subscribe(key string) (<-chan HubEvent, uint64)

// Close marks the stream as done and removes it from the hub.
func (h *RuntimeHub) Close(key string)
```

- **Hot path**：`Hub.Publish()` 立即推送到 SSE 订阅者（内存 channel，无网络延迟）
- **Cold path**：Redis Stream `XADD` 持久化存储，用于断线重连和数据恢复

### 创建与注册

通过 `NewStreamWriter` 创建实例并注册到全局 `sync.Map` 注册表，设置 30 分钟超时 context：

```go
var registry sync.Map

func IsActive(messageID string) bool {
	_, ok := registry.Load(messageID)
	return ok
}

func NewStreamWriter(ctx context.Context, taskID, sessionID, messageID, agentType string) *StreamWriter {
	childCtx, cancel := context.WithTimeout(ctx, goroutineTimeout)
	key := pkgredis.StreamKey(sessionID, messageID)
	sw := &StreamWriter{
		ctx: childCtx, cancel: cancel,
		messageID: messageID, sessionID: sessionID,
		taskID: taskID, streamKey: key,
		originalMessageID: messageID,
		currentAgentType:  agentType,
	}
	registry.Store(messageID, sw)
	return sw
}
```

Redis Stream key 格式为 `agent:{sessionId}:{messageId}`，由 `pkg/redis.StreamKey()` 生成：

```go
func StreamKey(sessionID, messageID string) string {
	return fmt.Sprintf("agent:%s:%s", sessionID, messageID)
}
```

### Run — 主消费循环

`Run` 接收一个扫描函数，将每行 SSE 数据同时发送到 Hub（立即推送）和 Redis（持久化），并处理 Agent 类型切换：

```go
func (sw *StreamWriter) Run(scanFunc func(func(line string))) {
	defer sw.finish()
	sw.wg.Add(1)
	go sw.flushLoop()

	sawError := false
	scanFunc(func(line string) {
		if sw.ctx.Err() != nil { return }

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var event generated.StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				switch event.Type {
				case generated.EventTypeText:
					if text, ok := event.Content["text"].(string); ok {
						sourceMessageID, _ := event.Content["message_id"].(string)
						// Forward without persist if source is from another session
						if sw.shouldForwardTextWithoutPersist(sourceMessageID) {
							sw.flushTextBuffer()
							sw.publishForwardedText(text, agentType, agentName, sourceMessageID)
							return
						}
						// Split after forwarded text, or on agent/message boundary change
						if sw.shouldSplitAfterForward() ||
							newAgentType != sw.currentAgentType ||
							(sourceMessageID != "" && sourceMessageID != sw.currentSourceID) {
							sw.flushTextBuffer()
							sw.switchAgent(newAgentType, newAgentName, sourceMessageID)
						}
						sw.appendText(text)
						sw.bufferTextLine(text) // batched Redis publish
						return
					}
				case generated.EventTypeDone:
					sw.flushTextBuffer()
				case generated.EventTypeError:
					sw.flushTextBuffer()
					sawError = true
					if errMsg, ok := event.Content["error"].(string); ok && errMsg != "" {
						sw.appendText("[Error] " + errMsg)
					}
				case generated.EventTypeAskCardStart:
					sw.flushTextBuffer()
					sw.persistAskCardEvent(event, "pending")
				case generated.EventTypeAskCardDone:
					sw.flushTextBuffer()
					sw.persistAskCardEvent(event, askCardStatus(event.Content["status"]))
				case generated.EventTypePlanReview:
					sw.flushTextBuffer()
					sw.persistPlanReviewEvent(event)
					db.GetDB().Model(&model.Session{}).Where(...).Update("status", "awaiting_review")
				default:
					sw.flushTextBuffer()
				}
			}
		} else {
			sw.flushTextBuffer()
		}
		// Non-TEXT lines published immediately
		sw.publishToRedis(line)
	})

	sw.flushTextBuffer()
	sw.doFlush()

	status := "completed"
	if sawError { status = "failed" }
	sw.updateMessageStatus(sw.messageID, status)
	if sw.messageID != sw.originalMessageID {
		sw.updateMessageStatus(sw.originalMessageID, status)
	}
}
```

### Agent 类型切换（switchAgent）

当 Orchestrator 场景下 SSE 事件携带不同的 `agent_type` 或 `message_id` 边界时，自动拆分子消息：

```go
func (sw *StreamWriter) switchAgent(newAgentType, newAgentName, sourceMessageID string) {
	// Flush current buffer to the current Message
	sw.doFlush()
	// Finalize current sub-message (not the original — it stays streaming)
	sw.updateMessageStatus(sw.messageID, "completed")
	// Create new Message in MySQL
	newMsgID := uuid.New().String()
	db.GetDB().Create(&model.Message{MessageID: newMsgID, ...Status: "streaming"})
	// Switch internal state to new Message
	sw.messageID = newMsgID
	sw.currentAgentType = newAgentType
	sw.currentAgentName = newAgentName
	sw.currentSourceID = sourceMessageID
}
```

此外还支持**转发不持久化**机制：当 TEXT 事件的 `message_id` 指向其他 Session 的消息时，仅通过 Hub/Redis 转发，不写入当前 Session 的 MySQL Message（`shouldForwardTextWithoutPersist`）。转发后下一个事件自动触发 `switchAgent` 拆分（`splitAfterForward`）。

### 双层 Redis 写入

每行 SSE 数据通过双层通道发布：

```go
func (sw *StreamWriter) publishToRedis(line string) {
	// Hot path: immediate push to in-memory hub for low-latency SSE delivery
	if strings.HasPrefix(line, "data: ") {
		Hub.Publish(sw.streamKey, line)
	}
	// Cold path: durable Redis Stream for replay/reconnect
	sw.publishToRedisOnly(line)
}
```

TEXT 事件通过 `bufferTextLine` 批量合并后写入 Redis（冷路径），但每个 token 已经通过 Hub 立即推送到 SSE 订阅者（热路径）：

```go
func (sw *StreamWriter) bufferTextLine(text string) {
	// Hot path: immediate push to hub with full metadata
	Hub.Publish(sw.streamKey, FormatSSEWithMeta(text, agentType, agentName, currentMessageID))
	// Cold path: buffer plain text for batched Redis publish
	sw.textBuf = append(sw.textBuf, text)
	if sw.textBufSize >= textBatchSize || time.Since(sw.textBufStart) >= textBatchAge {
		sw.flushTextBuffer()
	}
}
```

### MySQL 批量刷写

双触发机制：缓冲区增量满 2048 字节或 500ms 定时器到期。

定时刷写 goroutine：

```go
func (sw *StreamWriter) flushLoop() {
	defer sw.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sw.flushTextBuffer()
			sw.mu.Lock()
			hasContent := sw.bufLen > 0
			sw.mu.Unlock()
			if hasContent {
				sw.doFlush()
			}
		case <-sw.ctx.Done():
			return
		}
	}
}
```

刷写逻辑 — 替换式写入（每次刷写写入完整内容，非追加），使用 `flushedLen` 追踪已刷写字节数避免无变化时重复写入：

```go
func (sw *StreamWriter) doFlush() {
	sw.mu.Lock()
	content := sw.buf.String()
	seq := sw.lastSeq
	if content == "" {
		sw.mu.Unlock()
		return
	}
	sw.flushedLen = sw.bufLen
	sw.lastFlush = time.Now()
	sw.mu.Unlock()

	db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", sw.messageID).
		Updates(map[string]interface{}{
			"content":  content,
			"last_seq": seq,
		})
}
```

### 结束与清理

流结束后关闭 Hub stream（`ServeStream` 层的 SSE handler 负责向订阅者发送终端 DONE 事件），设置 Redis key 600 秒 TTL，从注册表移除：

```go
func (sw *StreamWriter) finish() {
	sw.cancel()
	sw.wg.Wait()

	// Close hub stream — ServeStream emits the terminal DONE to subscribers
	Hub.Close(sw.streamKey)

	// Set Redis EXPIRE on the stream
	rdb := pkgredis.GetClient()
	if rdb != nil {
		if err := rdb.Expire(context.Background(), sw.streamKey, streamExpireTTL).Err(); err != nil {
			slog.Warn("failed to set stream expire", "key", sw.streamKey, "error", err)
		}
	}
	registry.Delete(sw.originalMessageID)
}
```

### 辅助函数

**Fail** — 主动标记消息为 failed（例如 context 取消或 scanner 出错时调用）：

```go
func (sw *StreamWriter) Fail() {
	sw.doFlush()
	sw.updateMessageStatus(sw.messageID, "failed")
	if sw.messageID != sw.originalMessageID {
		sw.updateMessageStatus(sw.originalMessageID, "failed")
	}
}
```

**PublishErrorAndFail** — 当 AgentEnd 在流式之前或中途失败时，向 Hub 和 Redis Stream 写入 error 事件并标记 Message 为 failed：

```go
func PublishErrorAndFail(messageID, sessionID, errMsg string) {
	key := pkgredis.StreamKey(sessionID, messageID)
	sseLine := fmt.Sprintf("data: %s", ...)
	// Hot path: push error to hub immediately
	Hub.Publish(key, sseLine)
	// Cold path: durable Redis
	rdb := pkgredis.GetClient()
	if rdb != nil {
		rdb.XAdd(context.Background(), &redis.XAddArgs{...}).Result()
		rdb.Expire(context.Background(), key, streamExpireTTL)
	}
	db.GetDB().Model(&model.Message{}).Where("message_id = ?", messageID).Update("status", "failed")
}
```

**CleanupStaleMessages** — 服务启动时将所有残留的 `streaming` 状态消息标记为 `failed`：

```go
func CleanupStaleMessages() {
	result := db.GetDB().Model(&model.Message{}).
		Where("status = ?", "streaming").
		Update("status", "failed")
	if result.RowsAffected > 0 {
		slog.Info("cleaned up stale streaming messages", "count", result.RowsAffected)
	}
}
```

### 运行时块事件持久化

除 TEXT/DONE/ERROR 外，StreamWriter 还处理两类运行时块事件：

**AskCard 事件**（`EventTypeAskCardStart` / `EventTypeAskCardDone`）— Orchestrator 跨 Agent 提问卡片：

- `persistAskCardEvent` 将卡片元数据（question_id、source/target agent、问题、状态）以 `legacyRuntimeBlockLine` 格式内联写入 Message content
- Start 时可能触发 `switchAgent` 拆分（如果之前有转发文本）
- 使用 `askCardMessageIDs` map 确保同一 question_id 的更新追加到同一个子消息

**PlanReview 事件**（`EventTypePlanReview`）— Orchestrator 规划审查：

- `persistPlanReviewEvent` 将规划内容（plan、waves、status=pending）写入 Message content
- 同时将 Session 状态更新为 `awaiting_review`
- 前端通过 `POST /api/tasks/:taskId/review` 提交审查结果后，`markLatestPlanReviewBlock` 更新块内 status

**FormatSSE** / **FormatSSEWithMeta** — 将文本块格式化为 SSE data 行：

```go
func FormatSSE(text string) string { ... }

func FormatSSEWithMeta(text, agentType, agentName, messageID string) string {
	content := map[string]string{"text": text}
	if agentType != "" { content["agent_type"] = agentType }
	if agentName != "" { content["agent"] = agentName }
	if messageID != "" { content["message_id"] = messageID }
	event := map[string]interface{}{"type": "text", "content": content}
	data, _ := json.Marshal(event)
	return fmt.Sprintf("data: %s", string(data))
}
```

### 事件流全景

```
AgentEnd (FastAPI :8001)
    │ SSE response body
    ▼
RunTask handler goroutine
    │ bufio.Scanner 逐行读取
    ├──────────────────────────────────┐
    ▼                                  ▼
publishToRedis()                  appendText()
    │ Hub.Publish() (hot)              │ 增量 >= 2048B?
    │ publishToRedisOnly() (cold)      ▼
    ▼                              doFlush()
RuntimeHub (内存)                     │ UPDATE content, last_seq
    │ 1024-buffered channel            ▼
    ▼                          MySQL messages 表
StreamHandler.serveStreaming()
    │ Hub.Subscribe() (实时)
    │ XREAD (Redis 缺口重放)
    ▼
前端 EventSource
```
