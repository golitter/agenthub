package stream

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
)

func TestHub_ClosePreventsRecreation(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	key := "session:msg-close-test"

	// Publish to create stream
	Hub.Publish(key, "data: test")
	Hub.Close(key)

	// Publish again — should be silently dropped (no re-creation)
	Hub.Publish(key, "data: after-close")

	Hub.mu.RLock()
	_, exists := Hub.streams[key]
	_, closed := Hub.closedKeys[key]
	Hub.mu.RUnlock()

	if exists {
		t.Error("stream should not exist after Close")
	}
	if !closed {
		t.Error("key should be in closedKeys after Close")
	}
}

func TestPublishErrorAndFailWithContextPublishesAndClosesHub(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	messageDao := newWriterMessageDao()
	messageDao.messages["msg-1"] = &model.Message{MessageID: "msg-1", Status: "streaming"}
	key := "agent:session-1:msg-1"
	ch, _ := Hub.Subscribe(key)

	PublishErrorAndFailWithContext(context.Background(), messageDao, "msg-1", "session-1", "agent service unavailable")

	if messageDao.messages["msg-1"].Status != "failed" {
		t.Fatalf("message status = %q, want failed", messageDao.messages["msg-1"].Status)
	}

	select {
	case line, ok := <-ch:
		if !ok {
			t.Fatal("hub channel closed before error line was delivered")
		}
		if !strings.Contains(line, "agent service unavailable") {
			t.Fatalf("hub line = %q, want sanitized error", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hub error line")
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("hub channel should close after publishing stream error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hub close")
	}
}

func TestHub_SubscribeReturnsNilAfterClose(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	key := "session:msg-sub-nil"

	// Close without ever publishing — mark key as closed
	Hub.Close(key)

	ch, seq := Hub.Subscribe(key)
	if ch != nil {
		t.Error("expected nil channel when subscribing to a closed key")
	}
	if seq != 0 {
		t.Errorf("expected seq 0 for closed key, got %d", seq)
	}
}

func TestHub_UnsubscribeRemovesSubscriber(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	key := "session:msg-unsub"

	// Create stream and subscribe
	Hub.Publish(key, "data: init")
	ch, _ := Hub.Subscribe(key)

	// Verify subscriber exists
	Hub.mu.RLock()
	s := Hub.streams[key]
	Hub.mu.RUnlock()
	s.mu.Lock()
	count := len(s.subscribers)
	s.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 subscriber, got %d", count)
	}

	// Unsubscribe
	Hub.Unsubscribe(key, ch)

	// Verify subscriber removed
	s.mu.Lock()
	count = len(s.subscribers)
	s.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 subscribers after Unsubscribe, got %d", count)
	}
}

func TestHub_UnsubscribeOnNonexistentStream(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	// Should not panic
	ch := make(chan HubEvent, 10)
	Hub.Unsubscribe("nonexistent:key", ch)
}

func TestHub_PublishDropOnClosedStream(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	key := "session:msg-drop"

	// Create and close
	Hub.Publish(key, "data: before")
	Hub.Close(key)

	// This should not create a new stream
	Hub.Publish(key, "data: after-close")

	Hub.mu.RLock()
	_, exists := Hub.streams[key]
	Hub.mu.RUnlock()
	if exists {
		t.Error("stream should not be recreated after Close")
	}
}

func TestHub_StartClosedKeysCleanup(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	// Add some closed keys
	Hub.Close("key1")
	Hub.Close("key2")

	Hub.mu.RLock()
	count := len(Hub.closedKeys)
	Hub.mu.RUnlock()
	if count != 2 {
		t.Fatalf("expected 2 closedKeys, got %d", count)
	}

	// Start cleanup (runs every 10min in prod, but we just test it doesn't panic)
	// We won't wait 10 minutes; just verify it starts without error
	done := make(chan struct{})
	go func() {
		Hub.StartClosedKeysCleanup()
		close(done)
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	Hub.mu.RLock()
	count = len(Hub.closedKeys)
	Hub.mu.RUnlock()
	// Keys should still exist (cleanup hasn't run yet — 10 min interval)
	if count != 2 {
		t.Logf("closedKeys count = %d (cleanup may have run)", count)
	}
}

func TestLegacyRuntimeBlockLineForEventPersistsRuntimeStatus(t *testing.T) {
	got := legacyRuntimeBlockLineForEvent(generated.StreamEvent{
		Type: generated.EventTypeRuntimeExecuting,
		Content: map[string]interface{}{
			"task_id": "task-001",
			"agent":   "worker",
			"title":   "Inspect refresh hydration",
			"status":  "running",
		},
	})

	for _, want := range []string{
		"type: runtime_status",
		`"task_id":"task-001"`,
		`"agent":"worker"`,
		`"title":"Inspect refresh hydration"`,
		`"status":"running"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("legacyRuntimeBlockLineForEvent() = %q, want substring %q", got, want)
		}
	}
}

func TestLegacyRuntimeBlockLineForEventSkipsRuntimeText(t *testing.T) {
	got := legacyRuntimeBlockLineForEvent(generated.StreamEvent{
		Type: generated.EventTypeRuntimeText,
		Content: map[string]interface{}{
			"task_id": "task-001",
			"agent":   "worker",
			"text":    "transient child output",
		},
	})
	if got != "" {
		t.Fatalf("runtime_text should stay transient, got %q", got)
	}
}

func TestStreamWriterPersistsForwardedCrossSessionTextAsSingleLocalSubMessage(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	const (
		taskID            = "task-forward"
		orchestratorID    = "orch-session"
		originalMessageID = "orch-message"
		childMessageID    = "child-message"
	)
	messageDao := newWriterMessageDao()
	messageDao.messages[originalMessageID] = &model.Message{
		MessageID: originalMessageID,
		TaskID:    taskID,
		SessionID: orchestratorID,
		Role:      "agent",
		Status:    "streaming",
		AgentType: "orchestrator",
		AgentName: "manager",
	}
	messageDao.sourceSessions[childMessageID] = "child-session"

	sw := NewStreamWriter(
		context.Background(),
		taskID,
		orchestratorID,
		originalMessageID,
		"orchestrator",
		messageDao,
		&writerSessionDao{},
		&writerDiffSnapshotDao{},
	)

	outcome := sw.Run(func(fn func(line string)) error {
		for _, event := range []generated.StreamEvent{
			{
				Type: generated.EventTypeAskCardStart,
				Content: map[string]interface{}{
					"question_id":       "q-forward",
					"source_agent":      "manager",
					"source_agent_type": "orchestrator",
					"source_session_id": orchestratorID,
					"target_agent":      "worker",
					"target_agent_type": "claude-code",
					"target_session_id": "child-session",
					"question":          "introduce yourself",
				},
			},
			{
				Type: generated.EventTypeText,
				Content: map[string]interface{}{
					"text":       "hello ",
					"agent":      "worker",
					"agent_type": "claude-code",
					"message_id": childMessageID,
				},
			},
			{
				Type: generated.EventTypeText,
				Content: map[string]interface{}{
					"text":       "world",
					"agent":      "worker",
					"agent_type": "claude-code",
					"message_id": childMessageID,
				},
			},
			{
				Type: generated.EventTypeAskCardDone,
				Content: map[string]interface{}{
					"question_id":       "q-forward",
					"target_agent":      "worker",
					"target_agent_type": "claude-code",
					"target_session_id": "child-session",
					"summary":           "hello world",
					"status":            "completed",
				},
			},
			{Type: generated.EventTypeDone},
		} {
			fn(formatTestSSE(event))
		}
		return nil
	})
	if outcome != RunOutcomeCompleted {
		t.Fatalf("Run() outcome = %q, want %q", outcome, RunOutcomeCompleted)
	}

	var workerMessages []model.Message
	for _, message := range messageDao.messages {
		if message.SessionID == orchestratorID && message.AgentType == "claude-code" {
			workerMessages = append(workerMessages, *message)
		}
	}
	if len(workerMessages) != 1 {
		t.Fatalf("created worker messages = %d, want 1: %#v", len(workerMessages), workerMessages)
	}
	if workerMessages[0].Content != "hello world" {
		t.Fatalf("worker content = %q, want %q", workerMessages[0].Content, "hello world")
	}
	original := messageDao.messages[originalMessageID]
	if original == nil || !strings.Contains(original.Content, `"status":"answered"`) {
		t.Fatalf("original message should contain answered ask card marker, got %#v", original)
	}
}

func TestStreamWriterPersistsErrorEventAsFailedMessage(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	const (
		taskID    = "task-error"
		sessionID = "orch-session"
		messageID = "orch-message-error"
	)
	messageDao := newWriterMessageDao()
	messageDao.messages[messageID] = &model.Message{
		MessageID: messageID,
		TaskID:    taskID,
		SessionID: sessionID,
		Role:      "agent",
		Status:    "streaming",
		AgentType: "orchestrator",
		AgentName: "manager",
	}

	sw := NewStreamWriter(
		context.Background(),
		taskID,
		sessionID,
		messageID,
		"orchestrator",
		messageDao,
		&writerSessionDao{},
		&writerDiffSnapshotDao{},
	)

	outcome := sw.Run(func(fn func(line string)) error {
		fn(formatTestSSE(generated.StreamEvent{
			Type: generated.EventTypeError,
			Content: map[string]interface{}{
				"error": "Orchestrator 推理失败：APIConnectionError: Connection error.",
			},
		}))
		fn(formatTestSSE(generated.StreamEvent{Type: generated.EventTypeDone}))
		return nil
	})

	if outcome != RunOutcomeFailed {
		t.Fatalf("Run() outcome = %q, want %q", outcome, RunOutcomeFailed)
	}
	message := messageDao.messages[messageID]
	if message == nil {
		t.Fatalf("message not found")
	}
	if message.Status != string(RunOutcomeFailed) {
		t.Fatalf("message status = %q, want %q", message.Status, RunOutcomeFailed)
	}
	if !strings.Contains(message.Content, "Orchestrator 推理失败") {
		t.Fatalf("message content = %q, want visible error", message.Content)
	}
}

func formatTestSSE(event generated.StreamEvent) string {
	data, _ := json.Marshal(event)
	return "data: " + string(data)
}

type writerMessageDao struct {
	messages       map[string]*model.Message
	sourceSessions map[string]string
}

func newWriterMessageDao() *writerMessageDao {
	return &writerMessageDao{
		messages:       make(map[string]*model.Message),
		sourceSessions: make(map[string]string),
	}
}

func (dao *writerMessageDao) ListByTask(string, string, string, string, int, *uint64) ([]model.Message, error) {
	return nil, nil
}

func (dao *writerMessageDao) CountBySessionID(string) (int64, error) { return 0, nil }

func (dao *writerMessageDao) FindByMessageID(messageID string) (*model.Message, error) {
	return dao.messages[messageID], nil
}

func (dao *writerMessageDao) CreateMessage(message model.Message) error {
	copyMessage := message
	dao.messages[message.MessageID] = &copyMessage
	return nil
}

func (dao *writerMessageDao) FindSessionIDByTaskMessage(_, messageID string) (string, error) {
	return dao.sourceSessions[messageID], nil
}

func (dao *writerMessageDao) FindMessageContent(messageID string) (string, error) {
	if message := dao.messages[messageID]; message != nil {
		return message.Content, nil
	}
	return "", nil
}

func (dao *writerMessageDao) UpdateMessageContentAndSeq(messageID, content, seq string) error {
	if message := dao.messages[messageID]; message != nil {
		message.Content = content
		message.LastSeq = seq
	}
	return nil
}

func (dao *writerMessageDao) UpdateMessageStatus(messageID, status string) error {
	if message := dao.messages[messageID]; message != nil {
		message.Status = status
	}
	return nil
}

func (dao *writerMessageDao) FailStaleStreamingMessages() (int64, error) { return 0, nil }

func (dao *writerMessageDao) FindLatestCompletedAgentMessage(string, string) (*model.Message, error) {
	return nil, nil
}

func (dao *writerMessageDao) ListGroupChatWindowMessages(string, string, *model.Message) ([]model.Message, error) {
	return nil, nil
}

func (dao *writerMessageDao) FindLatestPlanReviewMessage(string, string) (*model.Message, error) {
	return nil, nil
}

func (dao *writerMessageDao) UpdateContent(messageID, content string) error {
	if message := dao.messages[messageID]; message != nil {
		message.Content = content
	}
	return nil
}

type writerSessionDao struct{}

func (dao *writerSessionDao) DeactivateSession(string) (bool, error)        { return false, nil }
func (dao *writerSessionDao) GetBySessionID(string) (*model.Session, error) { return nil, nil }
func (dao *writerSessionDao) GetByTaskAndSessionID(string, string) (*model.Session, error) {
	return nil, nil
}
func (dao *writerSessionDao) ListByTaskID(string) ([]model.Session, error)     { return nil, nil }
func (dao *writerSessionDao) ListAll() ([]model.Session, error)                { return nil, nil }
func (dao *writerSessionDao) FindPrimaryGroupSessionID(string) (string, error) { return "", nil }
func (dao *writerSessionDao) UpdateFields(string, map[string]interface{}) (bool, error) {
	return false, nil
}
func (dao *writerSessionDao) UpdateSoul(string, string) (bool, error)         { return false, nil }
func (dao *writerSessionDao) UpdateStatusByTask(string, string, string) error { return nil }

type writerDiffSnapshotDao struct{}

func (dao *writerDiffSnapshotDao) GetBySnapshotID(string) (*model.DiffSnapshot, error) {
	return nil, nil
}
func (dao *writerDiffSnapshotDao) CancelPendingBySession(string, string) error { return nil }
func (dao *writerDiffSnapshotDao) Upsert(snapshot model.DiffSnapshot) (*model.DiffSnapshot, error) {
	return &snapshot, nil
}
func (dao *writerDiffSnapshotDao) UpsertPending(string, string, string) error { return nil }
