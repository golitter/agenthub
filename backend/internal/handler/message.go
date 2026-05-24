package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
)

type MessageHandler struct{}

func NewMessageHandler() *MessageHandler {
	return &MessageHandler{}
}

func (h *MessageHandler) ListMessages(c *gin.Context) {
	taskID := c.Param("taskId")

	var task model.Task
	if err := db.GetDB().Where("task_id = ?", taskID).First(&task).Error; err != nil {
		vo.NotFound(c, "task not found")
		return
	}

	var messages []model.Message
	db.GetDB().Where("task_id = ?", taskID).Order("created_at ASC").Find(&messages)
	vo.OK(c, messages)
}
