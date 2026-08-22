package stream

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	pkgredis "agenthub/backend/pkg/redis"
)

func TestHub_ClosePreventsRecreation(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	key := "session:msg-close-test"

	// 先发布以创建流
	Hub.Publish(key, "data: test")
	Hub.Close(key)

	// 再次发布 —— 应被静默丢弃（不会重新创建）
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
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("hub channel closed before error line was delivered")
		}
		if !strings.Contains(evt.Data, "agent service unavailable") {
			t.Fatalf("hub line = %q, want sanitized error", evt.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hub error line")
	}

	select {
	case evt, ok := <-ch:
		if !ok {
			return
		}
		if !evt.Done {
			t.Fatalf("hub event after error = %#v, want Done sentinel or closed channel", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hub done sentinel")
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("hub channel should close after done sentinel")
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

	// 从未发布就直接关闭 —— 将该 key 标记为已关闭
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

	// 创建流并订阅
	Hub.Publish(key, "data: init")
	ch, _ := Hub.Subscribe(key)

	// 验证订阅者存在
	Hub.mu.RLock()
	s := Hub.streams[key]
	Hub.mu.RUnlock()
	s.mu.Lock()
	count := len(s.subscribers)
	s.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 subscriber, got %d", count)
	}

	// 取消订阅
	Hub.Unsubscribe(key, ch)

	// 验证订阅者已移除
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

	// 不应 panic
	ch := make(chan HubEvent, 10)
	Hub.Unsubscribe("nonexistent:key", ch)
}

func TestHub_PublishDropOnClosedStream(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	key := "session:msg-drop"

	// 创建并关闭
	Hub.Publish(key, "data: before")
	Hub.Close(key)

	// 这不应创建新流
	Hub.Publish(key, "data: after-close")

	Hub.mu.RLock()
	_, exists := Hub.streams[key]
	Hub.mu.RUnlock()
	if exists {
		t.Error("stream should not be recreated after Close")
	}
}

func TestHub_StartClosedKeysCleanup(t *testing.T) {
	h := &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	// 添加一些已关闭的 key
	h.Close("key1")
	h.Close("key2")

	h.mu.RLock()
	count := len(h.closedKeys)
	h.mu.RUnlock()
	if count != 2 {
		t.Fatalf("expected 2 closedKeys, got %d", count)
	}

	// 启动清理（生产环境每 10 分钟跑一次，这里只验证它不会 panic）
	// 不会真的等 10 分钟，只验证它能正常启动
	done := make(chan struct{})
	go func() {
		h.StartClosedKeysCleanup()
		close(done)
	}()

	// 给它一点时间启动
	time.Sleep(50 * time.Millisecond)

	h.mu.RLock()
	count = len(h.closedKeys)
	h.mu.RUnlock()
	// 这些 key 应仍存在（清理尚未运行 —— 间隔为 10 分钟）
	if count != 2 {
		t.Logf("closedKeys count = %d (cleanup may have run)", count)
	}
}

func TestLegacyRuntimeBlockLineForEventPersistsRuntimeStatus(t *testing.T) {
	got := legacyRuntimeBlockLineForEvent(generated.StreamEvent{
		Type: generated.EventTypeRuntimeExecuting,
		Content: map[string]interface{}{
			"task_id":                  "root-scope",
			"plan_task_id":             "task-001",
			"run_id":                   "run-001",
			"integration_operation_id": "operation-001",
			"attempt":                  2,
			"agent":                    "worker",
			"title":                    "Inspect refresh hydration",
			"status":                   "running",
			"target_branch":            "task/root-scope",
		},
	})

	for _, want := range []string{
		"type: runtime_status",
		`"task_id":"root-scope"`,
		`"plan_task_id":"task-001"`,
		`"run_id":"run-001"`,
		`"integration_operation_id":"operation-001"`,
		`"attempt":2`,
		`"agent":"worker"`,
		`"title":"Inspect refresh hydration"`,
		`"status":"running"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("legacyRuntimeBlockLineForEvent() = %q, want substring %q", got, want)
		}
	}
	if strings.Contains(got, "target_branch") {
		t.Fatalf("ordinary runtime projection leaked Git branch: %q", got)
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

func TestSanitizeOrdinaryStreamEventRemovesGitAuditFacts(t *testing.T) {
	event := sanitizeOrdinaryStreamEvent(generated.StreamEvent{
		Type: generated.EventTypePlanReview,
		Content: map[string]interface{}{
			"plan_task_id":   "task-001",
			"source_branch":  "task/task-001",
			"target_branch":  "main",
			"source_commit":  "source-sha",
			"target_commit":  "target-sha",
			"merge_base":     "base-sha",
			"workspace_path": "/tmp/secret-worktree",
		},
	})
	if _, ok := event.Content["plan_task_id"]; !ok {
		t.Fatal("ordinary identity was removed")
	}
	for _, key := range []string{
		"source_branch",
		"target_branch",
		"source_commit",
		"target_commit",
		"merge_base",
		"workspace_path",
	} {
		if _, ok := event.Content[key]; ok {
			t.Fatalf("ordinary SSE retained Git fact %q", key)
		}
	}
}

func TestSanitizeOrdinaryStreamEventRemovesNestedGitAuditFacts(t *testing.T) {
	event := sanitizeOrdinaryStreamEvent(generated.StreamEvent{
		Type: generated.EventTypePlanning,
		Content: map[string]interface{}{
			"node": "dispatch",
			"dispatch": map[string]interface{}{
				"task_id":              "task-001",
				"workspace_path":       "/tmp/secret-worktree",
				"workspace_handle":     "opaque-workspace",
				"integration_scope_id": "scope-1",
				"source_branch":        "agent/session/scope-1",
			},
		},
	})

	dispatch, ok := event.Content["dispatch"].(map[string]interface{})
	if !ok {
		t.Fatalf("dispatch type = %T", event.Content["dispatch"])
	}
	if dispatch["task_id"] != "task-001" {
		t.Fatal("ordinary identity was removed from nested dispatch")
	}
	for _, key := range []string{"workspace_path", "workspace_handle", "integration_scope_id", "source_branch"} {
		if _, ok := dispatch[key]; ok {
			t.Fatalf("nested ordinary SSE retained Git/workspace fact %q", key)
		}
	}
}

func TestSanitizeOrdinaryStreamEventBoundsConflictPaths(t *testing.T) {
	event := sanitizeOrdinaryStreamEvent(generated.StreamEvent{
		Type: generated.EventTypeIntegrationConflict,
		Content: map[string]interface{}{
			"conflict_files": []interface{}{"src/main.go", "/tmp/secret", "../escape", "ok.txt", "a\\b"},
		},
	})
	files, ok := event.Content["conflict_files"].([]string)
	if !ok {
		t.Fatalf("conflict_files type = %T", event.Content["conflict_files"])
	}
	if strings.Join(files, ",") != "src/main.go,ok.txt" {
		t.Fatalf("safe conflict files = %#v", files)
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

func TestStreamWriterSeparatesGroupedRunsAndKeepsRuntimeOnRootMessage(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	const (
		taskID            = "task-grouped-runs"
		orchestratorID    = "orch-session"
		originalMessageID = "orch-message"
		groupID           = "orch-group"
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
				Type: generated.EventTypeText,
				Content: map[string]interface{}{
					"text":       "first run",
					"agent":      "worker",
					"agent_type": "codex",
					"message_id": "child-message-1",
					"group_id":   groupID,
				},
			},
			{
				Type: generated.EventTypeRuntimeCompleted,
				Content: map[string]interface{}{
					"task_id": "task-001",
					"agent":   "worker",
					"success": true,
					"status":  "completed",
				},
			},
			{
				Type: generated.EventTypeText,
				Content: map[string]interface{}{
					"text":       "second run",
					"agent":      "worker",
					"agent_type": "codex",
					"message_id": "child-message-2",
					"group_id":   groupID,
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
		if message.SessionID == orchestratorID && message.AgentType == "codex" {
			workerMessages = append(workerMessages, *message)
		}
	}
	if len(workerMessages) != 2 {
		t.Fatalf("created worker messages = %d, want 2: %#v", len(workerMessages), workerMessages)
	}
	contents := map[string]bool{}
	workerMessageIDs := map[string]bool{}
	for _, message := range workerMessages {
		contents[message.Content] = true
		workerMessageIDs[message.MessageID] = true
		if strings.Contains(message.Content, "runtime_status") {
			t.Fatalf("runtime marker leaked into worker message: %q", message.Content)
		}
	}
	if !contents["first run"] || !contents["second run"] {
		t.Fatalf("worker contents = %#v", contents)
	}
	root := messageDao.messages[originalMessageID]
	if root == nil || !strings.Contains(root.Content, "runtime_status") {
		t.Fatalf("root message should contain runtime marker, got %#v", root)
	}

	runtimeEvent := generated.StreamEvent{
		Type: generated.EventTypeRuntimeCompleted,
		Content: map[string]interface{}{
			"group_id":   groupID,
			"message_id": "child-message-2",
		},
	}
	sw.projectGroupedRuntimeMessageID(&runtimeEvent)
	projectedMessageID, _ := runtimeEvent.Content["message_id"].(string)
	if !workerMessageIDs[projectedMessageID] {
		t.Fatalf("runtime message_id = %q, want a grouped mirror ID", projectedMessageID)
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
				"error":              "Orchestrator 推理失败：APIConnectionError: Connection error.",
				"termination_reason": "wall_time_exceeded",
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
	if message.TerminationReason != "wall_time_exceeded" {
		t.Fatalf("termination reason = %q, want wall_time_exceeded", message.TerminationReason)
	}
	if !strings.Contains(message.Content, "Orchestrator 推理失败") {
		t.Fatalf("message content = %q, want visible error", message.Content)
	}
}

func TestStreamWriterReturnsAwaitingReviewWhenPlanReviewEndsPending(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	const (
		taskID    = "task-review"
		sessionID = "orch-session"
		messageID = "orch-message-review"
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
	sessionDao := &writerSessionDao{}

	sw := NewStreamWriter(
		context.Background(),
		taskID,
		sessionID,
		messageID,
		"orchestrator",
		messageDao,
		sessionDao,
		&writerDiffSnapshotDao{},
	)

	outcome := sw.Run(func(fn func(line string)) error {
		fn(formatTestSSE(generated.StreamEvent{
			Type: generated.EventTypePlanReview,
			Content: map[string]interface{}{
				"session_id": sessionID,
				"task_id":    taskID,
				"review_key": "review-1",
				"plan": map[string]interface{}{
					"overview": "please review",
				},
			},
		}))
		fn(formatTestSSE(generated.StreamEvent{Type: generated.EventTypeDone}))
		return nil
	})

	if outcome != RunOutcomeAwaitingReview {
		t.Fatalf("Run() outcome = %q, want %q", outcome, RunOutcomeAwaitingReview)
	}
	if messageDao.messages[messageID].Status != string(generated.MessageStatusCompleted) {
		t.Fatalf("message status = %q, want completed", messageDao.messages[messageID].Status)
	}
	if len(sessionDao.statuses) != 1 || sessionDao.statuses[0] != string(generated.SessionStateAwaitingReview) {
		t.Fatalf("session status updates = %#v, want awaiting_review", sessionDao.statuses)
	}
}

func TestStreamWriterCompletesWhenStreamContinuesAfterPlanReview(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	const (
		taskID    = "task-review-resumed"
		sessionID = "orch-session"
		messageID = "orch-message-review-resumed"
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
			Type: generated.EventTypePlanReview,
			Content: map[string]interface{}{
				"session_id": sessionID,
				"task_id":    taskID,
				"review_key": "review-1",
				"plan": map[string]interface{}{
					"overview": "please review",
				},
			},
		}))
		fn(formatTestSSE(generated.StreamEvent{
			Type: generated.EventTypeText,
			Content: map[string]interface{}{
				"text":       "resumed",
				"agent":      "manager",
				"agent_type": "orchestrator",
			},
		}))
		fn(formatTestSSE(generated.StreamEvent{Type: generated.EventTypeDone}))
		return nil
	})

	if outcome != RunOutcomeCompleted {
		t.Fatalf("Run() outcome = %q, want %q", outcome, RunOutcomeCompleted)
	}
}

func TestStreamWriterPausesForResolutionWithoutClosingRootStream(t *testing.T) {
	Hub = &RuntimeHub{
		streams:    make(map[string]*RuntimeStream),
		closedKeys: make(map[string]struct{}),
	}

	const (
		taskID    = "task-resolution-pause"
		sessionID = "orch-resolution-pause"
		messageID = "orch-message-resolution-pause"
	)
	messageDao := newWriterMessageDao()
	messageDao.messages[messageID] = &model.Message{
		MessageID: messageID,
		TaskID:    taskID,
		SessionID: sessionID,
		Role:      "agent",
		Status:    "streaming",
		AgentType: "orchestrator",
	}
	sessionDao := &writerSessionDao{}
	sw := NewStreamWriter(
		context.Background(),
		taskID,
		sessionID,
		messageID,
		"orchestrator",
		messageDao,
		sessionDao,
		&writerDiffSnapshotDao{},
	)

	outcome := sw.Run(func(fn func(line string)) error {
		fn(formatTestSSE(generated.StreamEvent{
			Type: generated.EventTypeResolutionFailed,
			Content: map[string]interface{}{
				"task_id": "task-child",
				"status":  "awaiting_user",
			},
		}))
		fn(formatTestSSE(generated.StreamEvent{
			Type: generated.EventTypeOrchestratorPaused,
			Content: map[string]interface{}{
				"task_id": taskID,
				"status":  "awaiting_user",
			},
		}))
		return nil
	})

	if outcome != RunOutcomeAwaitingResolution {
		t.Fatalf("Run() outcome = %q, want %q", outcome, RunOutcomeAwaitingResolution)
	}
	if messageDao.messages[messageID].Status != string(generated.MessageStatusStreaming) {
		t.Fatalf("message status = %q, want streaming", messageDao.messages[messageID].Status)
	}
	if len(sessionDao.statuses) == 0 || sessionDao.statuses[len(sessionDao.statuses)-1] != string(generated.SessionStateAwaitingResolution) {
		t.Fatalf("session status updates = %#v, want awaiting_resolution", sessionDao.statuses)
	}

	key := pkgredis.StreamKey(sessionID, messageID)
	ch, _ := Hub.Subscribe(key)
	select {
	case event := <-ch:
		if event.Done {
			t.Fatal("paused stream emitted synthetic Done")
		}
	case <-time.After(50 * time.Millisecond):
		// No queued event is also acceptable; importantly, the hub remains open.
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

func (dao *writerMessageDao) UpdateMessageRunState(messageID, status, terminationReason string) error {
	if message := dao.messages[messageID]; message != nil {
		message.Status = status
		message.TerminationReason = terminationReason
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

type writerSessionDao struct {
	session  *model.Session
	statuses []string
}

func (dao *writerSessionDao) DeactivateSession(string) (bool, error)        { return false, nil }
func (dao *writerSessionDao) GetBySessionID(string) (*model.Session, error) { return dao.session, nil }
func (dao *writerSessionDao) GetByTaskAndSessionID(string, string) (*model.Session, error) {
	return dao.session, nil
}
func (dao *writerSessionDao) ListByTaskID(string) ([]model.Session, error)     { return nil, nil }
func (dao *writerSessionDao) ListAll() ([]model.Session, error)                { return nil, nil }
func (dao *writerSessionDao) FindPrimaryGroupSessionID(string) (string, error) { return "", nil }
func (dao *writerSessionDao) UpdateFields(string, map[string]interface{}) (bool, error) {
	return false, nil
}
func (dao *writerSessionDao) UpdateSoul(string, string) (bool, error) { return false, nil }
func (dao *writerSessionDao) UpdateStatusByTask(_, _, status string) error {
	dao.statuses = append(dao.statuses, status)
	if dao.session != nil {
		dao.session.Status = status
	}
	return nil
}

type writerDiffSnapshotDao struct{}

func (dao *writerDiffSnapshotDao) GetBySnapshotID(string) (*model.DiffSnapshot, error) {
	return nil, nil
}
func (dao *writerDiffSnapshotDao) CancelPendingBySession(string, string) error { return nil }
func (dao *writerDiffSnapshotDao) Upsert(snapshot model.DiffSnapshot) (*model.DiffSnapshot, error) {
	return &snapshot, nil
}
func (dao *writerDiffSnapshotDao) UpsertPending(string, string, string) error { return nil }
