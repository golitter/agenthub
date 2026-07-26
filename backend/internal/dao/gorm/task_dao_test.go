package gormdao

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"agenthub/backend/internal/model"

	"gorm.io/gorm"
)

func TestApplyTaskListCursorForPinnedBoundary(t *testing.T) {
	db, mock := setupTestDB(t)
	pinnedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	before := model.Task{
		TaskID:    "task-pinned",
		PinnedAt:  &pinnedAt,
		CreatedAt: pinnedAt.Add(-time.Hour),
	}

	stmt := taskListCursorQuery(db.Session(&gorm.Session{DryRun: true}).Model(&model.Task{}), before).Find(&[]model.Task{}).Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		"pinned_at IS NULL",
		"pinned_at IS NOT NULL",
		"pinned_at < ?",
		"created_at < ?",
		"task_id < ?",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("task list cursor SQL = %q, want substring %q", sql, want)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestApplyTaskListCursorForUnpinnedBoundary(t *testing.T) {
	db, mock := setupTestDB(t)
	before := model.Task{
		TaskID:    "task-unpinned",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	stmt := taskListCursorQuery(db.Session(&gorm.Session{DryRun: true}).Model(&model.Task{}), before).Find(&[]model.Task{}).Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		"pinned_at IS NULL",
		"created_at < ?",
		"task_id < ?",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("task list cursor SQL = %q, want substring %q", sql, want)
		}
	}
	if matched, _ := regexp.MatchString(`pinned_at\s+IS\s+NOT\s+NULL`, sql); matched {
		t.Fatalf("unpinned cursor should not include pinned rows: %q", sql)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}
