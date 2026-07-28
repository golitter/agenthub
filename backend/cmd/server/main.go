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

	"agenthub/backend/internal/app"
	"agenthub/backend/internal/conf"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/stream"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/redis"
	"agenthub/backend/pkg/storage"
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

	r := app.NewRouter(app.Dependencies{
		Config:          cfg,
		AgentClient:     agentClient,
		StorageProvider: storageProvider,
	})

	addr := ":" + fmt.Sprint(cfg.Server.Port)
	slog.Info("server starting", "port", cfg.Server.Port)

	srv := &http.Server{Addr: addr, Handler: r}

	// 在 goroutine 中启动 HTTP 服务
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server...")

	// 给未完成的请求 15 秒处理时间，然后强制关闭
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server exited")
}
