# Wiring — 应用组装

## 实现了什么

`main.go` 作为应用入口，完成配置加载、数据库初始化、Redis 连接、模型自动迁移、Handler 依赖注入、中间件挂载和路由注册，将所有组件串联为可运行的 HTTP 服务。

## 怎么实现的

### 初始化链 (`cmd/server/main.go`)

按依赖顺序依次初始化：配置 -> MySQL -> AutoMigrate -> Redis -> 清理残留消息。

```go
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

	if err := db.GetDB().AutoMigrate(&model.Session{}, &model.Task{}, &model.Message{}, &model.DiffSnapshot{}, &model.SessionAgent{}, &model.AdminSetting{}, &model.Announcement{}); err != nil {
		slog.Error("auto migrate", "error", err)
		os.Exit(1)
	}

	if err := redis.Init(&cfg.Redis); err != nil {
		slog.Error("init redis", "error", err)
		os.Exit(1)
	}
	defer redis.Close()

	stream.CleanupStaleMessages()
	// ...
}
```

### 依赖注入

通过构造函数闭包注入外部依赖（AgentEnd Client、七牛云 Uploader）到各 Handler：

```go
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
announcementHandler := handler.NewAnnouncementHandler()
adminHandler := handler.NewAdminHandler(cfg, qiniuUploader, agentClient)
```

- `TaskHandler` 依赖 `agentend_client.Client`（转发 run、review 和 validate-repo-path）
- `AvatarHandler` 依赖 `qiniu.Uploader`（头像上传）
- `AgentProfileHandler` 无外部依赖（读取 Session/Task/Message 表）
- `WorkspaceHandler` 依赖 `agentend_client.Client`（代理工作区操作到 AgentEnd）
- `AdminHandler` 依赖 `Config`（密码验证）+ `qiniu.Uploader`（头像管理）+ `agentend_client.Client`（代理资源/健康请求）
- 其余 Handler（Session、Message、Agent、Stream、DiffSnapshot、Announcement）无外部依赖

### 中间件

```go
r := gin.New()
r.Use(middleware.Logger())
r.Use(middleware.CORS(cfg.CORS.AllowOrigins))
r.Use(gin.Recovery())
```

CORS 配置从 `config.yaml` 的 `cors.allow_origins` 字段加载，默认允许 `http://localhost:5173`：

```go
func CORS(origins []string) gin.HandlerFunc {
	if len(origins) == 0 {
		origins = []string{"http://localhost:5173"}
	}
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
```

### 路由注册

所有业务路由挂载在 `/api` Group 下：

```go
api := r.Group("/api")
{
	api.POST("/tasks", taskHandler.CreateTask)
	api.GET("/tasks", taskHandler.ListTasks)
	api.GET("/tasks/:taskId", taskHandler.GetTask)
	api.DELETE("/tasks/:taskId", taskHandler.DeleteTask)
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

	// Workspace proxy routes (by workspace ID)
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

	// Session-level workspace proxy routes
	ss := api.Group("/session")
	{
		ss.GET("/:sessionId/files/*filepath", workspaceHandler.SessionFileRead)
		ss.PUT("/:sessionId/files/*filepath", workspaceHandler.SessionFileWrite)
		ss.GET("/:sessionId/diff", workspaceHandler.SessionGetDiff)
		ss.POST("/:sessionId/commit", workspaceHandler.SessionCommit)
		ss.POST("/:sessionId/revert", workspaceHandler.SessionRevert)
	}
}

	// Admin panel routes (self-registered via RegisterRoutes)
	adminHandler.RegisterRoutes(api)
```

健康检查端点：

```go
r.GET("/ping", func(c *gin.Context) {
	vo.OK(c, gin.H{"message": "pong"})
})

r.GET("/health", func(c *gin.Context) {
	vo.OK(c, gin.H{"status": "ok"})
})
```

服务监听：

```go
slog.Info("server starting", "port", 8080)
if err := r.Run(":8080"); err != nil && err != http.ErrServerClosed {
	slog.Error("server failed", "error", err)
	os.Exit(1)
}
```
