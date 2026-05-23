package handler

import (
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type AgentHandler struct{}

func NewAgentHandler() *AgentHandler {
	return &AgentHandler{}
}

var agentTypes = []string{"claude-code", "opencode", "orchestrator"}

func (h *AgentHandler) ListAgents(c *gin.Context) {
	vo.OK(c, agentTypes)
}
