package impl

import (
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type AnnouncementController struct {
	service service.AnnouncementService
}

func NewAnnouncementController(announcementService service.AnnouncementService) *AnnouncementController {
	return &AnnouncementController{service: announcementService}
}

type CreateAnnouncementReq struct {
	SenderID   string `json:"sender_id" binding:"required"`
	SenderName string `json:"sender_name" binding:"required"`
	Content    string `json:"content" binding:"required"`
	Pinned     bool   `json:"pinned"`
}

func (ctrl *AnnouncementController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/tasks/:taskId/announcements", ctrl.ListAnnouncements)
	rg.POST("/tasks/:taskId/announcements", ctrl.CreateAnnouncement)
	rg.DELETE("/tasks/:taskId/announcements/:id", ctrl.DeleteAnnouncement)
}

func (ctrl *AnnouncementController) RegisterInternalReadRoutes(rg *gin.RouterGroup) {
	rg.GET("/tasks/:taskId/announcements", ctrl.ListAnnouncements)
}

func (ctrl *AnnouncementController) ListAnnouncements(c *gin.Context) {
	announcements, err := ctrl.service.ListAnnouncements(c.Param("taskId"), c.Query("pinned") == "true")
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, announcements)
}

func (ctrl *AnnouncementController) CreateAnnouncement(c *gin.Context) {
	var req CreateAnnouncementReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "content is required")
		return
	}

	announcement, err := ctrl.service.CreateAnnouncement(c.Param("taskId"), service.CreateAnnouncementInput{
		SenderID:   req.SenderID,
		SenderName: req.SenderName,
		Content:    req.Content,
		Pinned:     req.Pinned,
	})
	if err != nil {
		handleBizError(c, err)
		return
	}

	vo.Created(c, announcement)
}

func (ctrl *AnnouncementController) DeleteAnnouncement(c *gin.Context) {
	if err := ctrl.service.DeleteAnnouncement(c.Param("id")); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, nil)
}
