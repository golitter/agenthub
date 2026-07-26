package gormdao

import (
	"agenthub/backend/internal/model"

	"gorm.io/gorm"
)

func cascadeDeleteBySessionIDs(tx *gorm.DB, sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		return nil
	}
	if err := tx.Where("session_id IN ?", sessionIDs).Delete(&model.Message{}).Error; err != nil {
		return err
	}
	if err := tx.Where("session_id IN ?", sessionIDs).Delete(&model.SessionAgent{}).Error; err != nil {
		return err
	}
	if err := tx.Where("session_id IN ?", sessionIDs).Delete(&model.DiffSnapshot{}).Error; err != nil {
		return err
	}
	if err := tx.Where("session_id IN ?", sessionIDs).Delete(&model.AgentSkill{}).Error; err != nil {
		return err
	}
	return nil
}

func cascadeDeleteByTaskID(tx *gorm.DB, taskID string) error {
	var sessionIDs []string
	if err := tx.Model(&model.Session{}).Where("task_id = ?", taskID).Pluck("session_id", &sessionIDs).Error; err != nil {
		return err
	}
	if err := cascadeDeleteBySessionIDs(tx, sessionIDs); err != nil {
		return err
	}
	if err := tx.Where("task_id = ?", taskID).Delete(&model.Session{}).Error; err != nil {
		return err
	}
	if err := tx.Where("task_id = ?", taskID).Delete(&model.Announcement{}).Error; err != nil {
		return err
	}
	if err := tx.Where("task_id = ?", taskID).Delete(&model.ContactGroupItem{}).Error; err != nil {
		return err
	}
	return nil
}
