package handler

import (
	"io"
	"path/filepath"
	"strings"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/qiniu"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	maxAvatarSize = 2 << 20 // 2MB
)

var allowedExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

type AvatarHandler struct {
	uploader *qiniu.Uploader
}

func NewAvatarHandler(uploader *qiniu.Uploader) *AvatarHandler {
	return &AvatarHandler{uploader: uploader}
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

	key := "avatars/" + uuid.New().String() + ext
	data, err := io.ReadAll(file)
	if err != nil {
		vo.InternalError(c, "failed to read file")
		return
	}

	avatarURL, err := h.uploader.UploadBytes(c.Request.Context(), key, data)
	if err != nil {
		vo.InternalError(c, "failed to upload file")
		return
	}

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

	// Update sessions table
	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}

	if err := db.GetDB().Model(&session).Updates(updates).Error; err != nil {
		vo.InternalError(c, "failed to update session")
		return
	}

	vo.OK(c, gin.H{"session_id": sessionID})
}
