package impl

import (
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type AgentProfileController struct {
	service service.AgentProfileService
}

func NewAgentProfileController(agentProfileService service.AgentProfileService) *AgentProfileController {
	return &AgentProfileController{service: agentProfileService}
}

type UpdateSoulReq struct {
	SoulMD string `json:"soul_md"`
}

func (ctrl *AgentProfileController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/sessions/:sessionId/profile", ctrl.GetProfile)
	rg.GET("/sessions/:sessionId/detail", ctrl.GetDetail)
	rg.GET("/sessions/:sessionId/soul", ctrl.GetSoul)
	rg.PUT("/sessions/:sessionId/soul", ctrl.UpdateSoul)
}

func (ctrl *AgentProfileController) GetProfile(c *gin.Context) {
	profile, err := ctrl.service.GetProfile(c.Param("sessionId"))
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, profile)
}

func (ctrl *AgentProfileController) GetDetail(c *gin.Context) {
	detail, err := ctrl.service.GetDetail(c.Param("sessionId"))
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, detail)
}

func (ctrl *AgentProfileController) GetSoul(c *gin.Context) {
	soulMD, err := ctrl.service.GetSoul(c.Param("sessionId"))
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"soul_md": soulMD, "session_id": c.Param("sessionId")})
}

func (ctrl *AgentProfileController) UpdateSoul(c *gin.Context) {
	var req UpdateSoulReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "soul_md is required")
		return
	}

	if err := ctrl.service.UpdateSoul(c.Param("sessionId"), req.SoulMD); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"success": true, "session_id": c.Param("sessionId")})
}
