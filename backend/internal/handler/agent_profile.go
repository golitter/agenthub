package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"
	"time"

	"github.com/gin-gonic/gin"
)

type AgentSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Source      string `json:"source"`
}

var mockSkills = []AgentSkill{
	{
		Name:        "taskctl",
		Description: "任务管理技能：创建、追踪和编排 Agent 任务。支持任务的创建、查询、状态更新，以及多 Agent 之间的任务分派与协调。",
		Builtin:     true,
		Source:      "agentend/skills/taskctl",
	},
	{
		Name:        "render",
		Description: "内容渲染技能：将 Agent 输出格式化为可交互卡片。支持 HTML 渲染、Diff 高亮、图片展示、文件预览等多种输出格式。",
		Builtin:     true,
		Source:      "agentend/skills/render",
	},
}

type AgentProfileResponse struct {
	AgentName string       `json:"agent_name"`
	AgentType string       `json:"agent_type"`
	AvatarURL string       `json:"avatar_url,omitempty"`
	Status    string       `json:"status"`
	SessionID string       `json:"session_id"`
	Skills    []AgentSkill `json:"skills"`
}

type AgentDetailResponse struct {
	AgentName     string       `json:"agent_name"`
	AgentType     string       `json:"agent_type"`
	AvatarURL     string       `json:"avatar_url,omitempty"`
	Status        string       `json:"status"`
	SessionID     string       `json:"session_id"`
	TaskID        string       `json:"task_id"`
	WorkspacePath string       `json:"workspace_path,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	MessageCount  int64        `json:"message_count"`
	Skills        []AgentSkill `json:"skills"`
}

type AgentProfileHandler struct{}

func NewAgentProfileHandler() *AgentProfileHandler {
	return &AgentProfileHandler{}
}

func (h *AgentProfileHandler) GetProfile(c *gin.Context) {
	sessionID := c.Param("sessionId")

	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}

	vo.OK(c, AgentProfileResponse{
		AgentName: session.AgentName,
		AgentType: session.AgentType,
		AvatarURL: session.AvatarURL,
		Status:    session.Status,
		SessionID: session.SessionID,
		Skills:    mockSkills,
	})
}

func (h *AgentProfileHandler) GetDetail(c *gin.Context) {
	sessionID := c.Param("sessionId")

	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}

	var task model.Task
	if err := db.GetDB().Where("task_id = ?", session.TaskID).First(&task).Error; err != nil {
		vo.NotFound(c, "task not found")
		return
	}

	var messageCount int64
	db.GetDB().Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&messageCount)

	vo.OK(c, AgentDetailResponse{
		AgentName:     session.AgentName,
		AgentType:     session.AgentType,
		AvatarURL:     session.AvatarURL,
		Status:        session.Status,
		SessionID:     session.SessionID,
		TaskID:        session.TaskID,
		WorkspacePath: task.RepoPath,
		CreatedAt:     session.CreatedAt,
		MessageCount:  messageCount,
		Skills:        mockSkills,
	})
}
