package main

import (
	"log/slog"
	"net/http"
	"os"

	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/handler"
	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"

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

	if err := db.GetDB().AutoMigrate(&model.Session{}, &model.Task{}); err != nil {
		slog.Error("auto migrate", "error", err)
		os.Exit(1)
	}

	agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)

	sessionHandler := handler.NewSessionHandler()
	taskHandler := handler.NewTaskHandler(agentClient)
	agentHandler := handler.NewAgentHandler()

	r := gin.New()
	r.Use(middleware.Logger())
	r.Use(middleware.CORS())
	r.Use(gin.Recovery())

	r.GET("/ping", func(c *gin.Context) {
		vo.OK(c, gin.H{"message": "pong"})
	})

	api := r.Group("/api")
	{
		api.POST("/sessions", sessionHandler.CreateSession)
		api.GET("/sessions", sessionHandler.ListSessions)
		api.GET("/sessions/:id", sessionHandler.GetSession)
		api.DELETE("/sessions/:id", sessionHandler.DeleteSession)

		api.POST("/sessions/:sid/tasks/run", taskHandler.RunTask)

		api.GET("/agents", agentHandler.ListAgents)
	}

	slog.Info("server starting", "port", 8080)
	if err := r.Run(":8080"); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
