package impl

import (
	"strings"
	"testing"
	"unicode/utf8"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
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
			SessionID: "session-a",
			Status:    "completed",
		},
	}
	svc := NewStreamService(dao)

	var out strings.Builder
	err := svc.ServeStream(t.Context(), "session-b", "message-1", &out, noopFlusher{})
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
