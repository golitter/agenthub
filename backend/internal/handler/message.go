package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"
	"strconv"

	"github.com/gin-gonic/gin"
)

type MessageHandler struct{}

func NewMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

type ListMessagesResponse struct {
	Data    []model.Message `json:"data"`
	HasMore bool            `json:"has_more"`
}

func (h *MessageHandler) ListMessages(c *gin.Context) {
	taskID := c.Param("taskId")

	var task model.Task
	if err := db.GetDB().Where("task_id = ?", taskID).First(&task).Error; err != nil {
		vo.NotFound(c, "task not found")
		return
	}

	limitStr := c.Query("limit")
	beforeStr := c.Query("before")
	sessionID := c.Query("session_id")

	// No pagination params: return all messages, has_more=false
	if limitStr == "" && beforeStr == "" {
		query := db.GetDB().Where("task_id = ?", taskID)
		if sessionID != "" {
			query = query.Where("session_id = ?", sessionID)
		}
		var messages []model.Message
		query.Order("created_at ASC").Order("id ASC").Find(&messages)
		vo.OK(c, ListMessagesResponse{Data: messages, HasMore: false})
		return
	}

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	query := db.GetDB().Where("task_id = ?", taskID)
	if sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}

	if beforeStr != "" {
		if beforeID, err := strconv.ParseUint(beforeStr, 10, 64); err == nil {
			query = query.Where("id < ?", beforeID)
		}
	}

	var messages []model.Message
	query.Order("id DESC").Limit(limit + 1).Find(&messages)

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	reverseMessages(messages)

	vo.OK(c, ListMessagesResponse{Data: messages, HasMore: hasMore})
}

func reverseMessages(messages []model.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}
