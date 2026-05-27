package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
)

type WorkspaceItem struct {
	ID     string  `json:"id"`
	Task   string  `json:"task"`
	Agent  string  `json:"agent"`
	Branch string  `json:"branch"`
	DiskMB float64 `json:"disk_mb"`
	Status string  `json:"status"`
}

func (h *AdminHandler) GetWorkspaces(c *gin.Context) {
	database := db.GetDB()

	var sessions []model.Session
	database.Where("status = ?", "running").Find(&sessions)

	var workspaces []WorkspaceItem
	for _, s := range sessions {
		workspaces = append(workspaces, WorkspaceItem{
			ID:     s.SessionID,
			Task:   s.TaskID,
			Agent:  s.AgentName,
			Branch: s.SessionID[:8],
			DiskMB: 0,
			Status: "active",
		})
	}

	vo.OK(c, gin.H{
		"workspaces": workspaces,
		"total":      len(workspaces),
		"active":     len(workspaces),
		"cleaned":    0,
		"totalDisk":  0,
	})
}

func (h *AdminHandler) DeleteWorkspace(c *gin.Context) {
	id := c.Param("id")
	database := db.GetDB()

	var session model.Session
	if err := database.Where("session_id = ?", id).First(&session).Error; err != nil {
		vo.NotFound(c, "workspace not found")
		return
	}

	database.Model(&session).Update("status", "cleaned")
	vo.OK(c, gin.H{"success": true})
}
