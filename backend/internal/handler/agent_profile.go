package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

type AgentSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Source      string `json:"source"`
}

// TODO: fetch skills from agentend when a skills API is available
var noSkills = []AgentSkill{}

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
	RepoPath      string       `json:"repo_path,omitempty"`
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
		Skills:    noSkills,
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
		RepoPath:      task.RepoPath,
		WorkspacePath: filepath.Join(task.RepoPath, session.TaskID, session.SessionID),
		CreatedAt:     session.CreatedAt,
		MessageCount:  messageCount,
		Skills:        noSkills,
	})
}
