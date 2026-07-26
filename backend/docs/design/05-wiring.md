# Wiring — 应用组装

## 实现了什么

`main.go` 作为应用入口，完成配置加载、数据库初始化、Redis 连接、模型自动迁移、Controller 依赖注入（内部组装 DAO → Service → Controller）、中间件挂载和路由注册，将所有组件串联为可运行的 HTTP 服务。支持优雅关闭（SIGINT/SIGTERM 信号处理）。

## 怎么实现的

### 初始化链 (`cmd/server/main.go`)

按依赖顺序依次初始化：配置 → MySQL → 清理历史重复 join 行 → AutoMigrate → Redis → 清理残留消息。

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

	if err := gormdao.CleanupDuplicateJoinRows(); err != nil {
		slog.Error("cleanup duplicate join rows", "error", err)
		os.Exit(1)
	}

	if err := db.GetDB().AutoMigrate(
		&model.Session{}, &model.Task{}, &model.Message{},
		&model.DiffSnapshot{}, &model.SessionAgent{}, &model.AdminSetting{},
		&model.Announcement{}, &model.ContactGroup{}, &model.ContactGroupItem{},
		&model.SkillHub{}, &model.AgentSkill{},
	); err != nil {
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
	// ...
}
```

`CleanupDuplicateJoinRows` 只在旧表已存在时执行，用于在 `AutoMigrate` 创建 `(group_id, task_id)`、`(session_id, skill_name)` 复合唯一索引前清理历史重复关联，避免迁移被旧脏数据卡住。

### 依赖注入

每个 Controller 的构造函数内部自行创建 DAO 实例并组装 Service，最终返回完整的 Controller。Controller 层对外仅暴露 `NewXxxController(外部依赖)` 接口：

```go
agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
storageProvider, err := storage.NewProvider(&cfg.Qiniu, &cfg.Storage)

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
```

以 `TaskController` 为例，内部组装链为：

```go
func NewTaskController(agentClient *agentend_client.Client) *TaskController {
	taskDao := gormdao.NewTaskDao()
	sessionDao := gormdao.NewSessionDao()
	messageDao := gormdao.NewMessageDao()
	diffDao := gormdao.NewDiffSnapshotDao()
	taskService := svcimpl.NewTaskService(taskDao, sessionDao, messageDao, diffDao, agentClient)
	return &TaskController{service: taskService, agentClient: agentClient}
}
```

外部依赖说明：

| Controller | 外部依赖 | 说明 |
|------------|---------|------|
| TaskController | `agentend_client.Client` | 转发 run、review 和 validate-repo-path |
| AvatarController | `storage.Provider` | 头像上传（七牛云优先，本地磁盘兜底） |
| AgentProfileController | `agentend_client.Client` | 技能查询 |
| WorkspaceController | `agentend_client.Client` | 代理工作区操作到 AgentEnd |
| AnnouncementController | `agentend_client.Client` | Agent 通知 |
| SkillController | `agentend_client.Client` | 技能同步到 AgentEnd |
| AdminController | `Config` + `storage.Provider` + `agentend_client.Client` | 认证/头像/代理 |
| 其余 Controller | 无 | Session、Message、Agent、Stream、DiffSnapshot、ContactGroup |

### 中间件

```go
r := gin.New()
r.Use(middleware.Logger())
r.Use(middleware.CORS(cfg.CORS.AllowOrigins))
r.Use(gin.Recovery())

// Serve local uploads when using local storage
if local, ok := storageProvider.(*storage.LocalStorage); ok {
    r.Static("/uploads", local.Dir())
}
```

`/api` 路由组额外挂载 `middleware.JSONBodyLimit(1<<20, "/api/workspace")`：普通 JSON / `+json` 请求体最大 1MB；workspace 代理路由跳过该限制，继续使用代理层自己的 25MB 上限。

当 `cfg.Auth.Enabled` 为 true 时，`/api` 路由组还会挂载 `AuthWithSkips`。普通业务 API 需要 Bearer JWT；`/api/admin/auth`、`/api/admin/health`、`/api/admin/avatar` 保持公开，其中受保护的 admin 写接口仍由 `AdminAuth` 二次校验。只有 `GET .../stream` SSE 路由可以通过 `access_token` query 传递同一 JWT，兼容浏览器 EventSource。

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
})
```

当 `cors.allow_origins` 配置为 `*` 时，后端使用 allow-all origins 模式并关闭 credentials；配置为具体 origin 白名单时才允许 credentials。

### 路由注册

每个 Controller 通过 `RegisterRoutes(api)` 自注册路由，替代旧版 main.go 中的手动注册：

```go
api := r.Group("/api")
api.Use(middleware.JSONBodyLimit(1<<20, "/api/workspace"))
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

HTTP 服务监听端口来自 `config.yaml` 的 `server.port`，也可用 `SERVER_PORT` 覆盖；未配置时默认 `8080`。

### 优雅关闭

使用 `http.Server` + signal handling 实现 15 秒优雅关闭：

```go
addr := ":" + fmt.Sprint(cfg.Server.Port)
srv := &http.Server{Addr: addr, Handler: r}

go func() {
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}()

quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit
slog.Info("shutting down server...")

ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
if err := srv.Shutdown(ctx); err != nil {
	slog.Error("server forced to shutdown", "error", err)
}

redis.Close()
slog.Info("server exited")
```
