package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/handler"
	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/stream"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/qiniu"
	"agenthub/backend/pkg/redis"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := conf.Load("configs/config.yaml")
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	if err := db.Init(&cfg.MySQL); err != nil {
		slog.Error("init db", "error", err)
		os.Exit(1)
	}

	if err := db.GetDB().AutoMigrate(&model.Session{}, &model.Task{}, &model.Message{}, &model.DiffSnapshot{}, &model.SessionAgent{}, &model.AdminSetting{}, &model.Announcement{}, &model.ContactGroup{}, &model.ContactGroupItem{}, &model.SkillHub{}, &model.AgentSkill{}); err != nil {
		slog.Error("auto migrate", "error", err)
		os.Exit(1)
	}

	if err := redis.Init(&cfg.Redis); err != nil {
		slog.Error("init redis", "error", err)
		os.Exit(1)
	}
	defer redis.Close()

	stream.CleanupStaleMessages()
	stream.Hub.StartClosedKeysCleanup()

	agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
	qiniuUploader := qiniu.NewUploader(&cfg.Qiniu)

	taskHandler := handler.NewTaskHandler(agentClient)
	agentHandler := handler.NewAgentHandler()
	sessionHandler := handler.NewSessionHandler()
	messageHandler := handler.NewMessageHandler()
	avatarHandler := handler.NewAvatarHandler(qiniuUploader)
	streamHandler := handler.NewStreamHandler()
	agentProfileHandler := handler.NewAgentProfileHandler(agentClient)
	workspaceHandler := handler.NewWorkspaceHandler(agentClient)
	diffSnapshotHandler := handler.NewDiffSnapshotHandler()
	announcementHandler := handler.NewAnnouncementHandler(agentClient)
	contactGroupHandler := handler.NewContactGroupHandler()
	skillHandler := handler.NewSkillHandler(agentClient)
	adminHandler := handler.NewAdminHandler(cfg, qiniuUploader, agentClient)

	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.CORS(cfg.CORS.AllowOrigins))
	r.Use(gin.Recovery())

	r.GET("/ping", func(c *gin.Context) {
		vo.OK(c, gin.H{"message": "pong"})
	})

	r.GET("/health", func(c *gin.Context) {
		vo.OK(c, gin.H{"status": "ok"})
	})

	api := r.Group("/api")
	{
		api.POST("/tasks", taskHandler.CreateTask)
		api.GET("/tasks", taskHandler.ListTasks)
		api.GET("/tasks/:taskId", taskHandler.GetTask)
		api.DELETE("/tasks/:taskId", taskHandler.DeleteTask)
		api.DELETE("/tasks/:taskId/leave", taskHandler.LeaveTask)
		api.PATCH("/tasks/:taskId", taskHandler.PatchTask)

		api.POST("/tasks/:taskId/run", taskHandler.RunTask)
		api.POST("/tasks/:taskId/review", taskHandler.ReviewTask)
		api.GET("/tasks/:taskId/stream", streamHandler.ServeStream)
		api.GET("/tasks/:taskId/messages", messageHandler.ListMessages)
		api.GET("/tasks/:taskId/messages/window", messageHandler.WindowMessages)

		api.GET("/agent-types", agentHandler.ListAgentTypes)

		api.GET("/tasks/:taskId/announcements", announcementHandler.ListAnnouncements)
		api.POST("/tasks/:taskId/announcements", announcementHandler.CreateAnnouncement)
		api.DELETE("/tasks/:taskId/announcements/:id", announcementHandler.DeleteAnnouncement)

		api.PATCH("/sessions/:sessionId", sessionHandler.PatchSession)
		api.PUT("/sessions/:sessionId", avatarHandler.UpdateSession)
		api.GET("/sessions/:sessionId/profile", agentProfileHandler.GetProfile)
		api.GET("/sessions/:sessionId/detail", agentProfileHandler.GetDetail)
		api.GET("/sessions/:sessionId/soul", agentProfileHandler.GetSoul)
		api.PUT("/sessions/:sessionId/soul", agentProfileHandler.UpdateSoul)

		api.POST("/agents/avatar", avatarHandler.UploadAvatar)
		api.POST("/validate-repo-path", taskHandler.ValidateRepoPath)

		// Diff snapshot routes
		api.GET("/diff-snapshots/:snapshotId", diffSnapshotHandler.GetDiffSnapshot)
		api.PUT("/diff-snapshots/:snapshotId", diffSnapshotHandler.SaveDiffSnapshot)

		// Contact group routes
		api.GET("/contact-groups", contactGroupHandler.ListGroups)
		api.POST("/contact-groups", contactGroupHandler.CreateGroup)
		api.PUT("/contact-groups/:groupId", contactGroupHandler.UpdateGroup)
		api.DELETE("/contact-groups/:groupId", contactGroupHandler.DeleteGroup)
		api.POST("/contact-groups/:groupId/items", contactGroupHandler.AddItem)
		api.DELETE("/contact-groups/:groupId/items/:taskID", contactGroupHandler.RemoveItem)

		// SkillsHub routes
		api.POST("/skills/upload", skillHandler.Upload)
		api.POST("/skills/confirm", skillHandler.Confirm)
		api.GET("/skills", skillHandler.List)
		api.DELETE("/skills/:name", skillHandler.Delete)
		api.POST("/skills/:name/import", skillHandler.Import)
		api.DELETE("/skills/:name/sessions/:sessionId", skillHandler.Remove)

		// Builtin skills report (internal)
		api.POST("/internal/builtin-skills", skillHandler.ReportBuiltinSkills)

		ws := api.Group("/workspace")
		{
			ws.GET("/task/:taskId/git-info", workspaceHandler.TaskGitInfo)
			ws.POST("/task/:taskId/merge-to-main", workspaceHandler.MergeTaskToMain)
			ws.GET("/:id/files/*filepath", workspaceHandler.ReadFile)
			ws.PUT("/:id/files/*filepath", workspaceHandler.WriteFile)
			ws.GET("/:id/diff", workspaceHandler.GetDiff)
			ws.POST("/:id/commit", workspaceHandler.Commit)
			ws.POST("/:id/revert", workspaceHandler.Revert)
			ws.POST("/:id/preview/start", workspaceHandler.StartPreview)
			ws.POST("/:id/preview/stop", workspaceHandler.StopPreview)
		}

		ss := api.Group("/session")
		{
			ss.GET("/:sessionId/files/*filepath", workspaceHandler.SessionFileRead)
			ss.PUT("/:sessionId/files/*filepath", workspaceHandler.SessionFileWrite)
			ss.GET("/:sessionId/diff", workspaceHandler.SessionGetDiff)
			ss.POST("/:sessionId/commit", workspaceHandler.SessionCommit)
			ss.POST("/:sessionId/revert", workspaceHandler.SessionRevert)
		}
	}

	adminHandler.RegisterRoutes(api)

	slog.Info("server starting", "port", 8080)

	srv := &http.Server{Addr: ":8080", Handler: r}

	// Start server in goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server...")

	// Give outstanding requests 15 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	// Close Redis connection
	redis.Close()

	slog.Info("server exited")
}
