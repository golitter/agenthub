package gormdao

import (
	"errors"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type TaskDao struct{}

func NewTaskDao() *TaskDao {
	return &TaskDao{}
}

func (dao *TaskDao) GetByTaskID(taskID string) (*model.Task, error) {
	var task model.Task
	if err := db.GetDB().Where("task_id = ?", taskID).First(&task).Error; err != nil {
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
	found := true
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.Task{}).Where("task_id = ?", taskID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			found = false
			return nil
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
	task, err := dao.GetByTaskID(taskID)
	if err != nil || task == nil {
		return task, nil, err
	}

	var sessionIDs []string
	if err := db.GetDB().Model(&model.Session{}).Where("task_id = ?", taskID).Pluck("session_id", &sessionIDs).Error; err != nil {
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
