package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
)

type DeleteSessionsRequest struct {
	SessionIDs []string `json:"session_ids" binding:"required"`
}

func (h *AdminHandler) DeleteSessions(c *gin.Context) {
	var req DeleteSessionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "session_ids is required")
		return
	}

	database := db.GetDB()
	deleted := 0

	for _, sid := range req.SessionIDs {
		if err := database.Where("session_id = ?", sid).Delete(&model.Message{}).Error; err != nil {
			continue
		}
		if err := database.Where("session_id = ?", sid).Delete(&model.SessionAgent{}).Error; err != nil {
			continue
		}
		if err := database.Where("session_id = ?", sid).Delete(&model.Session{}).Error; err != nil {
			continue
		}
		deleted++
	}

	vo.OK(c, gin.H{"deleted": deleted})
}
