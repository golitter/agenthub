package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type SessionHandler struct{}

func NewSessionHandler() *SessionHandler {
	return &SessionHandler{}
}

type CreateSessionReq struct {
	Title string `json:"title" binding:"required"`
}

func (h *SessionHandler) CreateSession(c *gin.Context) {
	var req CreateSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "title is required")
		return
	}
	s := model.Session{
		SessionID: uuid.New().String(),
		Title:     req.Title,
		Status:    "active",
	}
	if err := db.GetDB().Create(&s).Error; err != nil {
		vo.InternalError(c, err.Error())
		return
	}
	vo.Created(c, s)
}

func (h *SessionHandler) ListSessions(c *gin.Context) {
	var sessions []model.Session
	if err := db.GetDB().Order("created_at DESC").Find(&sessions).Error; err != nil {
		vo.InternalError(c, err.Error())
		return
	}
	vo.OK(c, sessions)
}

func (h *SessionHandler) GetSession(c *gin.Context) {
	var s model.Session
	if err := db.GetDB().Where("session_id = ?", c.Param("id")).First(&s).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}
	vo.OK(c, s)
}

func (h *SessionHandler) DeleteSession(c *gin.Context) {
	result := db.GetDB().Where("session_id = ?", c.Param("id")).Delete(&model.Session{})
	if result.RowsAffected == 0 {
		vo.NotFound(c, "session not found")
		return
	}
	vo.OK(c, nil)
}
