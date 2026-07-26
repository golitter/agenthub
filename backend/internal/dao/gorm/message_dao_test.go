package gormdao

import (
	"errors"
	"strings"
	"testing"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

func TestApplyGroupMessageVisibilityUsesDirectTurnReplyPredicate(t *testing.T) {
	db, mock := setupTestDB(t)
	query := db.Session(&gorm.Session{DryRun: true}).
		Model(&model.Message{}).
		Where("task_id = ?", "task-123")

	stmt := applyGroupMessageVisibility(query, "task-123", "orch-session").Find(&[]model.Message{}).Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		"session_id = ?",
		"group_id <> ?",
		"SELECT 1 FROM messages user_msg",
		"SELECT 1 FROM messages agent_msg",
		"agent_msg.id > user_msg.id",
		"agent_msg.id < messages.id",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("group visibility SQL = %q, want substring %q", sql, want)
		}
	}
	if strings.Contains(sql, "DISTINCT session_id") {
		t.Fatalf("group visibility should not expose all messages from directly-addressed sessions: %q", sql)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestStaleStreamingSessionPairsQuery(t *testing.T) {
	db, mock := setupTestDB(t)
	stmt := staleStreamingSessionPairsQuery(db.Session(&gorm.Session{DryRun: true})).Find(&[]staleMessageSessionPair{}).Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		"DISTINCT session_id, task_id",
		"status = ?",
		"session_id <> ?",
		"task_id <> ?",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("stale session pair SQL = %q, want substring %q", sql, want)
		}
	}
	if got := stmt.Vars[0]; got != string(generated.MessageStatusStreaming) {
		t.Fatalf("stale session pair status var = %v, want %s", got, generated.MessageStatusStreaming)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestIsAllowedMessageStatus(t *testing.T) {
	for _, status := range []string{"streaming", "completed", "failed"} {
		if !isAllowedMessageStatus(status) {
			t.Fatalf("status %q rejected", status)
		}
	}

	for _, status := range []string{"running", "error", "done", ""} {
		if isAllowedMessageStatus(status) {
			t.Fatalf("status %q accepted", status)
		}
	}
}

func TestIsAllowedMessageRole(t *testing.T) {
	for _, role := range []string{"user", "agent"} {
		if !isAllowedMessageRole(role) {
			t.Fatalf("role %q rejected", role)
		}
	}

	for _, role := range []string{"system", "assistant", ""} {
		if isAllowedMessageRole(role) {
			t.Fatalf("role %q accepted", role)
		}
	}
}

func TestCreateMessageRejectsInvalidRole(t *testing.T) {
	dao := NewMessageDao()
	if err := dao.CreateMessage(model.Message{MessageID: "msg-1", TaskID: "task-1", SessionID: "session-1", Role: "system", Status: "completed"}); err == nil {
		t.Fatal("CreateMessage accepted invalid role")
	}
}

func TestCreateMessageRejectsInvalidStatus(t *testing.T) {
	dao := NewMessageDao()
	if err := dao.CreateMessage(model.Message{MessageID: "msg-1", TaskID: "task-1", SessionID: "session-1", Role: "agent", Status: "done"}); err == nil {
		t.Fatal("CreateMessage accepted invalid status")
	}
}

func TestNormalizeCreateMessageDefaultsAndTrims(t *testing.T) {
	message, err := normalizeCreateMessage(model.Message{
		MessageID: " msg-1 ",
		TaskID:    " task-1 ",
		SessionID: " session-1 ",
		Role:      " user ",
		Content:   "  keep spaces  ",
		AgentType: " codex ",
		AgentName: " Codex ",
		GroupID:   " group-1 ",
	})
	if err != nil {
		t.Fatalf("normalizeCreateMessage returned error: %v", err)
	}
	if message.MessageID != "msg-1" || message.TaskID != "task-1" || message.SessionID != "session-1" {
		t.Fatalf("ids were not trimmed: %#v", message)
	}
	if message.Role != "user" || message.Status != "completed" {
		t.Fatalf("role/status = %q/%q, want user/completed", message.Role, message.Status)
	}
	if message.Content != "  keep spaces  " {
		t.Fatalf("content was unexpectedly changed: %q", message.Content)
	}
	if message.AgentType != "codex" || message.AgentName != "Codex" || message.GroupID != "group-1" {
		t.Fatalf("agent metadata was not trimmed: %#v", message)
	}
}

func TestNormalizeCreateMessageRejectsMissingIDs(t *testing.T) {
	if _, err := normalizeCreateMessage(model.Message{TaskID: "task-1", SessionID: "session-1", Role: "user"}); err == nil {
		t.Fatal("normalizeCreateMessage accepted empty message_id")
	}
	if _, err := normalizeCreateMessage(model.Message{MessageID: "msg-1", SessionID: "session-1", Role: "user"}); err == nil {
		t.Fatal("normalizeCreateMessage accepted empty task_id")
	}
	if _, err := normalizeCreateMessage(model.Message{MessageID: "msg-1", TaskID: "task-1", Role: "user"}); err == nil {
		t.Fatal("normalizeCreateMessage accepted empty session_id")
	}
}

func TestNormalizeCreateMessageRejectsLongFields(t *testing.T) {
	base := model.Message{MessageID: "msg-1", TaskID: "task-1", SessionID: "session-1", Role: "user"}
	tooLongID := strings.Repeat("x", maxMessageIDLen+1)
	if _, err := normalizeCreateMessage(model.Message{MessageID: tooLongID, TaskID: base.TaskID, SessionID: base.SessionID, Role: base.Role}); err == nil {
		t.Fatal("normalizeCreateMessage accepted long message_id")
	}
	if _, err := normalizeCreateMessage(model.Message{MessageID: base.MessageID, TaskID: strings.Repeat("x", maxMessageTaskIDLen+1), SessionID: base.SessionID, Role: base.Role}); err == nil {
		t.Fatal("normalizeCreateMessage accepted long task_id")
	}
	base.AgentType = strings.Repeat("x", maxMessageAgentTypeLen+1)
	if _, err := normalizeCreateMessage(base); err == nil {
		t.Fatal("normalizeCreateMessage accepted long agent_type")
	}
	base.AgentType = ""
	base.AgentName = strings.Repeat("x", maxMessageAgentNameLen+1)
	if _, err := normalizeCreateMessage(base); err == nil {
		t.Fatal("normalizeCreateMessage accepted long agent_name")
	}
	base.AgentName = ""
	base.GroupID = strings.Repeat("x", maxMessageGroupIDLen+1)
	if _, err := normalizeCreateMessage(base); err == nil {
		t.Fatal("normalizeCreateMessage accepted long group_id")
	}
}

func TestUpdateMessageStatusRejectsInvalidStatus(t *testing.T) {
	dao := NewMessageDao()
	if err := dao.UpdateMessageStatus("msg-1", "done"); err == nil {
		t.Fatal("UpdateMessageStatus accepted invalid message status")
	}
}

func TestEnsureMessageUpdateFoundRowsAffected(t *testing.T) {
	db, mock := setupTestDB(t)
	if err := ensureMessageUpdateFound(db, 1, "msg-1"); err != nil {
		t.Fatalf("ensureMessageUpdateFound returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestEnsureMessageUpdateFoundExistingSameValue(t *testing.T) {
	db, mock := setupTestDB(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `messages`").WithArgs("msg-1").WillReturnRows(
		sqlmock.NewRows([]string{"count"}).AddRow(1),
	)

	if err := ensureMessageUpdateFound(db, 0, "msg-1"); err != nil {
		t.Fatalf("ensureMessageUpdateFound returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestEnsureMessageUpdateFoundMissing(t *testing.T) {
	db, mock := setupTestDB(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `messages`").WithArgs("msg-1").WillReturnRows(
		sqlmock.NewRows([]string{"count"}).AddRow(0),
	)

	err := ensureMessageUpdateFound(db, 0, "msg-1")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("ensureMessageUpdateFound error = %v, want %v", err, gorm.ErrRecordNotFound)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}
