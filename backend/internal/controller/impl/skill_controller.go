package impl

import (
	"io"
	"path/filepath"
	"strings"

	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type SkillController struct {
	service service.SkillService
}

func NewSkillController(skillService service.SkillService) *SkillController {
	return &SkillController{service: skillService}
}

type ConfirmSkillReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	TmpDir      string `json:"tmp_dir"`
}

type ImportSkillReq struct {
	SessionID string `json:"session_id" binding:"required"`
}

func (ctrl *SkillController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/skills/upload", ctrl.Upload)
	rg.POST("/skills/confirm", ctrl.Confirm)
	rg.GET("/skills", ctrl.List)
	rg.DELETE("/skills/:name", ctrl.Delete)
	rg.POST("/skills/:name/import", ctrl.Import)
	rg.DELETE("/skills/:name/sessions/:sessionId", ctrl.Remove)
	rg.POST("/internal/builtin-skills", ctrl.ReportBuiltinSkills)
}

func (ctrl *SkillController) Upload(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		vo.BadRequest(c, "file is required")
		return
	}
	defer file.Close()

	if strings.ToLower(filepath.Ext(header.Filename)) != ".zip" {
		vo.BadRequest(c, "skill package must be a .zip file")
		return
	}

	zipData, err := io.ReadAll(io.LimitReader(file, service.MaxUnzipSize+1))
	if err != nil {
		vo.InternalError(c, "read file failed")
		return
	}
	if int64(len(zipData)) > service.MaxUnzipSize {
		vo.BadRequest(c, "file size exceeds 10MB limit")
		return
	}

	result, err := ctrl.service.UploadSkill(header.Filename, zipData)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *SkillController) Confirm(c *gin.Context) {
	var req ConfirmSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "name is required")
		return
	}

	result, err := ctrl.service.ConfirmSkill(req.Name, req.Description, req.FileCount, req.TotalSize, req.TmpDir)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *SkillController) List(c *gin.Context) {
	items, err := ctrl.service.ListSkills()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, items)
}

func (ctrl *SkillController) Delete(c *gin.Context) {
	if err := ctrl.service.DeleteSkill(c.Param("name")); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"success": true})
}

func (ctrl *SkillController) Import(c *gin.Context) {
	var req ImportSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "session_id is required")
		return
	}

	result, err := ctrl.service.ImportSkill(c.Param("name"), req.SessionID)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *SkillController) Remove(c *gin.Context) {
	result, err := ctrl.service.RemoveSkill(c.Param("name"), c.Param("sessionId"))
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *SkillController) ReportBuiltinSkills(c *gin.Context) {
	var skills []service.BuiltinSkillItem
	if err := c.ShouldBindJSON(&skills); err != nil {
		vo.BadRequest(c, "invalid request")
		return
	}

	if err := ctrl.service.ReportBuiltinSkills(skills); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"success": true, "count": len(skills)})
}
