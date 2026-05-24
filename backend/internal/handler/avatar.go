package handler

import (
	"os"
	"path/filepath"
	"strings"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	maxAvatarSize = 2 << 20 // 2MB
	avatarDir     = "uploads/avatars"
)

var allowedExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

type AvatarHandler struct{}

func NewAvatarHandler() *AvatarHandler {
	return &AvatarHandler{}
}

func (h *AvatarHandler) UploadAvatar(c *gin.Context) {
	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		vo.BadRequest(c, "avatar file is required")
		return
	}
	defer file.Close()

	if header.Size > maxAvatarSize {
		vo.BadRequest(c, "file size exceeds 2MB limit")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		vo.BadRequest(c, "unsupported file format, allowed: jpg/png/gif/webp")
		return
	}

	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		vo.InternalError(c, "failed to create upload directory")
		return
	}

	filename := uuid.New().String() + ext
	savePath := filepath.Join(avatarDir, filename)

	if err := c.SaveUploadedFile(header, savePath); err != nil {
		vo.InternalError(c, "failed to save file")
		return
	}

	avatarURL := "/" + strings.ReplaceAll(savePath, "\\", "/")
	vo.OK(c, gin.H{"avatar_url": avatarURL})
}

type UpdateSessionReq struct {
	AgentName string `json:"agent_name"`
	AvatarURL string `json:"avatar_url"`
}

func (h *AvatarHandler) UpdateSession(c *gin.Context) {
	sessionID := c.Param("sessionId")

	var req UpdateSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "invalid request body")
		return
	}

	updates := map[string]interface{}{}
	if req.AgentName != "" {
		updates["agent_name"] = req.AgentName
	}
	if req.AvatarURL != "" {
		updates["avatar_url"] = req.AvatarURL
	}

	if len(updates) == 0 {
		vo.BadRequest(c, "at least one field (agent_name or avatar_url) is required")
		return
	}

	result := db.GetDB().Model(&model.Session{}).Where("session_id = ?", sessionID).Updates(updates)
	if result.RowsAffected == 0 {
		vo.NotFound(c, "session not found")
		return
	}

	vo.OK(c, gin.H{"session_id": sessionID})
}
