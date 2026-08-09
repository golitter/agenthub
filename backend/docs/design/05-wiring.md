# Wiring — 应用组装

## 实现了什么

`main.go` 作为应用入口，完成配置加载、数据库初始化、Redis 连接、模型自动迁移和 HTTP Server 生命周期管理。`internal/app` 集中完成 DAO → Service → Controller 的依赖组装、中间件挂载和路由注册，将所有组件串联为可运行的 HTTP 服务。支持优雅关闭（SIGINT/SIGTERM 信号处理）。

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

`internal/app.NewRouter(deps Dependencies)` 集中创建 DAO、Service、Controller。`Config`、`agentend_client.Client`、`storage.Provider` 等外部依赖由 `main.go` 构造为 `Dependencies` 结构体传入（`app.NewRouter(app.Dependencies{Config: cfg, AgentClient: agentClient, StorageProvider: storageProvider})`），路由内部通过 `deps.Config` / `deps.AgentClient` / `deps.StorageProvider` 取用。Controller 构造函数只接收所需的 Service 接口或外部客户端，不再直接依赖 GORM DAO 实现：

```go
// main.go 中构造外部依赖并注入
agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
storageProvider, err := storage.NewProvider(&cfg.Qiniu, &cfg.Storage)
router := app.NewRouter(app.Dependencies{
    Config:           cfg,
    AgentClient:      agentClient,
    StorageProvider:  storageProvider,
})

// NewRouter 内部（deps.* 即上面注入的依赖）
sessionDao := gormdao.NewSessionDao()
taskDao := gormdao.NewTaskDao()
messageDao := gormdao.NewMessageDao()
diffSnapshotDao := gormdao.NewDiffSnapshotDao()

sessionService := impl.NewSessionService(sessionDao)
taskService := impl.NewTaskService(taskDao, sessionDao, messageDao, diffSnapshotDao, deps.AgentClient)
messageService := impl.NewMessageService(taskDao, sessionDao, messageDao)

taskController := ctrlimpl.NewTaskController(taskService, deps.AgentClient)
agentController := ctrlimpl.NewAgentController()
sessionController := ctrlimpl.NewSessionController(sessionService)
messageController := ctrlimpl.NewMessageController(messageService)
workspaceController := ctrlimpl.NewWorkspaceController(deps.AgentClient)
```

以 `TaskController` 为例，Controller 只保存业务接口：

```go
func NewTaskController(taskService service.TaskService, agentClient *agentend_client.Client) *TaskController {
	return &TaskController{service: taskService, agentClient: agentClient}
}
```

外部依赖说明：

| Controller | 外部依赖 | 说明 |
|------------|---------|------|
| TaskController | `TaskService`（内部注入 `agentend_client.Client`） | 转发 run、review 和 validate-repo-path；`agentend_client.Client` 在 Service 层注入，Controller 只持有 `TaskService` |
| AvatarController | `AvatarService`（内部注入 `storage.Provider`） | 头像上传（七牛云优先，本地磁盘兜底）；`storage.Provider` 在 Service 层注入，Controller 只持有 `AvatarService` |
| AgentProfileController | `AgentProfileService`（内部注入 `agentend_client.Client`） | Agent 详情 / SOUL.md 读写；Controller 只持有 Service，不直接依赖 agentend_client |
| WorkspaceController | `agentend_client.Client`（直接持有，无 Service 层） | 代理工作区操作到 AgentEnd，并持有 `*http.Client` 用于流式合并预览 |
| AnnouncementController | `AnnouncementService`（内部注入 `agentend_client.Client`） | 公告管理；Controller 只持有 Service，不直接依赖 agentend_client |
| SkillController | `SkillService`（内部注入 `agentend_client.Client`） | 技能同步到 AgentEnd |
| AdminController | `Config` + `AdminService` | 认证/头像/代理 |
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
