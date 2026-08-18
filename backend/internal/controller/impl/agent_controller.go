package impl

import (
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type AgentController struct{}

func NewAgentController() *AgentController {
	return &AgentController{}
}

var agentTypes = []string{"claude-code", "opencode", "orchestrator", "codex", "pi"}

func (ctrl *AgentController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/agent-types", ctrl.ListAgentTypes)
}

func (ctrl *AgentController) ListAgentTypes(c *gin.Context) {
	vo.OK(c, agentTypes)
}
