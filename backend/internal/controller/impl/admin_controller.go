package impl

import (
	"time"

	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type AdminController struct {
	service service.AdminService
	cfg     *conf.Config
}

func NewAdminController(cfg *conf.Config, adminService service.AdminService) *AdminController {
	return &AdminController{service: adminService, cfg: cfg}
}

type AuthRequest struct {
	Password string `json:"password" binding:"required"`
}

type AvatarRequest struct {
	URL string `json:"url" binding:"required"`
}

type DeleteSessionsRequest struct {
	SessionIDs []string `json:"session_ids" binding:"required"`
}

func (ctrl *AdminController) RegisterRoutes(rg *gin.RouterGroup) {
	admin := rg.Group("/admin")
	{
		authLimiter := middleware.NewIPRateLimiter(5, time.Minute)
		admin.POST("/auth", authLimiter.Middleware(), ctrl.Auth)
		admin.GET("/health", ctrl.HealthCheck)
		admin.GET("/avatar", ctrl.GetAvatar)

		protected := admin.Group("")
		protected.Use(middleware.AdminAuth(ctrl.cfg.JWT.Secret))
		{
			protected.GET("/resources", ctrl.GetResources)
			protected.DELETE("/sessions", ctrl.DeleteSessions)
			protected.GET("/workspaces", ctrl.GetWorkspaces)
			protected.DELETE("/workspaces/:id", ctrl.DeleteWorkspace)
			protected.GET("/agents", ctrl.GetAgents)
			protected.GET("/services", ctrl.GetServices)
			protected.GET("/statistics", ctrl.GetStatistics)
			protected.PUT("/avatar", ctrl.UpdateAvatar)
		}
	}
}

func (ctrl *AdminController) Auth(c *gin.Context) {
	var req AuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "password is required")
		return
	}

	result, err := ctrl.service.Auth(req.Password)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *AdminController) HealthCheck(c *gin.Context) {
	vo.OK(c, gin.H{"status": "ok"})
}

func (ctrl *AdminController) GetAvatar(c *gin.Context) {
	url, err := ctrl.service.GetAvatar()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"url": url})
}

func (ctrl *AdminController) UpdateAvatar(c *gin.Context) {
	var req AvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "url is required")
		return
	}
	if err := ctrl.service.UpdateAvatar(req.URL); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"success": true})
}

func (ctrl *AdminController) GetResources(c *gin.Context) {
	result, err := ctrl.service.GetResources()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *AdminController) DeleteSessions(c *gin.Context) {
	var req DeleteSessionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "session_ids is required")
		return
	}
	deleted, err := ctrl.service.DeleteSessions(req.SessionIDs)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"deleted": deleted})
}

func (ctrl *AdminController) GetWorkspaces(c *gin.Context) {
	result, err := ctrl.service.GetWorkspaces()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *AdminController) DeleteWorkspace(c *gin.Context) {
	if err := ctrl.service.DeleteWorkspace(c.Param("id")); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"success": true})
}

func (ctrl *AdminController) GetAgents(c *gin.Context) {
	agents, err := ctrl.service.GetAgents()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, agents)
}

func (ctrl *AdminController) GetServices(c *gin.Context) {
	vo.OK(c, ctrl.service.GetServices())
}

func (ctrl *AdminController) GetStatistics(c *gin.Context) {
	stats, err := ctrl.service.GetStatistics()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, stats)
}
