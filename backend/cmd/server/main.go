package main

import (
	"log/slog"
	"net/http"
	"os"

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

	if err := db.GetDB().AutoMigrate(&model.Session{}, &model.Task{}, &model.Message{}, &model.DiffSnapshot{}, &model.SessionAgent{}); err != nil {
		slog.Error("auto migrate", "error", err)
		os.Exit(1)
	}

	if err := redis.Init(&cfg.Redis); err != nil {
		slog.Error("init redis", "error", err)
		os.Exit(1)
	}
	defer redis.Close()

	stream.CleanupStaleMessages()

	agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
	qiniuUploader := qiniu.NewUploader(&cfg.Qiniu)

	taskHandler := handler.NewTaskHandler(agentClient)
	agentHandler := handler.NewAgentHandler()
	sessionHandler := handler.NewSessionHandler()
	messageHandler := handler.NewMessageHandler()
	avatarHandler := handler.NewAvatarHandler(qiniuUploader)
	streamHandler := handler.NewStreamHandler()
	agentProfileHandler := handler.NewAgentProfileHandler()
	workspaceHandler := handler.NewWorkspaceHandler(agentClient)
	diffSnapshotHandler := handler.NewDiffSnapshotHandler()
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

		api.POST("/tasks/:taskId/run", taskHandler.RunTask)
		api.GET("/tasks/:taskId/stream", streamHandler.ServeStream)
		api.GET("/tasks/:taskId/messages", messageHandler.ListMessages)

		api.GET("/agent-types", agentHandler.ListAgentTypes)

		api.PATCH("/sessions/:sessionId", sessionHandler.PatchSession)
		api.PUT("/sessions/:sessionId", avatarHandler.UpdateSession)
		api.GET("/sessions/:sessionId/profile", agentProfileHandler.GetProfile)
		api.GET("/sessions/:sessionId/detail", agentProfileHandler.GetDetail)

		api.POST("/agents/avatar", avatarHandler.UploadAvatar)
		api.POST("/validate-repo-path", taskHandler.ValidateRepoPath)

		// Diff snapshot routes
		api.GET("/diff-snapshots/:snapshotId", diffSnapshotHandler.GetDiffSnapshot)
		api.PUT("/diff-snapshots/:snapshotId", diffSnapshotHandler.SaveDiffSnapshot)

		ws := api.Group("/workspace")
		{
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
	if err := r.Run(":8080"); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
