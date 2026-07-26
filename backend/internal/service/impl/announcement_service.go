package impl

import (
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/agentend_client"
)

type AnnouncementService struct {
	announcementDao dao.AnnouncementDao
	taskDao         dao.TaskDao
	agentClient     *agentend_client.Client
}

const (
	maxAnnouncementSenderLen  = 64
	maxAnnouncementContentLen = 10000
)

func NewAnnouncementService(announcementDao dao.AnnouncementDao, taskDao dao.TaskDao, agentClient *agentend_client.Client) *AnnouncementService {
	return &AnnouncementService{
		announcementDao: announcementDao,
		taskDao:         taskDao,
		agentClient:     agentClient,
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
	if announcement.Pinned {
		svc.notifyUnpin(*announcement)
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

func (svc *AnnouncementService) notifyUnpin(announcement model.Announcement) {
	go func() {
		repoPath, err := svc.taskDao.FindRepoPathByTaskID(announcement.TaskID)
		if err != nil {
			slog.Warn("failed to find task for announcement unpin notification", "task_id", announcement.TaskID, "error", err)
			return
		}
		if repoPath == "" {
			slog.Warn("task has no repo_path, skipping unpin notification", "task_id", announcement.TaskID)
			return
		}

		absRepoPath, err := filepath.Abs(repoPath)
		if err != nil {
			slog.Warn("failed to resolve repo_path", "repo_path", repoPath, "error", err)
			return
		}
		sharedDir := filepath.Join(filepath.Dir(absRepoPath), "worktrees", announcement.TaskID, "shared", ".agent")

		if err := svc.agentClient.NotifyAnnouncementUnpin(agentend_client.AnnouncementUnpinRequest{
			SharedDir:  sharedDir,
			Content:    announcement.Content,
			SenderName: announcement.SenderName,
		}); err != nil {
			slog.Warn("failed to notify agentend of announcement unpin", "task_id", announcement.TaskID, "announcement_id", announcement.ID, "error", err)
		}
	}()
}
