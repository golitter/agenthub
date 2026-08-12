package impl

import (
	"io"
	"path/filepath"
	"strings"

	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

const (
	maxAvatarSize = 2 << 20
)

var allowedExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

type AvatarController struct {
	service service.AvatarService
}

func NewAvatarController(avatarService service.AvatarService) *AvatarController {
	return &AvatarController{service: avatarService}
}

type UpdateSessionReq struct {
	AgentName string `json:"agent_name"`
	AvatarURL string `json:"avatar_url"`
}

func (ctrl *AvatarController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/agents/avatar", ctrl.UploadAvatar)
	rg.PUT("/sessions/:sessionId", ctrl.UpdateSession)
}

func (ctrl *AvatarController) UploadAvatar(c *gin.Context) {
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

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarSize+1))
	if err != nil {
		vo.InternalError(c, "failed to read file")
		return
	}
	if len(data) > maxAvatarSize {
		vo.BadRequest(c, "file size exceeds 2MB limit")
		return
	}

	avatarURL, err := ctrl.service.UploadAvatar(c.Request.Context(), header.Filename, data)
	if err != nil {
		handleBizError(c, err)
		return
	}

	vo.OK(c, gin.H{"avatar_url": avatarURL})
}

func (ctrl *AvatarController) UpdateSession(c *gin.Context) {
	var req UpdateSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "invalid request body")
		return
	}

	if err := ctrl.service.UpdateSession(c.Param("sessionId"), req.AgentName, req.AvatarURL); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"session_id": c.Param("sessionId")})
}
