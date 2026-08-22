package gormdao

import (
	"context"
	"encoding/json"
	"errors"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskDao struct{}

func NewTaskDao() *TaskDao {
	return &TaskDao{}
}

func (dao *TaskDao) GetByTaskID(taskID string) (*model.Task, error) {
	return dao.GetByTaskIDContext(context.Background(), taskID)
}

func (dao *TaskDao) GetByTaskIDContext(ctx context.Context, taskID string) (*model.Task, error) {
	var task model.Task
	if err := db.GetDB().WithContext(ctx).Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func (dao *TaskDao) FindRepoPathByTaskID(taskID string) (string, error) {
	var task model.Task
	if err := db.GetDB().Select("repo_path").Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return task.RepoPath, nil
}

func (dao *TaskDao) CreateTaskWithSessions(task *model.Task, sessions []model.Session, sessionAgents []model.SessionAgent) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		for _, session := range sessions {
			if err := tx.Create(&session).Error; err != nil {
				return err
			}
		}
		for _, sessionAgent := range sessionAgents {
			if err := tx.Create(&sessionAgent).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (dao *TaskDao) ListTasks(limit int, beforeTaskID string) ([]model.Task, error) {
	var tasks []model.Task
	query := db.GetDB().Model(&model.Task{})
	if beforeTaskID != "" {
		var before model.Task
		if err := db.GetDB().Where("task_id = ?", beforeTaskID).First(&before).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		query = taskListCursorQuery(query, before)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Order("pinned_at IS NULL, pinned_at DESC, created_at DESC, task_id DESC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func taskListCursorQuery(query *gorm.DB, before model.Task) *gorm.DB {
	if before.PinnedAt != nil {
		return query.Where(
			"(pinned_at IS NULL) OR (pinned_at IS NOT NULL AND (pinned_at < ? OR (pinned_at = ? AND (created_at < ? OR (created_at = ? AND task_id < ?)))))",
			*before.PinnedAt, *before.PinnedAt, before.CreatedAt, before.CreatedAt, before.TaskID,
		)
	}
	return query.Where(
		"pinned_at IS NULL AND (created_at < ? OR (created_at = ? AND task_id < ?))",
		before.CreatedAt, before.CreatedAt, before.TaskID,
	)
}

func (dao *TaskDao) ListSessionAgentsBySessionIDs(sessionIDs []string) ([]model.SessionAgent, error) {
	var agents []model.SessionAgent
	if len(sessionIDs) == 0 {
		return agents, nil
	}
	if err := db.GetDB().Where("session_id IN ?", sessionIDs).Find(&agents).Error; err != nil {
		return nil, err
	}
	return agents, nil
}

func (dao *TaskDao) DeleteTaskCascade(taskID string) (bool, error) {
	return dao.deleteTaskCascade(context.Background(), taskID, "")
}

func (dao *TaskDao) DeleteTaskCascadeWithCleanup(ctx context.Context, taskID, action string) (bool, error) {
	return dao.deleteTaskCascade(ctx, taskID, action)
}

func (dao *TaskDao) deleteTaskCascade(ctx context.Context, taskID, cleanupAction string) (bool, error) {
	found := true
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", taskID).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				found = false
				return nil
			}
			return err
		}
		if cleanupAction != "" {
			var sessionIDs []string
			if err := tx.Model(&model.Session{}).Where("task_id = ?", taskID).Pluck("session_id", &sessionIDs).Error; err != nil {
				return err
			}
			encoded, err := json.Marshal(sessionIDs)
			if err != nil {
				return err
			}
			if err := tx.Create(&model.TaskCleanupJob{
				TaskID: taskID, Action: cleanupAction, RepoPath: task.RepoPath,
				SessionIDsJSON: string(encoded), Status: model.TaskCleanupStatusPending,
			}).Error; err != nil {
				return err
			}
		}
		// Close the upload/delete race in the same transaction as the message
		// cascade. CreatePending and MarkReady take the same Agent-message row
		// lock. Pending rows are deliberately left for the uploader/stale cleanup
		// path; completed rows can be marked deleting immediately.
		var agentMessages []struct {
			MessageID string `gorm:"column:message_id"`
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Model(&model.Message{}).Select("message_id").
			Where("task_id = ? AND role = ?", taskID, string(generated.MessageRoleAgent)).
			Find(&agentMessages).Error; err != nil {
			return err
		}
		// Keep pending rows until their uploader finishes. Deleting a pending
		// object here can race with the subsequent MinIO Put and leave an orphan
		// object with no metadata row to discover. The uploader will either mark
		// the row failed after the message disappears, or stale cleanup will reap
		// a crashed upload.
		deletableStatuses := []string{model.ArtifactStatusReady, model.ArtifactStatusFailed, model.ArtifactStatusDeleting}
		if err := tx.Model(&model.Artifact{}).
			Where("task_id = ? AND status IN ?", taskID, deletableStatuses).
			Update("status", model.ArtifactStatusDeleting).Error; err != nil {
			return err
		}
		if err := cascadeDeleteByTaskID(tx, taskID); err != nil {
			return err
		}
		result := tx.Where("task_id = ?", taskID).Delete(&model.Task{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			found = false
			return nil
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

func (dao *TaskDao) GetTaskAndSessionIDs(taskID string) (*model.Task, []string, error) {
	return dao.GetTaskAndSessionIDsContext(context.Background(), taskID)
}

func (dao *TaskDao) GetTaskAndSessionIDsContext(ctx context.Context, taskID string) (*model.Task, []string, error) {
	task, err := dao.GetByTaskIDContext(ctx, taskID)
	if err != nil || task == nil {
		return task, nil, err
	}

	var sessionIDs []string
	if err := db.GetDB().WithContext(ctx).Model(&model.Session{}).Where("task_id = ?", taskID).Pluck("session_id", &sessionIDs).Error; err != nil {
		return nil, nil, err
	}
	return task, sessionIDs, nil
}

func (dao *TaskDao) PatchTask(taskID string, updates map[string]interface{}) (bool, error) {
	result := db.GetDB().Model(&model.Task{}).Where("task_id = ?", taskID).Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}

	var count int64
	if err := db.GetDB().Model(&model.Task{}).Where("task_id = ?", taskID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
