package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type SkillHandler struct {
	agentClient *agentend_client.Client
}

func NewSkillHandler(agentClient *agentend_client.Client) *SkillHandler {
	return &SkillHandler{agentClient: agentClient}
}

// ── Upload: 上传 zip，返回校验结果 ──

func (h *SkillHandler) Upload(c *gin.Context) {
	file, handler, err := c.Request.FormFile("file")
	if err != nil {
		vo.BadRequest(c, "file is required")
		return
	}
	defer file.Close()

	zipData, err := io.ReadAll(file)
	if err != nil {
		vo.InternalError(c, "read file failed")
		return
	}

	result, tmpDir, _ := service.ValidateZip(zipData)
	if result.Valid {
		// 校验 zip 文件名（去掉 .zip 后缀）必须等于 SKILL.md 的 name
		zipName := strings.TrimSuffix(handler.Filename, ".zip")
		if zipName != result.Name {
			os.RemoveAll(tmpDir)
			vo.BadRequest(c, fmt.Sprintf("zip filename (%s) must match SKILL.md name (%s)", zipName, result.Name))
			return
		}
	}

	// 保存 tmpDir 到上下文供 confirm 使用
	if tmpDir != "" {
		c.Set("tmpDir", tmpDir)
	}

	vo.OK(c, result)
}

// ── Confirm: 确认入库 ──

type ConfirmSkillReq struct {
	Name string `json:"name" binding:"required"`
	// 以下字段来自校验结果，前端也需要传回来
	Description string `json:"description"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	TmpDir      string `json:"tmp_dir"`
}

func (h *SkillHandler) Confirm(c *gin.Context) {
	var req ConfirmSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "name is required")
		return
	}
	slog.Info("confirm skill", "name", req.Name, "tmpDir", req.TmpDir)

	// 检查名称是否已存在
	var count int64
	db.GetDB().Model(&model.SkillHub{}).Where("name = ?", req.Name).Count(&count)
	if count > 0 {
		vo.Conflict(c, "skill name already exists")
		return
	}

	if err := service.ConfirmSkill(req.Name, req.Description, req.FileCount, req.TotalSize, req.TmpDir); err != nil {
		vo.InternalError(c, err.Error())
		return
	}

	vo.OK(c, gin.H{"success": true, "name": req.Name})
}

// ── List: 列出所有 skills ──

type SkillHubItem struct {
	Name        string `json:"name"`
	Builtin     bool   `json:"builtin"`
	Description string `json:"description"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	ImportCount int64  `json:"import_count"`
	CreatedAt   string `json:"created_at"`
}

func (h *SkillHandler) List(c *gin.Context) {
	var skills []model.SkillHub
	db.GetDB().Order("builtin DESC, name ASC").Find(&skills)

	items := make([]SkillHubItem, 0, len(skills))
	for _, s := range skills {
		var importCount int64
		db.GetDB().Model(&model.AgentSkill{}).Where("skill_name = ?", s.Name).Count(&importCount)
		items = append(items, SkillHubItem{
			Name:        s.Name,
			Builtin:     s.Builtin,
			Description: s.Description,
			FileCount:   s.FileCount,
			TotalSize:   s.TotalSize,
			ImportCount: importCount,
			CreatedAt:   s.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	vo.OK(c, items)
}

// ── Delete: 从 hub 删除 (仅 external) ──

func (h *SkillHandler) Delete(c *gin.Context) {
	name := c.Param("name")
	if err := service.DeleteSkillFromHub(name); err != nil {
		if err.Error() == "cannot delete builtin skill" {
			vo.Forbidden(c, err.Error())
			return
		}
		vo.NotFound(c, err.Error())
		return
	}
	vo.OK(c, gin.H{"success": true})
}

// ── Import: 导入 skill 到指定 session（委托 Agentend 写文件）──

type ImportSkillReq struct {
	SessionID string `json:"session_id" binding:"required"`
}

func (h *SkillHandler) Import(c *gin.Context) {
	skillName := c.Param("name")

	var req ImportSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "session_id is required")
		return
	}

	// 查找 session
	var session model.Session
	if err := db.GetDB().Where("session_id = ?", req.SessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}

	// 校验 agent_type：仅 adapter 层可导入
	agentType := session.AgentType
	allowedTypes := map[string]bool{"claude-code": true, "opencode": true, "codex": true}
	if !allowedTypes[agentType] {
		vo.Forbidden(c, "orchestrator does not support importing external skills")
		return
	}

	// 查找 hub 中的 skill
	var skill model.SkillHub
	if err := db.GetDB().Where("name = ?", skillName).First(&skill).Error; err != nil {
		vo.NotFound(c, "skill not found in hub")
		return
	}

	// 检查是否已导入
	var existCount int64
	db.GetDB().Model(&model.AgentSkill{}).Where("session_id = ? AND skill_name = ?", req.SessionID, skillName).Count(&existCount)
	if existCount > 0 {
		vo.Conflict(c, "skill already imported to this session")
		return
	}

	// 将 hub 中的 skill 文件打包为 zip，发给 Agentend 安装到 worktree
	srcPath := filepath.Join(service.HubBasePath, skillName)
	zipData, err := service.PackSkillDir(srcPath)
	if err != nil {
		vo.InternalError(c, "pack skill files failed: "+err.Error())
		return
	}

	if err := h.agentClient.InstallSkill(agentType, req.SessionID, skillName, zipData); err != nil {
		vo.InternalError(c, "install skill to worktree failed: "+err.Error())
		return
	}

	// 写入 agent_skill 表
	db.GetDB().Create(&model.AgentSkill{
		SessionID: req.SessionID,
		SkillName: skillName,
		AgentType: agentType,
	})

	vo.OK(c, gin.H{"success": true, "skill": skillName, "session": req.SessionID})
}

// ── Remove: 从 session 移除 skill（委托 Agentend 删文件）──

func (h *SkillHandler) Remove(c *gin.Context) {
	skillName := c.Param("name")
	sessionID := c.Param("sessionId")

	// 查找 session
	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}

	// 委托 Agentend 删除 worktree 中的 skill 文件
	if err := h.agentClient.RemoveSkill(session.AgentType, sessionID, skillName); err != nil {
		vo.InternalError(c, "remove skill files from worktree failed: "+err.Error())
		return
	}

	// 删除 agent_skill 关联记录（忽略不存在的情况）
	db.GetDB().Where("session_id = ? AND skill_name = ?", sessionID, skillName).Delete(&model.AgentSkill{})

	vo.OK(c, gin.H{"success": true})
}

// ── BuiltinSkills: Agentend 启动时上报 builtin skills ──

type BuiltinSkillItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Source      string `json:"source"`
}

func (h *SkillHandler) ReportBuiltinSkills(c *gin.Context) {
	var skills []BuiltinSkillItem
	if err := c.ShouldBindJSON(&skills); err != nil {
		vo.BadRequest(c, "invalid request")
		return
	}

	for _, s := range skills {
		skill := model.SkillHub{
			Name:        s.Name,
			Builtin:     true,
			Description: s.Description,
		}
		// UPSERT: 存在则更新，不存在则插入
		db.GetDB().Where("name = ?", s.Name).Assign(map[string]interface{}{
			"description": s.Description,
			"builtin":     true,
		}).FirstOrCreate(&skill)
	}

	vo.OK(c, gin.H{"success": true, "count": len(skills)})
}
