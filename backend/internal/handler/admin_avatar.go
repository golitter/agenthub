package handler

import (
	"sync"

	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

var (
	adminAvatarURL string
	avatarMu       sync.RWMutex
)

type AvatarRequest struct {
	URL string `json:"url" binding:"required"`
}

func (h *AdminHandler) GetAvatar(c *gin.Context) {
	avatarMu.RLock()
	url := adminAvatarURL
	avatarMu.RUnlock()

	if url == "" {
		url = "https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede"
	}

	vo.OK(c, gin.H{"url": url})
}

func (h *AdminHandler) UpdateAvatar(c *gin.Context) {
	var req AvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "url is required")
		return
	}

	avatarMu.Lock()
	adminAvatarURL = req.URL
	avatarMu.Unlock()

	vo.OK(c, gin.H{"success": true})
}
