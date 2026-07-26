package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	pkgredis "agenthub/backend/pkg/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	flushInterval    = 500 * time.Millisecond
	flushThreshold   = 2048
	maxStreamLen     = 10000
	streamExpireTTL  = 600 * time.Second
	goroutineTimeout = 30 * time.Minute
	textBatchSize    = 2048
	textBatchAge     = 500 * time.Millisecond
	redisOpTimeout   = 5 * time.Second
)

// RunOutcome 表示消费 agentend SSE 流的终态结果。
type RunOutcome string

const (
	RunOutcomeCompleted RunOutcome = RunOutcome(generated.MessageStatusCompleted)
	RunOutcomeFailed    RunOutcome = RunOutcome(generated.MessageStatusFailed)
)

// registry 按 messageID 跟踪正在运行的 StreamWriter goroutine。
var registry sync.Map

// IsActive 判断给定 messageID 是否仍有 goroutine 在运行。
func IsActive(messageID string) bool {
	_, ok := registry.Load(messageID)
	return ok
}

// StreamWriter 消费 agentend 的 SSE 流，将事件发布到 Redis Stream，
// 并把文本内容批量刷写到 MySQL。
// 当 SSE 事件中的 agent_type 发生变化时，它会终结当前 Message，并在同一会话下
// 创建一条新 Message；原始 Message 会保持 streaming 状态，直到整轮对话结束。
type StreamWriter struct {
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	messageID string // 当前（最新）message ID —— 在 agent 切换时更新
	sessionID string
	taskID    string
	streamKey string

	originalMessageID string // 首条 message ID —— 永不变化，用于 registry 与 Redis stream
	currentAgentType  string // 跟踪 SSE 事件中的当前 agent 类型
	currentAgentName  string // 跟踪 SSE 事件中的当前 agent 名称
	currentSourceID   string // 来自 agentend 的上游逻辑消息边界提示
	groupID           string // 当前活跃消息所属的编排分组
	sourcePersistSkip map[string]bool
	splitAfterForward bool
	askCardMessageIDs map[string]string
	groupMessageIDs   map[string]string

	buf        strings.Builder
	bufLen     int
	flushedLen int
	lastSeq    string
	lastFlush  time.Time
	mu         sync.Mutex

	textBuf      []string // 缓冲的 TEXT 事件文本片段，等待合并
	textBufSize  int      // textBuf 的总字节数
	textBufStart time.Time

	messageDao      dao.MessageDao
	sessionDao      dao.SessionDao
	diffSnapshotDao dao.DiffSnapshotDao
}

// NewStreamWriter 创建一个新的 StreamWriter 并注册到 registry。
func NewStreamWriter(ctx context.Context, taskID, sessionID, messageID, agentType string, messageDao dao.MessageDao, sessionDao dao.SessionDao, diffSnapshotDao dao.DiffSnapshotDao) *StreamWriter {
	childCtx, cancel := context.WithTimeout(ctx, goroutineTimeout)
	key := pkgredis.StreamKey(sessionID, messageID)
	sw := &StreamWriter{
		ctx:               childCtx,
		cancel:            cancel,
		messageID:         messageID,
		sessionID:         sessionID,
		taskID:            taskID,
		streamKey:         key,
		originalMessageID: messageID,
		currentAgentType:  agentType,
		currentAgentName:  "",
		sourcePersistSkip: make(map[string]bool),
		askCardMessageIDs: make(map[string]string),
		groupMessageIDs:   make(map[string]string),
		messageDao:        messageDao,
		sessionDao:        sessionDao,
		diffSnapshotDao:   diffSnapshotDao,
	}
	registry.Store(messageID, sw)
	return sw
}

// Run 消费 agentend 响应体（按 SSE 行），发布到 Redis 并刷写到 MySQL。
// 该方法应在 goroutine 中调用。它返回终态结果，调用方可据此保持 Session 状态与
// Message 状态一致。
func (sw *StreamWriter) Run(scanFunc func(func(line string)) error) RunOutcome {
	defer sw.finish()

	sw.wg.Add(1)
	go sw.flushLoop()

	sawError := false

	scanErr := scanFunc(func(line string) {
		if sw.ctx.Err() != nil {
			return
		}

		// 解析 SSE data 行，用于按事件类型路由
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var event generated.StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				switch event.Type {
				case generated.EventTypeText:
					if text, ok := event.Content["text"].(string); ok {
						newAgentType, _ := event.Content["agent_type"].(string)
						if newAgentType == "" {
							newAgentType = sw.currentAgentType
						}
						newAgentName, _ := event.Content["agent"].(string)
						if newAgentName == "" {
							newAgentName = sw.currentAgentName
						}
						sourceMessageID, _ := event.Content["message_id"].(string)
						groupID, _ := event.Content["group_id"].(string)

						if groupID != "" {
							targetMessageID := sw.ensureGroupedAgentMessage(newAgentType, newAgentName, groupID)
							if targetMessageID == "" {
								return
							}

							sw.mu.Lock()
							currentMessageID := sw.messageID
							currentAgentType := sw.currentAgentType
							currentAgentName := sw.currentAgentName
							currentGroupID := sw.groupID
							currentSourceID := sw.currentSourceID
							sw.mu.Unlock()

							if currentMessageID != targetMessageID ||
								currentAgentType != newAgentType ||
								currentAgentName != newAgentName ||
								currentGroupID != groupID ||
								(sourceMessageID != "" && currentSourceID != sourceMessageID) {
								sw.flushTextBuffer()
								sw.switchAgent(newAgentType, newAgentName, sourceMessageID, groupID, targetMessageID)
							}

							sw.appendText(text)
							sw.bufferTextLine(text)
							return
						}

						if sw.shouldForwardTextWithoutPersist(sourceMessageID) {
							if sw.needsAgentSwitch(newAgentType, newAgentName, sourceMessageID, "") {
								sw.flushTextBuffer()
								sw.switchAgent(newAgentType, newAgentName, sourceMessageID, "", "")
							}
							sw.appendText(text)
							sw.bufferTextLine(text)
							sw.markSplitAfterForward()
							return
						}

						if sw.shouldSplitAfterForward() {
							sw.flushTextBuffer()
							sw.switchAgent(newAgentType, newAgentName, sourceMessageID, "", "")
						} else if newAgentType != sw.currentAgentType ||
							(sourceMessageID != "" && sourceMessageID != sw.currentSourceID) {
							// 检查 agent 切换或上游消息边界切换。
							sw.flushTextBuffer()
							sw.switchAgent(newAgentType, newAgentName, sourceMessageID, "", "")
						} else if newName, ok := event.Content["agent"].(string); ok && newName != "" {
							// agentType 相同但提供了 name —— 更新跟踪信息，确保
							// flushTextBuffer 输出正确的元信息（例如流开始后
							// Orchestrator 的首条 TEXT）。
							sw.mu.Lock()
							sw.currentAgentName = newName
							if sourceMessageID != "" {
								sw.currentSourceID = sourceMessageID
							}
							sw.groupID = ""
							sw.mu.Unlock()
						}
						sw.appendText(text)
						// 缓冲 TEXT 事件，待批量发布到 Redis
						sw.bufferTextLine(text)
						return
					}
				case generated.EventTypeDone:
					sw.flushTextBuffer()
				case generated.EventTypeError:
					sw.flushTextBuffer()
					sawError = true
					if errMsg := eventErrorMessage(event.Content); errMsg != "" {
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
					if err := sw.sessionDao.UpdateStatusByTask(sw.sessionID, sw.taskID, string(generated.SessionStateAwaitingReview)); err != nil {
						slog.Warn("failed to mark session awaiting review", "task_id", sw.taskID, "session_id", sw.sessionID, "error", err)
					}
				case generated.EventTypePlanning,
					generated.EventTypeRuntimeExecuting,
					generated.EventTypeRuntimeCompleted,
					generated.EventTypeCoordinationMessage,
					generated.EventTypeCoordinationDone:
					sw.flushTextBuffer()
					sw.persistRuntimeBlockEvent(event)
				default:
					// runtime_text、tool_call、tool_result 等 —— 先刷写文本缓冲
					sw.flushTextBuffer()
				}
			}
		} else {
			// 非 data 行（例如 "event: ..." 行）—— 先刷写文本缓冲
			sw.flushTextBuffer()
		}
		// 非 TEXT 行立即发布
		sw.publishToRedis(line)
	})
	if scanErr != nil {
		sawError = true
		sw.flushTextBuffer()
		errMsg := fmt.Sprintf("stream read error: %v", scanErr)
		sw.appendText("[Error] " + errMsg)
		sw.publishToRedis(formatErrorSSE(errMsg))
	}

	// 最终刷写
	sw.flushTextBuffer()
	sw.doFlush()

	outcome := RunOutcomeCompleted
	if sawError {
		outcome = RunOutcomeFailed
	}
	// 终结当前（最后一条）子消息
	sw.updateMessageStatus(sw.messageID, string(outcome))
	// 终结原始消息（若未发生 agent 切换，则与上面是同一条）
	if sw.messageID != sw.originalMessageID {
		sw.updateMessageStatus(sw.originalMessageID, string(outcome))
	}
	return outcome
}

// switchAgent 处理 agent/message 的过渡：刷写缓冲、终结当前 Message，
// 并在发言者或上游消息变化时，在同一会话下创建一条新 Message。
func (sw *StreamWriter) switchAgent(newAgentType, newAgentName, sourceMessageID, groupID, targetMessageID string) {
	sw.mu.Lock()
	hasContent := sw.bufLen > 0
	currentMessageID := sw.messageID
	currentAgentType := sw.currentAgentType
	currentAgentName := sw.currentAgentName
	sw.mu.Unlock()

	if hasContent {
		// 将当前缓冲刷写到当前 Message
		sw.doFlush()

		// 终结当前子消息。原始消息保持 streaming 状态直到整轮结束，
		// 这样迟到的 SSE 订阅者不会在子 agent 输出仍在产生时就收到提前的 done。
		if currentMessageID != sw.originalMessageID && currentMessageID != targetMessageID {
			sw.updateMessageStatus(currentMessageID, string(generated.MessageStatusCompleted))
		}
	}

	if targetMessageID != "" {
		sw.seedBufferFromMessage(targetMessageID, groupID)
	} else if hasContent || shouldCreateEmptySubMessage(currentMessageID, sw.originalMessageID, sourceMessageID, newAgentType, newAgentName, currentAgentType, currentAgentName) {
		newMsgID := sw.createSubMessage(newAgentType, newAgentName, groupID)
		if newMsgID == "" {
			return
		}
		sw.mu.Lock()
		sw.messageID = newMsgID
		sw.buf.Reset()
		sw.bufLen = 0
		sw.flushedLen = 0
		sw.groupID = groupID
		sw.mu.Unlock()
	}

	// 始终更新 agent 跟踪信息
	sw.mu.Lock()
	sw.currentAgentType = newAgentType
	sw.currentAgentName = newAgentName
	sw.currentSourceID = sourceMessageID
	sw.groupID = groupID
	sw.mu.Unlock()
}

func (sw *StreamWriter) needsAgentSwitch(newAgentType, newAgentName, sourceMessageID, groupID string) bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.messageID == "" ||
		sw.currentAgentType != newAgentType ||
		(newAgentName != "" && sw.currentAgentName != newAgentName) ||
		(sourceMessageID != "" && sw.currentSourceID != sourceMessageID) ||
		sw.groupID != groupID
}

func (sw *StreamWriter) createSubMessage(agentType, agentName, groupID string) string {
	newMsgID := uuid.New().String()
	newMsg := model.Message{
		MessageID: newMsgID,
		TaskID:    sw.taskID,
		SessionID: sw.sessionID,
		Role:      string(generated.MessageRoleAgent),
		Content:   "",
		Status:    string(generated.MessageStatusStreaming),
		AgentType: agentType,
		AgentName: agentName,
		GroupID:   groupID,
	}
	if err := sw.messageDao.CreateMessage(newMsg); err != nil {
		slog.Error("create sub-message failed", "error", err)
		return ""
	}
	return newMsgID
}

func groupMessageKey(groupID, agentType, agentName string) string {
	return groupID + "\x00" + agentType + "\x00" + agentName
}

func (sw *StreamWriter) ensureGroupedAgentMessage(agentType, agentName, groupID string) string {
	key := groupMessageKey(groupID, agentType, agentName)

	sw.mu.Lock()
	if messageID, ok := sw.groupMessageIDs[key]; ok && messageID != "" {
		sw.mu.Unlock()
		return messageID
	}
	sw.mu.Unlock()

	messageID := sw.createSubMessage(agentType, agentName, groupID)
	if messageID == "" {
		return ""
	}

	sw.mu.Lock()
	sw.groupMessageIDs[key] = messageID
	sw.mu.Unlock()
	return messageID
}

func (sw *StreamWriter) seedBufferFromMessage(messageID, groupID string) {
	content, err := sw.messageDao.FindMessageContent(messageID)
	if err != nil {
		slog.Error("load grouped message content failed", "message_id", messageID, "error", err)
		content = ""
	}

	sw.mu.Lock()
	sw.messageID = messageID
	sw.buf.Reset()
	sw.buf.WriteString(content)
	sw.bufLen = len(content)
	sw.flushedLen = len(content)
	sw.groupID = groupID
	sw.mu.Unlock()
}

func (sw *StreamWriter) shouldForwardTextWithoutPersist(sourceMessageID string) bool {
	if sourceMessageID == "" || sourceMessageID == sw.messageID || sourceMessageID == sw.originalMessageID {
		return false
	}

	sw.mu.Lock()
	if skip, ok := sw.sourcePersistSkip[sourceMessageID]; ok {
		sw.mu.Unlock()
		return skip
	}
	sw.mu.Unlock()

	sessionID, err := sw.messageDao.FindSessionIDByTaskMessage(sw.taskID, sourceMessageID)
	skip := err == nil && sessionID != "" && sessionID != sw.sessionID

	sw.mu.Lock()
	sw.sourcePersistSkip[sourceMessageID] = skip
	sw.mu.Unlock()

	return skip
}

func shouldCreateEmptySubMessage(currentMessageID, originalMessageID, sourceMessageID, newAgentType, newAgentName, currentAgentType, currentAgentName string) bool {
	if currentMessageID != originalMessageID {
		return false
	}
	if sourceMessageID == "" || sourceMessageID == currentMessageID || sourceMessageID == originalMessageID {
		return false
	}
	return newAgentType != currentAgentType || (newAgentName != "" && newAgentName != currentAgentName)
}

func (sw *StreamWriter) markSplitAfterForward() {
	sw.mu.Lock()
	sw.splitAfterForward = true
	sw.mu.Unlock()
}

func (sw *StreamWriter) shouldSplitAfterForward() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if !sw.splitAfterForward {
		return false
	}
	sw.splitAfterForward = false
	return true
}

func (sw *StreamWriter) publishToRedis(line string) {
	// 热路径：立即推送到内存 hub，实现低延迟 SSE 投递
	if strings.HasPrefix(line, "data: ") {
		Hub.Publish(sw.streamKey, line)
	}

	// 冷路径：写入持久化的 Redis Stream，用于回放/重连
	sw.publishToRedisOnly(line)
}

// publishToRedisOnly 只写 Redis Stream，不经过 hub（用于合并后的批量事件，
// 这些事件此前已通过 bufferTextLine 单独推送给了 hub）。
func (sw *StreamWriter) publishToRedisOnly(line string) {
	rdb := pkgredis.GetClient()
	if rdb == nil {
		return
	}
	seq, err := rdb.XAdd(sw.ctx, &redis.XAddArgs{
		Stream: sw.streamKey,
		MaxLen: maxStreamLen,
		Approx: true,
		Values: map[string]interface{}{
			"data": line,
		},
	}).Result()
	if err != nil {
		slog.Error("redis XADD failed", "key", sw.streamKey, "error", err)
		return
	}
	sw.mu.Lock()
	sw.lastSeq = seq
	sw.mu.Unlock()
}

// bufferTextLine 立即把增强后的 TEXT 事件发布到 hub，并把纯文本缓冲起来，
// 留待后续合并发布到 Redis。
func (sw *StreamWriter) bufferTextLine(text string) {
	sw.mu.Lock()
	agentType := sw.currentAgentType
	agentName := sw.currentAgentName
	currentMessageID := sw.messageID
	groupID := sw.groupID
	sw.mu.Unlock()

	// 热路径：立即推送到 hub（不批量）
	Hub.Publish(sw.streamKey, FormatSSEWithMeta(text, agentType, agentName, currentMessageID, groupID))

	// 冷路径：缓冲文本以批量发布到 Redis（避免重复 JSON 解析）
	sw.mu.Lock()
	if len(sw.textBuf) == 0 {
		sw.textBufStart = time.Now()
	}
	sw.textBuf = append(sw.textBuf, text)
	sw.textBufSize += len(text)
	shouldFlush := sw.textBufSize >= textBatchSize || time.Since(sw.textBufStart) >= textBatchAge
	sw.mu.Unlock()

	if shouldFlush {
		sw.flushTextBuffer()
	}
}

// flushTextBuffer 将缓冲的 TEXT 文本合并为一条 SSE 行并发布到 Redis。
func (sw *StreamWriter) flushTextBuffer() {
	sw.mu.Lock()
	buf := sw.textBuf
	sw.textBuf = nil
	sw.textBufSize = 0
	sw.mu.Unlock()

	if len(buf) == 0 {
		return
	}

	var combined strings.Builder
	for _, text := range buf {
		combined.WriteString(text)
	}

	if combined.Len() > 0 {
		sw.mu.Lock()
		agentType := sw.currentAgentType
		agentName := sw.currentAgentName
		currentMessageID := sw.messageID
		groupID := sw.groupID
		sw.mu.Unlock()
		// 仅走冷路径：合并后的批量事件发往 Redis（hub 已收到逐条事件）
		sw.publishToRedisOnly(FormatSSEWithMeta(combined.String(), agentType, agentName, currentMessageID, groupID))
	}
}

func (sw *StreamWriter) appendText(text string) {
	sw.mu.Lock()
	sw.buf.WriteString(text)
	sw.bufLen += len(text)
	shouldFlush := sw.bufLen-sw.flushedLen >= flushThreshold
	sw.mu.Unlock()

	if shouldFlush {
		sw.doFlush()
	}
}

func (sw *StreamWriter) persistAskCardEvent(event generated.StreamEvent, status string) {
	questionID, _ := event.Content["question_id"].(string)
	groupID, _ := event.Content["group_id"].(string)
	if groupID == "" && status == "pending" && sw.shouldSplitAfterForward() {
		sourceAgent, _ := event.Content["source_agent"].(string)
		sourceAgentType, _ := event.Content["source_agent_type"].(string)
		if sourceAgentType == "" {
			sourceAgentType = sw.currentAgentType
		}
		if sourceAgent == "" {
			sourceAgent = sw.currentAgentName
		}
		sw.switchAgent(sourceAgentType, sourceAgent, "", "", "")
	}

	payload := map[string]interface{}{
		"question_id":       event.Content["question_id"],
		"source_agent":      event.Content["source_agent"],
		"source_agent_type": event.Content["source_agent_type"],
		"source_session_id": event.Content["source_session_id"],
		"target_agent":      event.Content["target_agent"],
		"target_agent_type": event.Content["target_agent_type"],
		"target_session_id": event.Content["target_session_id"],
		"question":          event.Content["question"],
		"summary":           event.Content["summary"],
		"status":            status,
		"collapsed":         status == "answered",
	}
	marker := legacyRuntimeBlockLine("ask_agent", payload)

	sw.mu.Lock()
	currentMessageID := sw.messageID
	sw.mu.Unlock()

	targetMessageID := currentMessageID
	if groupID != "" {
		targetAgent, _ := event.Content["target_agent"].(string)
		targetAgentType, _ := event.Content["target_agent_type"].(string)
		targetMessageID = sw.ensureGroupedAgentMessage(targetAgentType, targetAgent, groupID)
		if targetMessageID == "" {
			return
		}
	}

	if questionID != "" && targetMessageID != "" {
		sw.mu.Lock()
		if existingMessageID, ok := sw.askCardMessageIDs[questionID]; ok {
			targetMessageID = existingMessageID
		} else {
			sw.askCardMessageIDs[questionID] = targetMessageID
		}
		sw.mu.Unlock()
	}

	if targetMessageID == currentMessageID {
		sw.appendText(marker)
		sw.doFlush()
		return
	}

	sw.appendTextToMessage(targetMessageID, marker)
}

func (sw *StreamWriter) persistPlanReviewEvent(event generated.StreamEvent) {
	diffSnapshotID, _ := event.Content["diff_snapshot_id"].(string)
	diffText, _ := event.Content["diff"].(string)
	sessionID, _ := event.Content["session_id"].(string)
	if diffSnapshotID != "" && diffText != "" {
		if err := sw.diffSnapshotDao.UpsertPending(diffSnapshotID, sessionID, diffText); err != nil {
			slog.Warn("failed to persist merge diff snapshot", "snapshot_id", diffSnapshotID, "error", err)
		}
	}

	payload := map[string]interface{}{
		"session_id":       event.Content["session_id"],
		"task_id":          event.Content["task_id"],
		"review_key":       event.Content["review_key"],
		"review_type":      event.Content["review_type"],
		"source_branch":    event.Content["source_branch"],
		"target_branch":    event.Content["target_branch"],
		"diff_snapshot_id": diffSnapshotID,
		"plan":             event.Content["plan"],
		"waves":            event.Content["waves"],
		"status":           "pending",
	}
	sw.appendText(legacyRuntimeBlockLine("plan_review", payload))
	sw.doFlush()
}

func (sw *StreamWriter) persistRuntimeBlockEvent(event generated.StreamEvent) {
	marker := legacyRuntimeBlockLineForEvent(event)
	if marker == "" {
		return
	}
	sw.appendText(marker)
	sw.doFlush()
}

func (sw *StreamWriter) appendTextToMessage(messageID, text string) {
	if text == "" {
		return
	}
	sw.mu.Lock()
	seq := sw.lastSeq
	sw.mu.Unlock()

	content, err := sw.messageDao.FindMessageContent(messageID)
	if err != nil {
		slog.Error("load message for runtime block append failed", "message_id", messageID, "error", err)
		return
	}

	err = sw.messageDao.UpdateMessageContentAndSeq(messageID, content+text, seq)
	if err != nil {
		slog.Error("append runtime block to MySQL failed", "message_id", messageID, "error", err)
	}
}

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

	err := sw.messageDao.UpdateMessageContentAndSeq(sw.messageID, content, seq)
	if err != nil {
		slog.Error("flush to MySQL failed", "message_id", sw.messageID, "error", err)
	}
}

func (sw *StreamWriter) updateMessageStatus(messageID, status string) error {
	err := sw.messageDao.UpdateMessageStatus(messageID, status)
	if err != nil {
		slog.Error("update message status failed", "message_id", messageID, "error", err)
	}
	return err
}

func (sw *StreamWriter) finish() {
	sw.cancel()
	sw.wg.Wait()

	// 关闭 hub 流 —— ServeStream 会向订阅者发送一个终结 DONE 事件。
	Hub.Close(sw.streamKey)

	// 为该 stream 设置 Redis EXPIRE
	rdb := pkgredis.GetClient()
	if rdb != nil {
		if err := expireStream(rdb, sw.streamKey); err != nil {
			slog.Warn("redis EXPIRE failed", "key", sw.streamKey, "error", err)
		}
	}

	registry.Delete(sw.originalMessageID)
}

// Fail 将 StreamWriter 对应的消息标记为失败（例如 context 被取消时）。
func (sw *StreamWriter) Fail() {
	sw.doFlush()
	sw.updateMessageStatus(sw.messageID, string(generated.MessageStatusFailed))
	if sw.messageID != sw.originalMessageID {
		sw.updateMessageStatus(sw.originalMessageID, string(generated.MessageStatusFailed))
	}

	// 关闭 hub 流，让订阅者收到 Done 事件。
	Hub.Close(sw.streamKey)

	// 为该 stream 设置 Redis EXPIRE。
	rdb := pkgredis.GetClient()
	if rdb != nil {
		if err := expireStream(rdb, sw.streamKey); err != nil {
			slog.Warn("redis EXPIRE failed in Fail", "key", sw.streamKey, "error", err)
		}
	}

	registry.Delete(sw.originalMessageID)
}

// PublishErrorAndFail 向 Redis Stream 和 hub 写入一个错误事件，然后把消息标记为失败。
// 用于 agentend 在流式传输前/中失败时，让前端能看到该错误。
func PublishErrorAndFail(messageDao dao.MessageDao, messageID, sessionID, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	PublishErrorAndFailWithContext(ctx, messageDao, messageID, sessionID, errMsg)
}

func expireStream(rdb *redis.Client, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	return rdb.Expire(ctx, key, streamExpireTTL).Err()
}

func PublishErrorAndFailWithContext(ctx context.Context, messageDao dao.MessageDao, messageID, sessionID, errMsg string) {
	key := pkgredis.StreamKey(sessionID, messageID)
	sseLine := formatErrorSSE(errMsg)

	// 热路径：立即把错误推送到 hub
	Hub.Publish(key, sseLine)

	// 冷路径：写入持久化 Redis
	rdb := pkgredis.GetClient()
	if rdb != nil {
		if _, err := rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: key,
			MaxLen: maxStreamLen,
			Approx: true,
			Values: map[string]interface{}{
				"data": sseLine,
			},
		}).Result(); err != nil {
			slog.Warn("failed to publish stream error to redis", "key", key, "error", err)
		}
		if err := rdb.Expire(ctx, key, streamExpireTTL).Err(); err != nil {
			slog.Warn("failed to set stream error ttl", "key", key, "error", err)
		}
	}
	if err := messageDao.UpdateMessageStatus(messageID, string(generated.MessageStatusFailed)); err != nil {
		slog.Warn("failed to mark message failed", "message_id", messageID, "error", err)
	}

	// 确保 hub 流被清理，让订阅者收到 Done 事件。
	Hub.Close(key)
}

func eventErrorMessage(content map[string]interface{}) string {
	if errMsg, ok := content["error"].(string); ok && errMsg != "" {
		return errMsg
	}
	if errMsg, ok := content["message"].(string); ok && errMsg != "" {
		return errMsg
	}
	return ""
}

func formatErrorSSE(errMsg string) string {
	event := map[string]interface{}{
		"type": "error",
		"content": map[string]string{
			"message": errMsg,
		},
	}
	data, _ := json.Marshal(event)
	return fmt.Sprintf("data: %s", string(data))
}

// CleanupStaleMessages 把所有 streaming 状态的消息标记为失败（启动时调用）。
func CleanupStaleMessages(messageDao dao.MessageDao) {
	rowsAffected, err := messageDao.FailStaleStreamingMessages()
	if err != nil {
		slog.Warn("failed to clean up stale streaming messages", "error", err)
		return
	}
	if rowsAffected > 0 {
		slog.Info("cleaned up stale streaming messages", "count", rowsAffected)
	}
}

// FormatSSE 将一段文本格式化为符合 StreamEvent 契约的 SSE data 行。
func FormatSSE(text string) string {
	event := map[string]interface{}{
		"type": "text",
		"content": map[string]string{
			"text": text,
		},
	}
	data, _ := json.Marshal(event)
	return fmt.Sprintf("data: %s", string(data))
}

// FormatSSEWithMeta 将一段文本格式化为带 agent 元信息的 SSE data 行。
func FormatSSEWithMeta(text, agentType, agentName, messageID, groupID string) string {
	content := map[string]string{
		"text": text,
	}
	if agentType != "" {
		content["agent_type"] = agentType
	}
	if agentName != "" {
		content["agent"] = agentName
	}
	if messageID != "" {
		content["message_id"] = messageID
	}
	if groupID != "" {
		content["group_id"] = groupID
	}
	event := map[string]interface{}{
		"type":    "text",
		"content": content,
	}
	data, _ := json.Marshal(event)
	return fmt.Sprintf("data: %s", string(data))
}

func legacyRuntimeBlockLine(blockType string, payload map[string]interface{}) string {
	data, _ := json.Marshal(payload)
	return fmt.Sprintf("\ntype: %s\njson: %s\n", blockType, string(data))
}

func legacyRuntimeBlockLineForEvent(event generated.StreamEvent) string {
	switch event.Type {
	case generated.EventTypePlanning:
		dispatch, ok := event.Content["dispatch"].(map[string]interface{})
		if !ok {
			return ""
		}
		payload := map[string]interface{}{
			"overview": "",
			"tasks": []map[string]interface{}{
				{
					"task_id": dispatch["task_id"],
					"agent":   dispatch["agent"],
					"title":   firstNonEmptyString(dispatch["title"], dispatch["content"]),
					"content": dispatch["content"],
					"status":  "pending",
				},
			},
		}
		return legacyRuntimeBlockLine("plan", payload)
	case generated.EventTypeRuntimeExecuting:
		payload := map[string]interface{}{
			"task_id": event.Content["task_id"],
			"agent":   event.Content["agent"],
			"title":   event.Content["title"],
			"status":  firstNonEmptyString(event.Content["status"], "running"),
		}
		return legacyRuntimeBlockLine("runtime_status", payload)
	case generated.EventTypeRuntimeCompleted:
		status := firstNonEmptyString(event.Content["status"], "")
		if status == "" {
			if success, ok := event.Content["success"].(bool); ok && success {
				status = "completed"
			} else {
				status = "failed"
			}
		}
		payload := map[string]interface{}{
			"task_id": event.Content["task_id"],
			"agent":   event.Content["agent"],
			"status":  status,
		}
		return legacyRuntimeBlockLine("runtime_status", payload)
	case generated.EventTypeCoordinationMessage:
		payload := map[string]interface{}{
			"messages": []map[string]interface{}{
				{
					"from":  event.Content["from"],
					"to":    event.Content["to"],
					"text":  event.Content["text"],
					"round": event.Content["round"],
				},
			},
			"closed": false,
		}
		return legacyRuntimeBlockLine("coordination", payload)
	case generated.EventTypeCoordinationDone:
		payload := map[string]interface{}{
			"messages": []map[string]interface{}{},
			"closed":   true,
			"summary":  coordinationSummary(event.Content),
		}
		return legacyRuntimeBlockLine("coordination", payload)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...interface{}) string {
	for _, value := range values {
		if str, ok := value.(string); ok && str != "" {
			return str
		}
	}
	return ""
}

func coordinationSummary(content map[string]interface{}) string {
	if summary := firstNonEmptyString(content["summary"]); summary != "" {
		return summary
	}
	rawDecisions, ok := content["decisions"].([]interface{})
	if !ok || len(rawDecisions) == 0 {
		return ""
	}
	decisions := make([]string, 0, len(rawDecisions))
	for _, raw := range rawDecisions {
		if decision, ok := raw.(string); ok && decision != "" {
			decisions = append(decisions, decision)
		}
	}
	return strings.Join(decisions, "\n")
}

func askCardStatus(raw interface{}) string {
	status, _ := raw.(string)
	if status == "completed" || status == "answered" {
		return "answered"
	}
	if status == "failed" {
		return "failed"
	}
	return "pending"
}
