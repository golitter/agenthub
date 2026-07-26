package gormdao

import (
	"errors"
	"fmt"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type SessionDao struct{}

func NewSessionDao() *SessionDao {
	return &SessionDao{}
}

func (dao *SessionDao) DeactivateSession(sessionID string) (bool, error) {
	result := db.GetDB().
		Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Update("status", string(generated.SessionStateInactive))
	if result.Error != nil {
		return false, result.Error
	}

	if result.RowsAffected > 0 {
		return true, nil
	}

	var count int64
	if err := db.GetDB().Model(&model.Session{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *SessionDao) GetBySessionID(sessionID string) (*model.Session, error) {
	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func (dao *SessionDao) GetByTaskAndSessionID(taskID, sessionID string) (*model.Session, error) {
	var session model.Session
	if err := db.GetDB().Where("task_id = ? AND session_id = ?", taskID, sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func (dao *SessionDao) ListByTaskID(taskID string) ([]model.Session, error) {
	var sessions []model.Session
	if err := db.GetDB().Where("task_id = ?", taskID).Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (dao *SessionDao) ListAll() ([]model.Session, error) {
	var sessions []model.Session
	if err := db.GetDB().Order("created_at DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (dao *SessionDao) FindPrimaryGroupSessionID(taskID string) (string, error) {
	var session model.Session
	if err := db.GetDB().
		Where("task_id = ? AND agent_type = ?", taskID, "orchestrator").
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return session.SessionID, nil
}

func (dao *SessionDao) UpdateFields(sessionID string, updates map[string]interface{}) (bool, error) {
	if status, ok := updates["status"].(string); ok && !isAllowedSessionStatus(status) {
		return false, fmt.Errorf("invalid session status: %s", status)
	}

	updated := false
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		result := tx.
			Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&model.Session{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
				return err
			}
			updated = count > 0
			return nil
		}
		updated = true

		agentUpdates := map[string]interface{}{}
		for _, key := range []string{"agent_name", "agent_type", "avatar_url"} {
			if value, ok := updates[key]; ok {
				agentUpdates[key] = value
			}
		}
		if len(agentUpdates) == 0 {
			return nil
		}
		return tx.
			Model(&model.SessionAgent{}).
			Where("session_id = ?", sessionID).
			Updates(agentUpdates).Error
	})
	if err != nil {
		return false, err
	}
	return updated, nil
}

func (dao *SessionDao) UpdateSoul(sessionID, soulMD string) (bool, error) {
	result := db.GetDB().
		Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		Update("soul_md", soulMD)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}

	var count int64
	if err := db.GetDB().Model(&model.Session{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *SessionDao) UpdateStatusByTask(sessionID, taskID, status string) error {
	if !isAllowedSessionStatus(status) {
		return fmt.Errorf("invalid session status: %s", status)
	}
	result := db.GetDB().
		Model(&model.Session{}).
		Where("session_id = ? AND task_id = ?", sessionID, taskID).
		Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	var count int64
	if err := db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", sessionID, taskID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func isAllowedSessionStatus(status string) bool {
	switch generated.SessionState(status) {
	case generated.SessionStateIdle,
		generated.SessionStateRunning,
		generated.SessionStateAwaitingReview,
		generated.SessionStateCompleted,
		generated.SessionStateInterrupted,
		generated.SessionStateError,
		generated.SessionStateInactive:
		return true
	default:
		return false
	}
}
