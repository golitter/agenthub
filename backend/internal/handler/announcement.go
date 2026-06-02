package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
)

type AnnouncementHandler struct{}

func NewAnnouncementHandler() *AnnouncementHandler {
	return &AnnouncementHandler{}
}

type CreateAnnouncementReq struct {
	SenderID   string `json:"sender_id" binding:"required"`
	SenderName string `json:"sender_name" binding:"required"`
	Content    string `json:"content" binding:"required"`
	Pinned     bool   `json:"pinned"`
}

// ListAnnouncements returns all announcements for a task, pinned first then by time descending.
// Supports optional ?pinned=true query parameter to filter only pinned announcements.
func (h *AnnouncementHandler) ListAnnouncements(c *gin.Context) {
	taskID := c.Param("taskId")
	pinnedOnly := c.Query("pinned") == "true"

	query := db.GetDB().Where("task_id = ?", taskID)
	if pinnedOnly {
		query = query.Where("pinned = ?", true)
	}

	var announcements []model.Announcement
	if err := query.
		Order("pinned DESC, created_at DESC").
		Find(&announcements).Error; err != nil {
		vo.InternalError(c, "failed to fetch announcements")
		return
	}

	vo.OK(c, announcements)
}

// CreateAnnouncement creates a new announcement for a task.
func (h *AnnouncementHandler) CreateAnnouncement(c *gin.Context) {
	taskID := c.Param("taskId")

	var req CreateAnnouncementReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "content is required")
		return
	}

	announcement := model.Announcement{
		TaskID:     taskID,
		SenderID:   req.SenderID,
		SenderName: req.SenderName,
		Content:    req.Content,
		Pinned:     req.Pinned,
	}

	if err := db.GetDB().Create(&announcement).Error; err != nil {
		vo.InternalError(c, "failed to create announcement")
		return
	}

	vo.Created(c, announcement)
}

// DeleteAnnouncement deletes an announcement by ID.
func (h *AnnouncementHandler) DeleteAnnouncement(c *gin.Context) {
	id := c.Param("id")

	result := db.GetDB().Where("id = ?", id).Delete(&model.Announcement{})
	if result.RowsAffected == 0 {
		vo.NotFound(c, "announcement not found")
		return
	}

	vo.OK(c, nil)
}
