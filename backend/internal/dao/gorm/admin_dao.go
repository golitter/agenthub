package gormdao

import (
	"errors"
	"time"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type AdminDao struct{}

func NewAdminDao() *AdminDao {
	return &AdminDao{}
}

func (dao *AdminDao) GetAdminSetting(key string) (*model.AdminSetting, error) {
	var setting model.AdminSetting
	if err := db.GetDB().Where("`key` = ?", key).First(&setting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &setting, nil
}

func (dao *AdminDao) ReplaceAdminSetting(key, value string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("`key` = ?", key).Delete(&model.AdminSetting{}).Error; err != nil {
			return err
		}
		return tx.Create(&model.AdminSetting{Key: key, Value: value}).Error
	})
}

func (dao *AdminDao) DeleteSessions(sessionIDs []string) (int, error) {
	deleted := 0
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		for _, sessionID := range sessionIDs {
			var count int64
			if err := tx.Model(&model.Session{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				continue
			}
			if err := cascadeDeleteBySessionIDs(tx, []string{sessionID}); err != nil {
				return err
			}
			if err := tx.Where("session_id = ?", sessionID).Delete(&model.Session{}).Error; err != nil {
				return err
			}
			deleted++
		}
		return nil
	})
	return deleted, err
}

func (dao *AdminDao) CountSessionsByDate(date string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.Session{}).Where("DATE(created_at) = ?", date).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *AdminDao) CountSessionsBetween(start, end time.Time) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.Session{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *AdminDao) CountMessages() (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.Message{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *AdminDao) CountMessagesByAgent() (map[string]int64, error) {
	type row struct {
		AgentType string
		Count     int64
	}

	var rows []row
	if err := db.GetDB().Model(&model.Message{}).
		Select("agent_type as agent_type, COUNT(*) as count").
		Where("agent_type != ''").
		Group("agent_type").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[string]int64, len(rows))
	for _, item := range rows {
		result[item.AgentType] = item.Count
	}
	return result, nil
}
