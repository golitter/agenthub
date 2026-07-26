package gormdao

import (
	"errors"
	"testing"

	"agenthub/backend/internal/model"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      db,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open gorm: %v", err)
	}
	return gormDB, mock
}

func TestCascadeDeleteBySessionIDs_EmptySlice(t *testing.T) {
	db, mock := setupTestDB(t)
	if err := cascadeDeleteBySessionIDs(db, []string{}); err != nil {
		t.Fatalf("cascadeDeleteBySessionIDs returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestCascadeDeleteBySessionIDs_DeletesAllTables(t *testing.T) {
	db, mock := setupTestDB(t)

	sessionIDs := []string{"sess-1", "sess-2"}

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `messages`").WithArgs(sessionIDs[0], sessionIDs[1]).WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `session_agents`").WithArgs(sessionIDs[0], sessionIDs[1]).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `diff_snapshots`").WithArgs(sessionIDs[0], sessionIDs[1]).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `agent_skill`").WithArgs(sessionIDs[0], sessionIDs[1]).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	if err := cascadeDeleteBySessionIDs(db, sessionIDs); err != nil {
		t.Fatalf("cascadeDeleteBySessionIDs returned error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestCascadeDeleteBySessionIDs_ReturnsDeleteError(t *testing.T) {
	db, mock := setupTestDB(t)
	wantErr := errors.New("delete messages failed")

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `messages`").WithArgs("sess-1").WillReturnError(wantErr)
	mock.ExpectRollback()

	err := cascadeDeleteBySessionIDs(db, []string{"sess-1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("cascadeDeleteBySessionIDs error = %v, want %v", err, wantErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestCascadeDeleteByTaskID_DeletesAllTables(t *testing.T) {
	db, mock := setupTestDB(t)

	taskID := "task-123"
	sessionIDs := []string{"sess-1"}

	mock.ExpectQuery("SELECT `session_id` FROM `sessions`").WithArgs(taskID).WillReturnRows(
		sqlmock.NewRows([]string{"session_id"}).AddRow("sess-1"),
	)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `messages`").WithArgs(sessionIDs[0]).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `session_agents`").WithArgs(sessionIDs[0]).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `diff_snapshots`").WithArgs(sessionIDs[0]).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `agent_skill`").WithArgs(sessionIDs[0]).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sessions`").WithArgs(taskID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `announcements`").WithArgs(taskID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `contact_group_items`").WithArgs(taskID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := cascadeDeleteByTaskID(db, taskID); err != nil {
		t.Fatalf("cascadeDeleteByTaskID returned error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestCascadeDeleteByTaskID_ReturnsPluckError(t *testing.T) {
	db, mock := setupTestDB(t)
	wantErr := errors.New("pluck failed")

	mock.ExpectQuery("SELECT `session_id` FROM `sessions`").WithArgs("task-123").WillReturnError(wantErr)

	err := cascadeDeleteByTaskID(db, "task-123")
	if !errors.Is(err, wantErr) {
		t.Fatalf("cascadeDeleteByTaskID error = %v, want %v", err, wantErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestCascadeModels(t *testing.T) {
	var _ model.Message
	var _ model.SessionAgent
	var _ model.DiffSnapshot
	var _ model.AgentSkill
	var _ model.Session
	var _ model.Announcement
	var _ model.ContactGroupItem
}
