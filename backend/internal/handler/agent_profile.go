package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"
	"fmt"
	"log/slog"
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

type AgentProfileResponse struct {
	AgentName string       `json:"agent_name"`
	AgentType string       `json:"agent_type"`
	AvatarURL string       `json:"avatar_url,omitempty"`
	Status    string       `json:"status"`
	SessionID string       `json:"session_id"`
	SoulMD    string       `json:"soul_md,omitempty"`
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
	SoulMD        string       `json:"soul_md,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	MessageCount  int64        `json:"message_count"`
	Skills        []AgentSkill `json:"skills"`
}

type AgentProfileHandler struct {
	agentClient *agentend_client.Client
}

func NewAgentProfileHandler(agentClient *agentend_client.Client) *AgentProfileHandler {
	return &AgentProfileHandler{agentClient: agentClient}
}

func (h *AgentProfileHandler) GetProfile(c *gin.Context) {
	sessionID := c.Param("sessionId")

	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}

	skills := h.fetchSkills(session.AgentType, session.TaskID, sessionID)

	vo.OK(c, AgentProfileResponse{
		AgentName: session.AgentName,
		AgentType: session.AgentType,
		AvatarURL: session.AvatarURL,
		Status:    session.Status,
		SessionID: session.SessionID,
		SoulMD:    session.SoulMD,
		Skills:    skills,
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
		SoulMD:        session.SoulMD,
		CreatedAt:     session.CreatedAt,
		MessageCount:  messageCount,
		Skills:        h.fetchSkills(session.AgentType, session.TaskID, sessionID),
	})
}

// fetchSkills 从 Agentend 实时读取 skills（通过 session_id 让 Agentend 自行解析 worktree），失败或返回空时 fallback 到数据库
func (h *AgentProfileHandler) fetchSkills(agentType, taskID, sessionID string) []AgentSkill {
	// 尝试从 Agentend 实时读取（Agentend 通过 session_id 查找 worktree）
	skillInfos, err := h.agentClient.FetchSkills(agentType, sessionID)
	if err != nil {
		slog.Debug("fetch skills from agentend failed, fallback to db", "error", err)
		return h.fetchSkillsFromDB(sessionID)
	}

	// Agentend 返回空（可能 workspace 已清理），fallback 到数据库
	if len(skillInfos) == 0 {
		return h.fetchSkillsFromDB(sessionID)
	}

	skills := make([]AgentSkill, 0, len(skillInfos))
	for _, s := range skillInfos {
		skills = append(skills, AgentSkill{
			Name:        s.Name,
			Description: s.Description,
			Builtin:     s.Builtin,
			Source:      s.Source,
		})
	}

	// 同步回 DB，确保 fallback 路径也能拿到最新 skills
	h.syncSkillsToDB(agentType, sessionID, skills)

	return skills
}

// syncSkillsToDB 将实时扫描到的 skills 写回数据库（builtin → skill_hub, external → agent_skill）
func (h *AgentProfileHandler) syncSkillsToDB(agentType, sessionID string, skills []AgentSkill) {
	for _, s := range skills {
		if s.Builtin {
			// builtin skill: upsert to skill_hub
			skill := model.SkillHub{
				Name:        s.Name,
				Builtin:     true,
				Description: s.Description,
			}
			db.GetDB().Where("name = ?", s.Name).Assign(map[string]interface{}{
				"description": s.Description,
				"builtin":     true,
			}).FirstOrCreate(&skill)
		} else {
			// external skill: upsert to skill_hub + agent_skill
			skill := model.SkillHub{
				Name:        s.Name,
				Builtin:     false,
				Description: s.Description,
			}
			db.GetDB().Where("name = ?", s.Name).Assign(map[string]interface{}{
				"description": s.Description,
				"builtin":     false,
			}).FirstOrCreate(&skill)

			// 确保 session 关联存在
			var rel model.AgentSkill
			result := db.GetDB().Where("session_id = ? AND skill_name = ?", sessionID, s.Name).First(&rel)
			if result.Error != nil {
				db.GetDB().Create(&model.AgentSkill{
					SessionID:  sessionID,
					SkillName:  s.Name,
					AgentType:  agentType,
					ImportedAt: time.Now(),
				})
			}
		}
	}
}

// fetchSkillsFromDB 从数据库读取 builtin skills + session 已导入的 external skills
func (h *AgentProfileHandler) fetchSkillsFromDB(sessionID string) []AgentSkill {
	var skills []AgentSkill

	// 1. 所有 builtin skills
	var builtins []model.SkillHub
	db.GetDB().Where("builtin = ?", true).Find(&builtins)
	for _, s := range builtins {
		skills = append(skills, AgentSkill{
			Name:        s.Name,
			Description: s.Description,
			Builtin:     true,
			Source:      "builtin",
		})
	}

	// 2. 该 session 已导入的 external skills
	var relations []model.AgentSkill
	db.GetDB().Where("session_id = ?", sessionID).Find(&relations)
	for _, r := range relations {
		var hub model.SkillHub
		if err := db.GetDB().Where("name = ? AND builtin = false", r.SkillName).First(&hub).Error; err != nil {
			continue
		}
		skills = append(skills, AgentSkill{
			Name:        hub.Name,
			Description: hub.Description,
			Builtin:     false,
			Source:      "hub",
		})
	}

	return skills
}

func (h *AgentProfileHandler) GetSoul(c *gin.Context) {
	sessionID := c.Param("sessionId")
	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}
	vo.OK(c, gin.H{"soul_md": session.SoulMD, "session_id": sessionID})
}

type UpdateSoulReq struct {
	SoulMD string `json:"soul_md"`
}

func (h *AgentProfileHandler) UpdateSoul(c *gin.Context) {
	sessionID := c.Param("sessionId")
	var req UpdateSoulReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "soul_md is required")
		return
	}
	stripped := stripSpaces(req.SoulMD)
	if len([]rune(stripped)) > 300 {
		vo.BadRequest(c, fmt.Sprintf("soul_md must not exceed 300 characters, got %d", len([]rune(stripped))))
		return
	}
	result := db.GetDB().Model(&model.Session{}).Where("session_id = ?", sessionID).Update("soul_md", stripped)
	if result.RowsAffected == 0 {
		vo.NotFound(c, "session not found")
		return
	}
	vo.OK(c, gin.H{"success": true, "session_id": sessionID})
}

func stripSpaces(s string) string {
	var result []rune
	for _, r := range s {
		if r != ' ' {
			result = append(result, r)
		}
	}
	return string(result)
}
