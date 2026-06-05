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
)

// RunOutcome is the terminal result of consuming an agentend SSE stream.
type RunOutcome string

const (
	RunOutcomeCompleted RunOutcome = "completed"
	RunOutcomeFailed    RunOutcome = "failed"
)

// Registry tracks active StreamWriter goroutines by messageID.
var registry sync.Map

// IsActive returns true if a goroutine is running for the given messageID.
func IsActive(messageID string) bool {
	_, ok := registry.Load(messageID)
	return ok
}

// StreamWriter consumes an agentend SSE stream, publishes events to Redis Stream,
// and batch-flushes text content to MySQL.
// When agent_type changes in SSE events, it finalizes the current Message and creates
// a new one under the same session, keeping the original Message in streaming status
// until the entire round finishes.
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
	groupID           string // current orchestration group for the active message
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

	textBuf      []string // buffered text snippets for TEXT events awaiting merge
	textBufSize  int      // total byte size of textBuf
	textBufStart time.Time

	messageDao      dao.MessageDao
	sessionDao      dao.SessionDao
	diffSnapshotDao dao.DiffSnapshotDao
}

// NewStreamWriter creates a new StreamWriter and registers it.
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

// Run consumes the agentend response body (SSE lines), publishes to Redis, and flushes to MySQL.
// This should be called in a goroutine. It returns the terminal outcome so the
// caller can keep Session status consistent with Message status.
func (sw *StreamWriter) Run(scanFunc func(func(line string)) error) RunOutcome {
	defer sw.finish()

	sw.wg.Add(1)
	go sw.flushLoop()

	sawError := false

	scanErr := scanFunc(func(line string) {
		if sw.ctx.Err() != nil {
			return
		}

		// Parse SSE data lines for event-type routing
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
							sw.flushTextBuffer()
							sw.publishForwardedText(text, newAgentType, newAgentName, sourceMessageID)
							return
						}

						if sw.shouldSplitAfterForward() {
							sw.flushTextBuffer()
							sw.switchAgent(newAgentType, newAgentName, sourceMessageID, "", "")
						} else if newAgentType != sw.currentAgentType ||
							(sourceMessageID != "" && sourceMessageID != sw.currentSourceID) {
							// Check for agent switch or upstream message boundary switch.
							sw.flushTextBuffer()
							sw.switchAgent(newAgentType, newAgentName, sourceMessageID, "", "")
						} else if newName, ok := event.Content["agent"].(string); ok && newName != "" {
							// Same agentType but name provided — update tracking so
							// flushTextBuffer emits correct metadata (e.g. first
							// Orchestrator TEXT after stream starts).
							sw.mu.Lock()
							sw.currentAgentName = newName
							if sourceMessageID != "" {
								sw.currentSourceID = sourceMessageID
							}
							sw.groupID = ""
							sw.mu.Unlock()
						}
						sw.appendText(text)
						// Buffer TEXT event for batched Redis publish
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
					_ = sw.sessionDao.UpdateStatusByTask(sw.sessionID, sw.taskID, "awaiting_review")
				default:
					// runtime_text, tool_call, tool_result, etc. — flush text buffer first
					sw.flushTextBuffer()
				}
			}
		} else {
			// Non-data lines (e.g. "event: ..." lines) — flush text buffer
			sw.flushTextBuffer()
		}
		// Publish non-TEXT lines immediately
		sw.publishToRedis(line)
	})
	if scanErr != nil {
		sawError = true
		sw.flushTextBuffer()
		errMsg := fmt.Sprintf("stream read error: %v", scanErr)
		sw.appendText("[Error] " + errMsg)
		sw.publishToRedis(formatErrorSSE(errMsg))
	}

	// Final flush
	sw.flushTextBuffer()
	sw.doFlush()

	outcome := RunOutcomeCompleted
	if sawError {
		outcome = RunOutcomeFailed
	}
	// Finalize the current (last) sub-message
	sw.updateMessageStatus(sw.messageID, string(outcome))
	// Finalize the original message (may be the same if no agent switch happened)
	if sw.messageID != sw.originalMessageID {
		sw.updateMessageStatus(sw.originalMessageID, string(outcome))
	}
	return outcome
}

// switchAgent handles agent/message transitions: flushes buffer, finalizes current Message,
// and creates a new Message under the same session when the speaker or upstream message changes.
func (sw *StreamWriter) switchAgent(newAgentType, newAgentName, sourceMessageID, groupID, targetMessageID string) {
	sw.mu.Lock()
	hasContent := sw.bufLen > 0
	currentMessageID := sw.messageID
	sw.mu.Unlock()

	if hasContent {
		// Flush current buffer to the current Message
		sw.doFlush()

		// Finalize current sub-message. The original message remains streaming
		// until the full round finishes so late SSE subscribers do not receive
		// an early done while child-agent output is still being produced.
		if currentMessageID != sw.originalMessageID && currentMessageID != targetMessageID {
			sw.updateMessageStatus(currentMessageID, "completed")
		}
	}

	if targetMessageID != "" {
		sw.seedBufferFromMessage(targetMessageID, groupID)
	} else if hasContent {
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

	// Always update agent tracking
	sw.mu.Lock()
	sw.currentAgentType = newAgentType
	sw.currentAgentName = newAgentName
	sw.currentSourceID = sourceMessageID
	sw.groupID = groupID
	sw.mu.Unlock()
}

func (sw *StreamWriter) createSubMessage(agentType, agentName, groupID string) string {
	newMsgID := uuid.New().String()
	newMsg := model.Message{
		MessageID: newMsgID,
		TaskID:    sw.taskID,
		SessionID: sw.sessionID,
		Role:      "agent",
		Content:   "",
		Status:    "streaming",
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

func (sw *StreamWriter) publishForwardedText(text, agentType, agentName, messageID string) {
	sw.publishToRedis(FormatSSEWithMeta(text, agentType, agentName, messageID, ""))
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
	// Hot path: immediate push to in-memory hub for low-latency SSE delivery
	if strings.HasPrefix(line, "data: ") {
		Hub.Publish(sw.streamKey, line)
	}

	// Cold path: durable Redis Stream for replay/reconnect
	sw.publishToRedisOnly(line)
}

// publishToRedisOnly writes to Redis Stream without hub (for merged batch events
// that were already individually pushed to hub via bufferTextLine).
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

// bufferTextLine publishes an enriched TEXT event to the hub immediately, and buffers the
// plain text for deferred merged Redis publish.
func (sw *StreamWriter) bufferTextLine(text string) {
	sw.mu.Lock()
	agentType := sw.currentAgentType
	agentName := sw.currentAgentName
	currentMessageID := sw.messageID
	groupID := sw.groupID
	sw.mu.Unlock()

	// Hot path: immediate push to hub (no batching)
	Hub.Publish(sw.streamKey, FormatSSEWithMeta(text, agentType, agentName, currentMessageID, groupID))

	// Cold path: buffer text for batched Redis publish (avoids double JSON parse)
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

// flushTextBuffer merges buffered TEXT texts into a single SSE line and publishes to Redis.
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
		// Cold path only: merged batch to Redis (hub already got individual events)
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

	// Close hub stream — ServeStream emits one terminal DONE to subscribers.
	Hub.Close(sw.streamKey)

	// Set Redis EXPIRE on the stream
	rdb := pkgredis.GetClient()
	if rdb != nil {
		if err := rdb.Expire(context.Background(), sw.streamKey, streamExpireTTL).Err(); err != nil {
			slog.Warn("redis EXPIRE failed", "key", sw.streamKey, "error", err)
		}
	}

	registry.Delete(sw.originalMessageID)
}

// Fail marks a StreamWriter's message as failed (e.g., on context cancellation).
func (sw *StreamWriter) Fail() {
	sw.doFlush()
	sw.updateMessageStatus(sw.messageID, "failed")
	if sw.messageID != sw.originalMessageID {
		sw.updateMessageStatus(sw.originalMessageID, "failed")
	}

	// Close hub stream so subscribers receive Done event.
	Hub.Close(sw.streamKey)

	// Set Redis EXPIRE on the stream.
	rdb := pkgredis.GetClient()
	if rdb != nil {
		if err := rdb.Expire(context.Background(), sw.streamKey, streamExpireTTL).Err(); err != nil {
			slog.Warn("redis EXPIRE failed in Fail", "key", sw.streamKey, "error", err)
		}
	}

	registry.Delete(sw.originalMessageID)
}

// PublishErrorAndFail writes an error event to Redis Stream and hub, then marks the message as failed.
// Used when agentend fails before/during streaming so the frontend can see the error.
func PublishErrorAndFail(messageDao dao.MessageDao, messageID, sessionID, errMsg string) {
	key := pkgredis.StreamKey(sessionID, messageID)
	sseLine := formatErrorSSE(errMsg)

	// Hot path: push error to hub immediately
	Hub.Publish(key, sseLine)

	// Cold path: durable Redis
	rdb := pkgredis.GetClient()
	if rdb != nil {
		rdb.XAdd(context.Background(), &redis.XAddArgs{
			Stream: key,
			MaxLen: maxStreamLen,
			Approx: true,
			Values: map[string]interface{}{
				"data": sseLine,
			},
		}).Result()
		rdb.Expire(context.Background(), key, streamExpireTTL)
	}
	if err := messageDao.UpdateMessageStatus(messageID, "failed"); err != nil {
		slog.Warn("failed to mark message failed", "message_id", messageID, "error", err)
	}

	// Ensure hub stream is cleaned up so subscribers receive Done event.
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

// CleanupStaleMessages marks all streaming messages as failed (called at startup).
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

// FormatSSE formats a text chunk as an SSE data line matching the StreamEvent contract.
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

// FormatSSEWithMeta formats a text chunk as an SSE data line with agent metadata.
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
