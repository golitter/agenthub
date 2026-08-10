package app

import (
	"context"
	"log/slog"
	"time"

	"agenthub/backend/internal/conf"
	ctrlimpl "agenthub/backend/internal/controller/impl"
	"agenthub/backend/internal/dao"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/middleware"
	skillservice "agenthub/backend/internal/service"
	"agenthub/backend/internal/service/impl"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/package_store"
	"agenthub/backend/pkg/skill_upload_session"
	"agenthub/backend/pkg/storage"

	"github.com/gin-gonic/gin"
)

const maxJSONBodySize = 1 << 20

type Dependencies struct {
	Config             *conf.Config
	AgentClient        *agentend_client.Client
	StorageProvider    storage.Provider
	PackageStore       package_store.PackageStore
	UploadSessionStore *skill_upload_session.Store
	OperationDao       dao.SkillOperationDao
}

func NewRouter(deps Dependencies) *gin.Engine {
	sessionDao := gormdao.NewSessionDao()
	taskDao := gormdao.NewTaskDao()
	messageDao := gormdao.NewMessageDao()
	diffSnapshotDao := gormdao.NewDiffSnapshotDao()
	announcementDao := gormdao.NewAnnouncementDao()
	contactGroupDao := gormdao.NewContactGroupDao()
	skillDao := gormdao.NewSkillDao()
	adminDao := gormdao.NewAdminDao()

	sessionService := impl.NewSessionService(sessionDao)
	taskService := impl.NewTaskService(taskDao, sessionDao, messageDao, diffSnapshotDao, deps.AgentClient)
	messageService := impl.NewMessageService(taskDao, sessionDao, messageDao)
	avatarService := impl.NewAvatarService(sessionDao, deps.StorageProvider)
	streamService := impl.NewStreamService(messageDao)
	agentProfileService := impl.NewAgentProfileService(sessionDao, taskDao, messageDao, skillDao, deps.AgentClient)
	diffSnapshotService := impl.NewDiffSnapshotService(diffSnapshotDao)
	announcementService := impl.NewAnnouncementService(announcementDao, taskDao, deps.AgentClient)
	contactGroupService := impl.NewContactGroupService(contactGroupDao)
	skillService := impl.NewSkillService(skillDao, sessionDao, deps.AgentClient, deps.PackageStore)
	skillService.SetUploadSessionStore(deps.UploadSessionStore)
	skillService.SetOperationDao(deps.OperationDao)
	if deps.Config != nil {
		skillService.SetSkillTempDir(deps.Config.SkillStorage.TempDir)
		var scanner skillservice.SkillContentScanner
		if raw := deps.Config.SkillStorage.ContentScanCommand; raw != "" {
			timeout := 2 * time.Minute
			if configured := deps.Config.SkillStorage.ContentScanTimeout; configured != "" {
				if parsed, err := time.ParseDuration(configured); err == nil {
					timeout = parsed
				}
			}
			configuredScanner, err := skillservice.NewCommandSkillScanner(raw, timeout)
			if err != nil {
				slog.Warn("invalid Skill content scanner configuration", "error", err)
			} else {
				scanner = configuredScanner
			}
		}
		skillService.SetContentPolicy(deps.Config.SkillStorage.RejectBinaries, deps.Config.SkillStorage.RejectExecutables, scanner)
	}
	if deps.Config != nil {
		timeout := 2 * time.Minute
		if raw := deps.Config.SkillStorage.ValidationTimeout; raw != "" {
			if parsed, err := time.ParseDuration(raw); err == nil {
				timeout = parsed
			} else {
				slog.Warn("invalid Skill validation timeout; using default", "value", raw, "error", err)
			}
		}
		concurrency := deps.Config.SkillStorage.MaxConcurrentValidations
		if concurrency <= 0 {
			concurrency = 4
		}
		skillService.SetValidationLimits(concurrency, timeout)
		limits := skillservice.DefaultZipLimits()
		if raw := deps.Config.SkillStorage.MaxUploadSize; raw != "" {
			if parsed, err := conf.ParseByteSize(raw); err == nil {
				limits.UploadSize = parsed
			} else {
				slog.Warn("invalid Skill upload limit; using default", "value", raw, "error", err)
			}
		}
		if raw := deps.Config.SkillStorage.MaxPackageSize; raw != "" {
			if parsed, err := conf.ParseByteSize(raw); err == nil {
				limits.PackageSize = parsed
			} else {
				slog.Warn("invalid Skill package limit; using default", "value", raw, "error", err)
			}
		}
		if raw := deps.Config.SkillStorage.MaxFileSize; raw != "" {
			if parsed, err := conf.ParseByteSize(raw); err == nil {
				limits.FileSize = parsed
			} else {
				slog.Warn("invalid Skill per-file limit; using default", "value", raw, "error", err)
			}
		}
		if raw := deps.Config.SkillStorage.MaxUnpackedSize; raw != "" {
			if parsed, err := conf.ParseByteSize(raw); err == nil {
				limits.UnpackedSize = parsed
			} else {
				slog.Warn("invalid Skill unpacked limit; using default", "value", raw, "error", err)
			}
		}
		if deps.Config.SkillStorage.MaxCompressionRatio > 0 {
			limits.CompressionRatio = deps.Config.SkillStorage.MaxCompressionRatio
		}
		if deps.Config.SkillStorage.MaxFileCount > 0 {
			limits.FileCount = deps.Config.SkillStorage.MaxFileCount
		}
		if raw := deps.Config.SkillStorage.MinTempFreeBytes; raw != "" {
			if parsed, err := conf.ParseByteSize(raw); err == nil {
				skillService.SetTempMinFreeBytes(parsed)
			} else {
				slog.Warn("invalid Skill temp free-space reserve; using default", "value", raw, "error", err)
			}
		}
		skillService.SetZipLimits(limits)
		if deps.Config.SkillStorage.Enabled {
			skillService.SetStorageReadOptions(deps.Config.SkillStorage.ReadPreference, deps.Config.SkillStorage.ShadowWriteBlob)
			skillService.SetLegacyTmpConfirmAllowed(deps.Config.SkillStorage.AllowLegacyTmpConfirm)
		}
	}
	adminService := impl.NewAdminService(deps.Config, adminDao, sessionDao, deps.AgentClient)

	taskController := ctrlimpl.NewTaskController(taskService, deps.AgentClient)
	agentController := ctrlimpl.NewAgentController()
	sessionController := ctrlimpl.NewSessionController(sessionService)
	messageController := ctrlimpl.NewMessageController(messageService)
	avatarController := ctrlimpl.NewAvatarController(avatarService)
	streamController := ctrlimpl.NewStreamController(streamService)
	agentProfileController := ctrlimpl.NewAgentProfileController(agentProfileService)
	workspaceController := ctrlimpl.NewWorkspaceController(deps.AgentClient)
	diffSnapshotController := ctrlimpl.NewDiffSnapshotController(diffSnapshotService)
	announcementController := ctrlimpl.NewAnnouncementController(announcementService)
	contactGroupController := ctrlimpl.NewContactGroupController(contactGroupService)
	tempDir := ""
	if deps.Config != nil {
		// The legacy DB-BLOB path still writes and validates uploads locally;
		// keep it on the same private, configured volume even when MinIO is
		// feature-gated off.
		tempDir = deps.Config.SkillStorage.TempDir
	}
	skillController := ctrlimpl.NewSkillController(skillService, tempDir)
	adminController := ctrlimpl.NewAdminController(deps.Config, adminService)

	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.CORS(deps.Config.CORS.AllowOrigins))
	r.Use(gin.Recovery())

	if local, ok := deps.StorageProvider.(*storage.LocalStorage); ok {
		r.Static("/uploads", local.Dir())
		slog.Info("serving local uploads", "dir", local.Dir())
	}

	r.GET("/ping", func(c *gin.Context) {
		vo.OK(c, gin.H{"message": "pong"})
	})

	r.GET("/health", func(c *gin.Context) {
		vo.OK(c, gin.H{"status": "ok"})
	})
	r.GET("/ready", func(c *gin.Context) {
		storageEnabled := deps.Config != nil && deps.Config.SkillStorage.Enabled
		if storageEnabled {
			checker, hasChecker := deps.PackageStore.(interface{ Health(context.Context) error })
			if !hasChecker {
				c.JSON(503, gin.H{"code": 503, "msg": "skill storage is not ready"})
				return
			}
			ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
			err := checker.Health(ctx)
			cancel()
			if err != nil {
				c.JSON(503, gin.H{"code": 503, "msg": "skill storage is not ready"})
				return
			}
		}
		vo.OK(c, gin.H{"status": "ready"})
	})

	api := r.Group("/api")
	api.Use(middleware.JSONBodyLimit(maxJSONBodySize, "/api/workspace"))
	if deps.Config.Auth.Enabled {
		api.Use(middleware.AuthWithSkips(
			deps.Config.JWT.Secret,
			"/api/admin/auth",
			"/api/admin/health",
			"/api/admin/avatar",
		))
	}

	taskController.RegisterRoutes(api)
	streamController.RegisterRoutes(api)
	messageController.RegisterRoutes(api)
	agentController.RegisterRoutes(api)
	announcementController.RegisterRoutes(api)
	sessionController.RegisterRoutes(api)
	avatarController.RegisterRoutes(api)
	agentProfileController.RegisterRoutes(api)
	diffSnapshotController.RegisterRoutes(api)
	contactGroupController.RegisterRoutes(api)
	if deps.Config != nil && deps.Config.SkillStorage.RequireAdmin {
		skillController.RegisterRoutesWithManagerAuth(api, middleware.AdminAuth(deps.Config.JWT.Secret))
	} else {
		skillController.RegisterRoutes(api)
	}
	workspaceController.RegisterRoutes(api)
	adminController.RegisterRoutes(api)

	return r
}
