package impl

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"

	"github.com/redis/go-redis/v9"
)

func TestSplitContentKeepsUTF8Boundaries(t *testing.T) {
	text := strings.Repeat("你", 360) + "\n" + strings.Repeat("好", 360)

	chunks := splitContent(text, 500)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if !utf8.ValidString(chunk) {
			t.Fatalf("chunk %d is not valid UTF-8", i)
		}
	}
	if got := strings.Join(chunks, ""); got != text {
		t.Fatalf("split/join changed content")
	}
}

func TestServeStreamRejectsSessionMismatch(t *testing.T) {
	dao := &streamServiceMessageDao{
		message: &model.Message{
			MessageID: "message-1",
			TaskID:    "task-1",
			SessionID: "session-a",
			Status:    "completed",
		},
	}
	svc := NewStreamService(dao)

	var out strings.Builder
	err := svc.ServeStream(t.Context(), "task-1", "session-b", "message-1", &out, noopFlusher{})
	if err == nil {
		t.Fatal("ServeStream error = nil, want not found")
	}
	bizErr, ok := err.(*service.BizError)
	if !ok {
		t.Fatalf("ServeStream error = %T, want BizError", err)
	}
	if bizErr.Code != 404 {
		t.Fatalf("BizError.Code = %d, want 404", bizErr.Code)
	}
	if out.Len() != 0 {
		t.Fatalf("stream output = %q, want empty before validation succeeds", out.String())
	}
}

func TestServeStreamRejectsTaskMismatch(t *testing.T) {
	dao := &streamServiceMessageDao{
		message: &model.Message{
			MessageID: "message-1",
			TaskID:    "task-a",
			SessionID: "session-a",
			Status:    "completed",
		},
	}
	svc := NewStreamService(dao)

	var out strings.Builder
	err := svc.ServeStream(t.Context(), "task-b", "session-a", "message-1", &out, noopFlusher{})
	if err == nil {
		t.Fatal("ServeStream error = nil, want not found")
	}
	if bizErr, ok := err.(*service.BizError); !ok || bizErr.Code != 404 {
		t.Fatalf("ServeStream error = %#v, want 404 BizError", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stream output = %q, want empty", out.String())
	}
}

func TestIsTerminalSSELine(t *testing.T) {
	for _, line := range []string{
		`data: {"type":"done"}`,
		`data: {"type":"error","content":{"message":"failed"}}`,
	} {
		if !isTerminalSSELine(line) {
			t.Fatalf("isTerminalSSELine(%q) = false", line)
		}
	}
	for _, line := range []string{
		`data: {"type":"text","content":{"text":"done"}}`,
		`data: not-json`,
		`: heartbeat`,
	} {
		if isTerminalSSELine(line) {
			t.Fatalf("isTerminalSSELine(%q) = true", line)
		}
	}
}

func TestServeRedisStreamingDeliversEachPersistedEventOnce(t *testing.T) {
	reader := &scriptedRedisStreamReader{results: []redisReadResult{
		{streams: []redis.XStream{{Stream: "agent:session-1:message-1", Messages: []redis.XMessage{
			{ID: "1-0", Values: map[string]interface{}{"data": `data: {"type":"text","content":{"text":"once"}}`}},
		}}}},
		{streams: []redis.XStream{{Stream: "agent:session-1:message-1", Messages: []redis.XMessage{
			{ID: "2-0", Values: map[string]interface{}{"data": `data: {"type":"done"}`}},
		}}}},
	}}
	svc := NewStreamService(&streamServiceMessageDao{})
	var out strings.Builder
	err := svc.serveRedisStreaming(
		t.Context(),
		&out,
		noopFlusher{},
		&model.Message{MessageID: "message-1", SessionID: "session-1", Status: "streaming"},
		reader,
		"agent:session-1:message-1",
	)
	if err != nil {
		t.Fatalf("serveRedisStreaming: %v", err)
	}
	if got := strings.Count(out.String(), `"text":"once"`); got != 1 {
		t.Fatalf("text event count = %d, want 1; output=%q", got, out.String())
	}
	if reader.calls != 2 {
		t.Fatalf("XRead calls = %d, want 2", reader.calls)
	}
}

type redisReadResult struct {
	streams []redis.XStream
	err     error
}

type scriptedRedisStreamReader struct {
	results []redisReadResult
	calls   int
}

func (reader *scriptedRedisStreamReader) XRead(ctx context.Context, _ *redis.XReadArgs) *redis.XStreamSliceCmd {
	reader.calls++
	if len(reader.results) == 0 {
		return redis.NewXStreamSliceCmdResult(nil, redis.Nil)
	}
	result := reader.results[0]
	reader.results = reader.results[1:]
	return redis.NewXStreamSliceCmdResult(result.streams, result.err)
}

func TestNormalizeStreamIDs(t *testing.T) {
	sessionID, messageID, err := normalizeStreamIDs(" session-1 ", " message-1 ")
	if err != nil {
		t.Fatalf("normalizeStreamIDs: %v", err)
	}
	if sessionID != "session-1" || messageID != "message-1" {
		t.Fatalf("ids = %q/%q, want trimmed ids", sessionID, messageID)
	}

	if _, _, err := normalizeStreamIDs("", "message-1"); err == nil {
		t.Fatal("blank session_id accepted")
	}
	if _, _, err := normalizeStreamIDs("session-1", ""); err == nil {
		t.Fatal("blank message_id accepted")
	}
	if _, _, err := normalizeStreamIDs(strings.Repeat("s", maxSessionIDLen+1), "message-1"); err == nil {
		t.Fatal("too long session_id accepted")
	}
	if _, _, err := normalizeStreamIDs("session-1", strings.Repeat("m", maxStreamMessageIDLen+1)); err == nil {
		t.Fatal("too long message_id accepted")
	}
}

type noopFlusher struct{}

func (noopFlusher) Flush() {}

type streamServiceMessageDao struct {
	message *model.Message
}

func (dao *streamServiceMessageDao) ListByTask(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64) ([]model.Message, error) {
	return nil, nil
}

func (dao *streamServiceMessageDao) CountBySessionID(sessionID string) (int64, error) {
	return 0, nil
}

func (dao *streamServiceMessageDao) FindByMessageID(messageID string) (*model.Message, error) {
	if dao.message != nil && dao.message.MessageID == messageID {
		return dao.message, nil
	}
	return nil, nil
}

func (dao *streamServiceMessageDao) CreateMessage(message model.Message) error {
	return nil
}

func (dao *streamServiceMessageDao) FindSessionIDByTaskMessage(taskID, messageID string) (string, error) {
	return "", nil
}

func (dao *streamServiceMessageDao) FindMessageContent(messageID string) (string, error) {
	return "", nil
}

func (dao *streamServiceMessageDao) UpdateMessageContentAndSeq(messageID, content, seq string) error {
	return nil
}

func (dao *streamServiceMessageDao) UpdateMessageStatus(messageID, status string) error {
	return nil
}

func (dao *streamServiceMessageDao) UpdateMessageRunState(messageID, status, terminationReason string) error {
	if dao.message != nil && dao.message.MessageID == messageID {
		dao.message.Status = status
		dao.message.TerminationReason = terminationReason
	}
	return nil
}

func (dao *streamServiceMessageDao) FailStaleStreamingMessages() (int64, error) {
	return 0, nil
}

func (dao *streamServiceMessageDao) FindLatestCompletedAgentMessage(taskID, sessionID string) (*model.Message, error) {
	return nil, nil
}

func (dao *streamServiceMessageDao) ListGroupChatWindowMessages(taskID, sessionID string, afterCreatedAt *model.Message) ([]model.Message, error) {
	return nil, nil
}

func (dao *streamServiceMessageDao) FindLatestPlanReviewMessage(taskID, sessionID string) (*model.Message, error) {
	return nil, nil
}

func (dao *streamServiceMessageDao) UpdateContent(messageID, content string) error {
	return nil
}
