package impl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type SkillController struct {
	service service.SkillService
	tempDir string
}

type skillUploadAdmission interface {
	AcquireSkillUpload(context.Context) (func(), error)
	CheckSkillTempSpace() error
}

func NewSkillController(skillService service.SkillService, tempDirs ...string) *SkillController {
	tempDir := ""
	if len(tempDirs) > 0 {
		tempDir = tempDirs[0]
	}
	return &SkillController{service: skillService, tempDir: tempDir}
}

type ConfirmSkillReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	TmpDir      string `json:"tmp_dir"`
	UploadID    string `json:"upload_id"`
}

type ImportSkillReq struct {
	SessionID string `json:"session_id" binding:"required"`
}

func (ctrl *SkillController) RegisterRoutes(rg *gin.RouterGroup) {
	managed := rg.Group("")
	managed.Use(middleware.NewIPRateLimiter(30, time.Minute).Middleware())
	ctrl.registerManagedRoutes(managed)
	ctrl.registerPublicRoutes(rg)
}

// RegisterRoutesWithManagerAuth keeps read/import APIs available to normal
// authenticated users while putting all Skill Hub mutations behind the
// existing Admin JWT middleware.  The check lives at the router boundary so
// direct API calls cannot bypass a UI permission guard.
func (ctrl *SkillController) RegisterRoutesWithManagerAuth(rg *gin.RouterGroup, managerAuth gin.HandlerFunc) {
	managed := rg.Group("")
	managed.Use(managerAuth, middleware.NewIPRateLimiter(30, time.Minute).Middleware())
	ctrl.registerManagedRoutes(managed)
	ctrl.registerPublicRoutes(rg)
}

func (ctrl *SkillController) registerManagedRoutes(rg *gin.RouterGroup) {
	rg.POST("/skills/upload", ctrl.Upload)
	rg.POST("/skills/confirm", ctrl.Confirm)
	rg.DELETE("/skills/:name", ctrl.Delete)
}

func (ctrl *SkillController) registerPublicRoutes(rg *gin.RouterGroup) {
	rg.GET("/skills", ctrl.List)
	rg.POST("/skills/:name/import", ctrl.Import)
	rg.DELETE("/skills/:name/sessions/:sessionId", ctrl.Remove)
	rg.POST("/internal/builtin-skills", ctrl.ReportBuiltinSkills)
}

func (ctrl *SkillController) Upload(c *gin.Context) {
	if admission, ok := ctrl.service.(skillUploadAdmission); ok {
		release, err := admission.AcquireSkillUpload(c.Request.Context())
		if err != nil {
			vo.ServiceUnavailable(c, "skill upload admission is unavailable")
			return
		}
		defer release()
		if err := admission.CheckSkillTempSpace(); err != nil {
			vo.ServiceUnavailable(c, "Skill temporary volume is low on space")
			return
		}
	}
	// Limit the complete multipart request before FormFile parses headers and
	// body parts.  Limiting only the extracted file would still allow an
	// oversized multipart envelope to consume memory and disk.
	maxUpload := ctrl.service.SkillUploadLimit()
	if maxUpload <= 0 {
		maxUpload = service.MaxUploadSize
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUpload+1<<20)
	multipartReader, err := c.Request.MultipartReader()
	if err != nil {
		vo.BadRequest(c, "multipart file is required")
		return
	}
	var inputPath, filename string
	var size int64
	fileFound := false
	for {
		part, partErr := multipartReader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(partErr, &maxErr) {
				vo.BadRequest(c, "skill upload request exceeds size limit")
				return
			}
			vo.BadRequest(c, "invalid multipart upload")
			return
		}
		partName := part.FormName()
		partFilename := part.FileName()
		if partName != "file" {
			// Do not persist arbitrary form fields.  Drain the part so the
			// multipart reader remains synchronized for the actual file part.
			drained, drainErr := io.Copy(io.Discard, io.LimitReader(part, maxUpload+1))
			_ = part.Close()
			if drainErr != nil {
				var maxErr *http.MaxBytesError
				if errors.As(drainErr, &maxErr) {
					vo.BadRequest(c, "skill upload request exceeds size limit")
					return
				}
				vo.BadRequest(c, "invalid multipart upload")
				return
			}
			if drained > maxUpload {
				vo.BadRequest(c, "skill upload request exceeds size limit")
				return
			}
			continue
		}
		if fileFound {
			_ = part.Close()
			vo.BadRequest(c, "only one skill file is allowed")
			return
		}
		fileFound = true
		filename = filepath.Base(partFilename)
		if strings.ToLower(filepath.Ext(filename)) != ".zip" {
			_ = part.Close()
			vo.BadRequest(c, "skill package must be a .zip file")
			return
		}
		input, createErr := os.CreateTemp(ctrl.tempDir, "skill-upload-input-*")
		if createErr != nil {
			_ = part.Close()
			vo.InternalError(c, "create upload staging file failed")
			return
		}
		inputPath = input.Name()
		defer os.Remove(inputPath)
		if chmodErr := input.Chmod(0o600); chmodErr != nil {
			_ = input.Close()
			_ = part.Close()
			vo.InternalError(c, "secure upload staging file failed")
			return
		}
		size, err = io.Copy(input, io.LimitReader(part, maxUpload+1))
		partCloseErr := part.Close()
		if err == nil {
			err = partCloseErr
		}
		if closeErr := input.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				vo.BadRequest(c, "skill upload request exceeds size limit")
				return
			}
			vo.InternalError(c, "read file failed")
			return
		}
		if size > maxUpload {
			vo.BadRequest(c, "file size exceeds configured upload limit")
			return
		}
	}
	if !fileFound || inputPath == "" {
		vo.BadRequest(c, "file is required")
		return
	}

	result, err := ctrl.service.UploadSkillFile(skillRequestContext(c), filename, inputPath, size)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *SkillController) Confirm(c *gin.Context) {
	var req ConfirmSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "invalid confirmation request")
		return
	}
	// The minio: pseudo-path is an internal server representation derived from
	// upload_id.  Accepting it from tmp_dir would bypass the legacy-confirm
	// rollout gate and let callers address arbitrary incoming object keys.
	if req.UploadID == "" && strings.HasPrefix(strings.TrimSpace(req.TmpDir), "minio:") {
		vo.BadRequest(c, "upload_id is required")
		return
	}
	if req.UploadID == "" && (req.TmpDir == "" || req.Name == "") {
		vo.BadRequest(c, "upload_id is required")
		return
	}

	tmpDir := req.TmpDir
	if req.UploadID != "" {
		if err := service.ValidateSkillUploadID(req.UploadID); err != nil {
			vo.BadRequest(c, "invalid upload_id")
			return
		}
		tmpDir = "minio:incoming/" + req.UploadID + ".zip"
	}
	result, err := ctrl.service.ConfirmSkill(skillRequestContext(c), req.Name, req.Description, req.FileCount, req.TotalSize, tmpDir)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func skillRequestContext(c *gin.Context) context.Context {
	isAdmin, _ := c.Get("isAdmin")
	admin := isAdmin == true
	ownerID, _ := c.Get("userID")
	owner := skillOwnerString(ownerID)
	if owner == "" && admin {
		owner = "admin"
	}
	if owner == "" {
		// Authentication may expose only a stable username in deployments that
		// do not issue numeric user IDs.  Keep the owner binding deterministic.
		if userName, ok := c.Get("userName"); ok {
			owner, _ = userName.(string)
		}
	}
	return service.WithSkillAdmin(service.WithSkillOwner(c.Request.Context(), owner), admin)
}

func skillOwnerString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
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
	if err := ctrl.service.DeleteSkill(skillRequestContext(c), c.Param("name")); err != nil {
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

	result, err := ctrl.service.ImportSkill(skillRequestContext(c), c.Param("name"), req.SessionID)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *SkillController) Remove(c *gin.Context) {
	result, err := ctrl.service.RemoveSkill(skillRequestContext(c), c.Param("name"), c.Param("sessionId"))
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
