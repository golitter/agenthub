package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agenthub/backend/internal/conf"
	ctrlimpl "agenthub/backend/internal/controller/impl"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/stream"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/redis"
	"agenthub/backend/pkg/storage"

	"github.com/gin-gonic/gin"
)

const maxJSONBodySize = 1 << 20

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

	if err := gormdao.CleanupDuplicateJoinRows(); err != nil {
		slog.Error("cleanup duplicate join rows", "error", err)
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
	defer func() {
		if err := redis.Close(); err != nil {
			slog.Warn("close redis", "error", err)
		}
	}()

	stream.CleanupStaleMessages(gormdao.NewMessageDao())
	stream.Hub.StartClosedKeysCleanup()

	agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
	storageProvider, err := storage.NewProvider(&cfg.Qiniu, &cfg.Storage)
	if err != nil {
		slog.Error("init storage", "error", err)
		os.Exit(1)
	}

	taskController := ctrlimpl.NewTaskController(agentClient)
	agentController := ctrlimpl.NewAgentController()
	sessionController := ctrlimpl.NewSessionController()
	messageController := ctrlimpl.NewMessageController()
	avatarController := ctrlimpl.NewAvatarController(storageProvider)
	streamController := ctrlimpl.NewStreamController()
	agentProfileController := ctrlimpl.NewAgentProfileController(agentClient)
	workspaceController := ctrlimpl.NewWorkspaceController(agentClient)
	diffSnapshotController := ctrlimpl.NewDiffSnapshotController()
	announcementController := ctrlimpl.NewAnnouncementController(agentClient)
	contactGroupController := ctrlimpl.NewContactGroupController()
	skillController := ctrlimpl.NewSkillController(agentClient)
	adminController := ctrlimpl.NewAdminController(cfg, storageProvider, agentClient)

	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.CORS(cfg.CORS.AllowOrigins))
	r.Use(gin.Recovery())

	// Serve local uploads when using local storage
	if local, ok := storageProvider.(*storage.LocalStorage); ok {
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
	if cfg.Auth.Enabled {
		api.Use(middleware.AuthWithSkips(
			cfg.JWT.Secret,
			"/api/admin/auth",
			"/api/admin/health",
			"/api/admin/avatar",
		))
	}
	{
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
	}

	adminController.RegisterRoutes(api)

	addr := ":" + fmt.Sprint(cfg.Server.Port)
	slog.Info("server starting", "port", cfg.Server.Port)

	srv := &http.Server{Addr: addr, Handler: r}

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

	slog.Info("server exited")
}
