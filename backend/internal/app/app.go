package app

import (
	"log/slog"

	"agenthub/backend/internal/conf"
	ctrlimpl "agenthub/backend/internal/controller/impl"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/service/impl"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/storage"

	"github.com/gin-gonic/gin"
)

const maxJSONBodySize = 1 << 20

type Dependencies struct {
	Config          *conf.Config
	AgentClient     *agentend_client.Client
	StorageProvider storage.Provider
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
	skillService := impl.NewSkillService(skillDao, sessionDao, deps.AgentClient)
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
	skillController := ctrlimpl.NewSkillController(skillService)
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
	skillController.RegisterRoutes(api)
	workspaceController.RegisterRoutes(api)
	adminController.RegisterRoutes(api)

	return r
}
