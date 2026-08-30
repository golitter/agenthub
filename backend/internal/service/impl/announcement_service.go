package impl

import (
	"strconv"
	"strings"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
)

type AnnouncementService struct {
	announcementDao dao.AnnouncementDao
}

const (
	maxAnnouncementSenderLen  = 64
	maxAnnouncementContentLen = 10000
)

func NewAnnouncementService(announcementDao dao.AnnouncementDao) *AnnouncementService {
	return &AnnouncementService{
		announcementDao: announcementDao,
	}
}

func (svc *AnnouncementService) ListAnnouncements(taskID string, pinnedOnly bool) ([]model.Announcement, error) {
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
	return svc.announcementDao.ListByTaskID(taskID, pinnedOnly)
}

func (svc *AnnouncementService) CreateAnnouncement(taskID string, input service.CreateAnnouncementInput) (*model.Announcement, error) {
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeAnnouncementInput(input)
	if err != nil {
		return nil, err
	}
	return svc.announcementDao.CreateAnnouncement(model.Announcement{
		TaskID:     taskID,
		SenderID:   normalized.SenderID,
		SenderName: normalized.SenderName,
		Content:    normalized.Content,
		Pinned:     normalized.Pinned,
	})
}

func normalizeAnnouncementInput(input service.CreateAnnouncementInput) (service.CreateAnnouncementInput, error) {
	input.SenderID = strings.TrimSpace(input.SenderID)
	input.SenderName = strings.TrimSpace(input.SenderName)
	input.Content = strings.TrimSpace(input.Content)
	if input.SenderID == "" {
		return input, service.ErrBadRequest("sender_id is required")
	}
	if input.SenderName == "" {
		return input, service.ErrBadRequest("sender_name is required")
	}
	if input.Content == "" {
		return input, service.ErrBadRequest("content is required")
	}
	if len([]rune(input.SenderName)) > maxAnnouncementSenderLen {
		return input, service.ErrBadRequest("sender_name is too long")
	}
	if len([]rune(input.SenderID)) > maxAnnouncementSenderLen {
		return input, service.ErrBadRequest("sender_id is too long")
	}
	if len([]rune(input.Content)) > maxAnnouncementContentLen {
		return input, service.ErrBadRequest("content is too long")
	}
	return input, nil
}

func (svc *AnnouncementService) DeleteAnnouncement(id string) error {
	announcementID, err := normalizeAnnouncementID(id)
	if err != nil {
		return err
	}
	announcement, err := svc.announcementDao.DeleteAnnouncement(announcementID)
	if err != nil {
		return err
	}
	if announcement == nil {
		return service.ErrNotFound("announcement not found")
	}
	return nil
}

func normalizeAnnouncementID(id string) (uint, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, service.ErrBadRequest("announcement id is required")
	}
	parsed, err := strconv.ParseUint(id, 10, 64)
	if err != nil || parsed == 0 || parsed > uint64(^uint(0)) {
		return 0, service.ErrBadRequest("announcement id is invalid")
	}
	return uint(parsed), nil
}
