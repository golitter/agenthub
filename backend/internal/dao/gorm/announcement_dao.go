package gormdao

import (
	"errors"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
)

type AnnouncementDao struct{}

func NewAnnouncementDao() *AnnouncementDao {
	return &AnnouncementDao{}
}

func (dao *AnnouncementDao) ListByTaskID(taskID string, pinnedOnly bool) ([]model.Announcement, error) {
	query := db.GetDB().Where("task_id = ?", taskID)
	if pinnedOnly {
		query = query.Where("pinned = ?", true)
	}

	var announcements []model.Announcement
	if err := query.Order("pinned DESC, created_at DESC").Find(&announcements).Error; err != nil {
		return nil, err
	}
	return announcements, nil
}

func (dao *AnnouncementDao) CreateAnnouncement(announcement model.Announcement) (*model.Announcement, error) {
	if err := db.GetDB().Create(&announcement).Error; err != nil {
		return nil, err
	}
	return &announcement, nil
}

func (dao *AnnouncementDao) GetAnnouncementByID(id uint) (*model.Announcement, error) {
	var announcement model.Announcement
	if err := db.GetDB().Where("id = ?", id).First(&announcement).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &announcement, nil
}

func (dao *AnnouncementDao) DeleteAnnouncement(id uint) (*model.Announcement, error) {
	announcement, err := dao.GetAnnouncementByID(id)
	if err != nil || announcement == nil {
		return announcement, err
	}
	if err := db.GetDB().Delete(announcement).Error; err != nil {
		return nil, err
	}
	return announcement, nil
}
