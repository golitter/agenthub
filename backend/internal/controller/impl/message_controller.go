package impl

import (
	"strconv"

	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/service"
	svcimpl "agenthub/backend/internal/service/impl"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type MessageController struct {
	service service.MessageService
}

const maxMessageLimit = 100

func NewMessageController() *MessageController {
	taskDao := gormdao.NewTaskDao()
	sessionDao := gormdao.NewSessionDao()
	messageDao := gormdao.NewMessageDao()
	messageService := svcimpl.NewMessageService(taskDao, sessionDao, messageDao)
	return &MessageController{service: messageService}
}

func (ctrl *MessageController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/tasks/:taskId/messages", ctrl.ListMessages)
	rg.GET("/tasks/:taskId/messages/window", ctrl.WindowMessages)
}

func (ctrl *MessageController) ListMessages(c *gin.Context) {
	var beforeID *uint64
	limitStr := c.Query("limit")
	beforeStr := c.Query("before")
	sessionID := c.Query("session_id")
	mode := c.Query("mode")
	primarySessionID := c.Query("primary_session_id")
	paginated := limitStr != "" || beforeStr != ""
	limit := 20

	if limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed <= 0 || parsed > maxMessageLimit {
			vo.BadRequest(c, "limit must be an integer between 1 and 100")
			return
		}
		limit = parsed
	}
	if beforeStr != "" {
		parsed, err := strconv.ParseUint(beforeStr, 10, 64)
		if err != nil || parsed == 0 {
			vo.BadRequest(c, "before must be a positive message id")
			return
		}
		beforeID = &parsed
	}

	result, err := ctrl.service.ListMessages(c.Param("taskId"), sessionID, mode, primarySessionID, limit, beforeID, paginated)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *MessageController) WindowMessages(c *gin.Context) {
	result, err := ctrl.service.WindowMessages(c.Param("taskId"), c.Query("session_id"))
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}
